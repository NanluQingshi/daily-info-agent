import { useCallback, useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { getStats } from "../api/client";
import { RunHistoryPanel } from "./RunHistoryPanel";
import type { StatsResult } from "../types";
import { SourceHealthPanel } from "./SourceHealthPanel";

// ── Color palette for categories ─────────────────────────────────────────

const CATEGORY_HEX: Record<string, string> = {
  "金融": "#10b981",
  "政治": "#3b82f6",
  "经济": "#f59e0b",
  "科技/AI": "#8b5cf6",
  "国际": "#f43f5e",
};

function categoryHex(cat: string): string {
  return CATEGORY_HEX[cat] ?? "#94a3b8";
}

// ── SVG chart components (zero-dependency) ────────────────────────────────

// TrendLine renders a simple area+line chart from date/count pairs.
function TrendLine({ points, height = 140 }: { points: { date: string; count: number }[]; height?: number }) {
  if (points.length === 0) return <p className="text-sm text-muted-foreground text-center py-4">暂无数据</p>;
  const width = 300;
  const pad = 8;
  const max = Math.max(1, ...points.map((p) => p.count));

  const coords = points.map((p, i) => {
    const x = pad + (i * (width - pad * 2)) / Math.max(1, points.length - 1);
    const y = height - pad - (p.count / max) * (height - pad * 2);
    return { x, y, ...p };
  });

  const line = coords.map((c, i) => `${i === 0 ? "M" : "L"}${c.x.toFixed(1)},${c.y.toFixed(1)}`).join(" ");
  const area = `${line} L${coords[coords.length - 1].x.toFixed(1)},${height - pad} L${coords[0].x.toFixed(1)},${height - pad} Z`;

  return (
    <div>
      <svg viewBox={`0 0 ${width} ${height}`} className="w-full h-auto">
        <path d={area} fill="oklch(0.205 0 0 / 0.08)" />
        <path d={line} fill="none" stroke="oklch(0.205 0 0 / 0.8)" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
        {coords.map((c) => (
          <circle key={c.date} cx={c.x} cy={c.y} r={2.5} fill="oklch(0.205 0 0 / 0.8)" />
        ))}
      </svg>
      <div className="flex justify-between text-[10px] text-muted-foreground mt-1">
        <span>{points[0].date}</span>
        <span>{points[points.length - 1].date}</span>
      </div>
    </div>
  );
}

// DonutChart renders a category distribution donut with a legend.
function DonutChart({ items }: { items: { category: string; count: number }[] }) {
  const total = items.reduce((s, i) => s + i.count, 0);
  if (total === 0) return <p className="text-sm text-muted-foreground text-center py-4">暂无数据</p>;

  const R = 40;
  const C = 2 * Math.PI * R;
  let offset = 0;

  const segments = items.map((it) => {
    const frac = it.count / total;
    const seg = { ...it, frac, dash: frac * C, offset };
    offset += frac * C;
    return seg;
  });

  return (
    <div className="flex items-center gap-4">
      <svg viewBox="0 0 100 100" className="w-32 h-32 shrink-0">
        <circle cx={50} cy={50} r={R} fill="none" stroke="var(--muted)" strokeWidth={14} />
        {segments.map((s) => (
          <circle
            key={s.category}
            cx={50} cy={50} r={R}
            fill="none"
            stroke={categoryHex(s.category)}
            strokeWidth={14}
            strokeDasharray={`${s.dash} ${C - s.dash}`}
            strokeDashoffset={-s.offset}
            transform="rotate(-90 50 50)"
          />
        ))}
        <text x={50} y={52} textAnchor="middle" fontSize={16} fontWeight={600} fill="currentColor">
          {total}
        </text>
      </svg>
      <div className="space-y-1.5 text-xs">
        {segments.map((s) => (
          <div key={s.category} className="flex items-center gap-2">
            <span className="w-2.5 h-2.5 rounded-full shrink-0" style={{ background: categoryHex(s.category) }} />
            <span className="text-foreground">{s.category}</span>
            <span className="text-muted-foreground ml-auto pl-2">{(s.frac * 100).toFixed(1)}%</span>
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Component ────────────────────────────────────────────────────────────

export function StatsPanel() {
  const [data, setData] = useState<StatsResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    setError(null);
    getStats()
      .then(setData)
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  if (loading) return <div className="text-center py-16 text-muted-foreground">加载中…</div>;
  if (error) return (
    <div className="bg-destructive/10 text-destructive text-sm rounded-xl p-4 space-y-1">
      <p>{error}</p>
      {error.includes("DATABASE_DSN") && (
        <p className="text-xs opacity-75">
          在 <code className="bg-destructive/10 px-1 rounded">.env</code> 中配置 DATABASE_DSN 并重启服务端即可启用统计功能。
        </p>
      )}
    </div>
  );
  if (!data) return null;

  const byDay = data.by_day ?? [];
  const byCategory = data.by_category ?? [];
  const recentRuns = data.recent_runs ?? [];
  const maxDay = Math.max(1, ...byDay.map((d) => d.count));
  const totalArticles = byDay.reduce((s, d) => s + d.count, 0);
  const totalRuns = recentRuns.length;
  const lastRun = recentRuns[0];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">统计</h2>
        <Button variant="ghost" size="sm" onClick={load} disabled={loading} className="h-8 px-2">
          <RefreshCw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} />
        </Button>
      </div>

      {/* Summary cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <Card>
          <CardContent className="p-4">
            <p className="text-xs text-muted-foreground uppercase tracking-wide">文章总数</p>
            <p className="text-2xl font-bold mt-1">{totalArticles}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <p className="text-xs text-muted-foreground uppercase tracking-wide">分类数</p>
            <p className="text-2xl font-bold mt-1">{byCategory.length}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <p className="text-xs text-muted-foreground uppercase tracking-wide">运行次数</p>
            <p className="text-2xl font-bold mt-1">{totalRuns}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <p className="text-xs text-muted-foreground uppercase tracking-wide">最近运行</p>
            <p className="text-lg font-bold mt-1 truncate">
              {lastRun ? `${(lastRun.duration_ms / 1000).toFixed(1)}s` : "—"}
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Charts */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* By Day — trend line */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium">每日文章数趋势（近90天）</CardTitle>
          </CardHeader>
          <CardContent>
            <TrendLine points={byDay.slice(0, 30)} />
          </CardContent>
        </Card>

        {/* By Category — donut */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium">按分类分布</CardTitle>
          </CardHeader>
          <CardContent>
            <DonutChart items={byCategory} />
          </CardContent>
        </Card>
      </div>

      {/* By Day — detailed bars (all data) */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-medium">每日文章数明细</CardTitle>
        </CardHeader>
        <CardContent>
          {byDay.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-4">暂无数据</p>
          ) : (
            <div className="space-y-2">
              {byDay.slice(0, 15).map((d) => (
                <div key={d.date} className="flex items-center gap-3">
                  <span className="text-xs text-muted-foreground w-24 shrink-0 font-mono">{d.date}</span>
                  <div className="flex-1 bg-muted rounded-full h-5 relative overflow-hidden">
                    <div
                      className="bg-primary h-full rounded-full transition-all duration-500"
                      style={{ width: `${(d.count / maxDay) * 100}%` }}
                    />
                    <span className="absolute inset-0 flex items-center px-2 text-xs text-primary-foreground font-medium mix-blend-difference">
                      {d.count}
                    </span>
                  </div>
                </div>
              ))}
              {byDay.length > 15 && (
                <p className="text-xs text-muted-foreground text-center pt-1">
                  … 还有 {byDay.length - 15} 天
                </p>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Run history */}
      <RunHistoryPanel />

      {/* Source health */}
      <SourceHealthPanel />
    </div>
  );
}
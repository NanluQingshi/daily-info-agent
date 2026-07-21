import { Fragment, useCallback, useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { getStats } from "../api/client";
import type { StatsResult } from "../types";

// ── Color palette for categories ─────────────────────────────────────────

const CATEGORY_COLORS: Record<string, string> = {
  "金融": "bg-emerald-500",
  "政治": "bg-blue-500",
  "经济": "bg-amber-500",
  "科技/AI": "bg-violet-500",
  "国际": "bg-rose-500",
};

function categoryColor(cat: string): string {
  return CATEGORY_COLORS[cat] ?? "bg-slate-400";
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
  const maxCat = Math.max(1, ...byCategory.map((c) => c.count));
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
        {/* By Day */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium">每日文章数（近90天）</CardTitle>
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

        {/* By Category */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium">按分类</CardTitle>
          </CardHeader>
          <CardContent>
            {byCategory.length === 0 ? (
              <p className="text-sm text-muted-foreground text-center py-4">暂无数据</p>
            ) : (
              <div className="space-y-3">
                {byCategory.map((c) => {
                  const pct = maxCat > 0 ? (c.count / maxCat) * 100 : 0;
                  return (
                    <div key={c.category}>
                      <div className="flex items-center justify-between mb-1">
                        <span className="text-sm font-medium">{c.category}</span>
                        <span className="text-xs text-muted-foreground">{c.count} 篇</span>
                      </div>
                      <div className="flex-1 bg-muted rounded-full h-2 overflow-hidden">
                        <div
                          className={`h-full rounded-full transition-all duration-500 ${categoryColor(c.category)}`}
                          style={{ width: `${pct}%` }}
                        />
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Recent Runs */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-medium">最近运行记录</CardTitle>
        </CardHeader>
        <CardContent>
          {recentRuns.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-4">暂无记录</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-muted-foreground border-b">
                    <th className="pb-2 font-medium text-left">Run ID</th>
                    <th className="pb-2 font-medium text-right">抓取</th>
                    <th className="pb-2 font-medium text-right">处理</th>
                    <th className="pb-2 font-medium text-right">保存</th>
                    <th className="pb-2 font-medium text-right">发布</th>
                    <th className="pb-2 font-medium text-right">跳过</th>
                    <th className="pb-2 font-medium text-right">失败</th>
                    <th className="pb-2 font-medium text-right">耗时</th>
                    <th className="pb-2 font-medium text-right">时间</th>
                  </tr>
                </thead>
                <tbody>
                  {recentRuns.map((r, i) => (
                    <Fragment key={r.run_id}>
                      <tr className="text-foreground hover:bg-muted/30 transition-colors">
                        <td className="py-2 font-mono">{r.run_id.slice(0, 8)}…</td>
                        <td className="py-2 text-right">{r.total_fetched}</td>
                        <td className="py-2 text-right">{r.total_processed}</td>
                        <td className="py-2 text-right">{r.total_saved}</td>
                        <td className="py-2 text-right font-medium text-green-600">{r.total_published}</td>
                        <td className="py-2 text-right text-muted-foreground">{r.total_skipped}</td>
                        <td className="py-2 text-right">{r.total_failed > 0 ? <span className="text-destructive">{r.total_failed}</span> : r.total_failed}</td>
                        <td className="py-2 text-right">{(r.duration_ms / 1000).toFixed(1)}s</td>
                        <td className="py-2 text-right text-muted-foreground">{new Date(r.started_at).toLocaleString("zh-CN")}</td>
                      </tr>
                      {i < recentRuns.length - 1 && (
                        <tr key={`sep-${r.run_id}`}><td colSpan={9}><Separator /></td></tr>
                      )}
                    </Fragment>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
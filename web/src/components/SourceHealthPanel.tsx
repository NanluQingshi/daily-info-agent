import { useCallback, useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { getSourceHealth } from "../api/client";
import type { SourceHealthRow } from "../types";
import { showToast } from "./Toast";

// ── Status presentation ──────────────────────────────────────────────────

const STATUS_META: Record<
  SourceHealthRow["status"],
  { label: string; dot: string; text: string }
> = {
  ok: { label: "正常", dot: "bg-emerald-500", text: "text-emerald-600 dark:text-emerald-400" },
  warning: { label: "告警", dot: "bg-amber-500", text: "text-amber-600 dark:text-amber-400" },
  disabled: { label: "已禁用", dot: "bg-red-500", text: "text-red-600 dark:text-red-400" },
  unknown: { label: "无运行记录", dot: "bg-slate-400", text: "text-muted-foreground" },
};

function StatusBadge({ status }: { status: SourceHealthRow["status"] }) {
  const meta = STATUS_META[status];
  return (
    <span className={`inline-flex items-center gap-1.5 text-xs font-medium ${meta.text}`}>
      <span className={`w-2 h-2 rounded-full ${meta.dot} ${status === "warning" ? "animate-pulse" : ""}`} />
      {meta.label}
    </span>
  );
}

// ── Formatting helpers ───────────────────────────────────────────────────

function formatTime(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function formatRate(row: SourceHealthRow): string {
  if (row.total_attempts === 0) return "—";
  const ok = row.total_attempts - row.total_failures;
  return `${((ok / row.total_attempts) * 100).toFixed(0)}%`;
}

// Short display: prefer the domain, fall back to a truncated source string.
function sourceLabel(row: SourceHealthRow): string {
  if (row.domain) return row.domain;
  return row.source.length > 32 ? row.source.slice(0, 32) + "…" : row.source;
}

// ── Component ────────────────────────────────────────────────────────────

export function SourceHealthPanel() {
  const [rows, setRows] = useState<SourceHealthRow[] | null>(null);
  const [windowDays, setWindowDays] = useState(7);
  const [loading, setLoading] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    getSourceHealth()
      .then((resp) => {
        setRows(resp.sources ?? []);
        setWindowDays(resp.window_days ?? 7);
      })
      .catch((e: Error) => {
        // The panel degrades gracefully: keep whatever was rendered before
        // and surface the error as a toast.
        showToast("error", `源健康加载失败: ${e.message}`);
        setRows((prev) => prev ?? []);
      })
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  const disabledCount = rows?.filter((r) => r.status === "disabled").length ?? 0;
  const warningCount = rows?.filter((r) => r.status === "warning").length ?? 0;

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-medium">
            数据源健康
            {rows && rows.length > 0 && (
              <span className="ml-2 text-xs font-normal text-muted-foreground">
                共 {rows.length} 个
                {disabledCount > 0 && (
                  <span className="ml-1.5 text-red-600 dark:text-red-400">· {disabledCount} 个已禁用</span>
                )}
                {warningCount > 0 && (
                  <span className="ml-1.5 text-amber-600 dark:text-amber-400">· {warningCount} 个告警</span>
                )}
              </span>
            )}
          </CardTitle>
          <Button variant="ghost" size="sm" onClick={load} disabled={loading} className="h-7 px-2">
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {rows === null ? (
          <p className="text-sm text-muted-foreground text-center py-4">加载中…</p>
        ) : rows.length === 0 ? (
          <p className="text-sm text-muted-foreground text-center py-4">
            暂无数据源运行记录 — 抓取一次后这里会显示各源的健康状态
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="text-muted-foreground border-b">
                  <th className="pb-2 font-medium text-left">数据源</th>
                  <th className="pb-2 font-medium text-left">状态</th>
                  <th className="pb-2 font-medium text-right">连续失败</th>
                  <th className="pb-2 font-medium text-right">成功率</th>
                  <th className="pb-2 font-medium text-right">近{windowDays}天文章</th>
                  <th className="pb-2 font-medium text-left pl-4">最近抓取</th>
                  <th className="pb-2 font-medium text-left pl-4">最近错误</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr
                    key={r.source}
                    className={`border-b last:border-0 ${r.status === "disabled" ? "bg-red-500/5" : ""}`}
                  >
                    <td className="py-2 pr-3 font-mono text-[11px]" title={r.source}>
                      {sourceLabel(r)}
                    </td>
                    <td className="py-2 pr-3">
                      <StatusBadge status={r.status} />
                    </td>
                    <td className="py-2 text-right tabular-nums">
                      {r.consecutive_failures > 0 ? (
                        <span className="text-red-600 dark:text-red-400 font-medium">{r.consecutive_failures}</span>
                      ) : (
                        "0"
                      )}
                    </td>
                    <td className="py-2 text-right tabular-nums">{formatRate(r)}</td>
                    <td className="py-2 text-right tabular-nums">{r.recent_articles}</td>
                    <td className="py-2 pl-4 text-muted-foreground whitespace-nowrap">{formatTime(r.last_attempt_at)}</td>
                    <td
                      className="py-2 pl-4 text-muted-foreground max-w-48 truncate"
                      title={r.last_error || undefined}
                    >
                      {r.last_error || "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <p className="text-[10px] text-muted-foreground mt-2">
              连续失败 3 次的源会被自动禁用并在下次运行时跳过；成功一次即自动恢复。成功率为进程内累计值，重启后重新计数。
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

import { useCallback, useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { getRuns } from "../api/client";
import type { RunLogRow } from "../types";
import { showToast } from "./Toast";

const FETCH_COUNT = 30;

function formatStartTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

type RunStatus = "failed" | "partial" | "ok";

function runStatus(r: RunLogRow): RunStatus {
  if (r.fatal_error) return "failed";
  if (r.total_failed > 0) return "partial";
  return "ok";
}

const STATUS_META: Record<RunStatus, { label: string; cls: string; row: string }> = {
  failed: { label: "失败", cls: "text-red-600 dark:text-red-400", row: "bg-red-500/5" },
  partial: { label: "部分失败", cls: "text-amber-600 dark:text-amber-400", row: "" },
  ok: { label: "成功", cls: "text-emerald-600 dark:text-emerald-400", row: "" },
};

/** 最近运行历史：每次管道运行的关键指标（含提取计数），失败行高亮。 */
export function RunHistoryPanel() {
  const [runs, setRuns] = useState<RunLogRow[] | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    getRuns(FETCH_COUNT)
      .then((resp) => setRuns(resp.runs ?? []))
      .catch((e: Error) => {
        showToast("error", `运行历史加载失败: ${e.message}`);
        setRuns((prev) => prev ?? []);
      })
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const failedCount = runs?.filter((r) => runStatus(r) === "failed").length ?? 0;

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-medium">
            运行历史
            {runs && runs.length > 0 && (
              <span className="ml-2 text-xs font-normal text-muted-foreground">
                最近 {runs.length} 次
                {failedCount > 0 && (
                  <span className="ml-1.5 text-red-600 dark:text-red-400">· {failedCount} 次失败</span>
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
        {runs === null ? (
          <p className="text-sm text-muted-foreground text-center py-4">加载中…</p>
        ) : runs.length === 0 ? (
          <p className="text-sm text-muted-foreground text-center py-4">
            暂无运行记录 — 首次运行（定时或手动触发）后会出现在这里
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="text-muted-foreground border-b">
                  <th className="pb-2 font-medium text-left">开始时间</th>
                  <th className="pb-2 font-medium text-left">状态</th>
                  <th className="pb-2 font-medium text-right">抓取</th>
                  <th className="pb-2 font-medium text-right">提取</th>
                  <th className="pb-2 font-medium text-right">处理</th>
                  <th className="pb-2 font-medium text-right">保存</th>
                  <th className="pb-2 font-medium text-right">发布</th>
                  <th className="pb-2 font-medium text-right">跳过</th>
                  <th className="pb-2 font-medium text-right">失败</th>
                  <th className="pb-2 font-medium text-right pl-4">耗时</th>
                </tr>
              </thead>
              <tbody>
                {runs.map((r) => {
                  const st = runStatus(r);
                  const meta = STATUS_META[st];
                  return (
                    <tr key={r.run_id} className={`border-b last:border-0 ${meta.row}`}>
                      <td className="py-2 pr-3 text-muted-foreground whitespace-nowrap">
                        {formatStartTime(r.started_at)}
                      </td>
                      <td className="py-2 pr-3">
                        <span
                          className={`font-medium ${meta.cls}`}
                          title={r.fatal_error || (r.total_failed > 0 ? `${r.total_failed} 条处理失败` : undefined)}
                        >
                          {meta.label}
                        </span>
                      </td>
                      <td className="py-2 text-right tabular-nums">{r.total_fetched}</td>
                      <td className="py-2 text-right tabular-nums text-muted-foreground">{r.total_extracted}</td>
                      <td className="py-2 text-right tabular-nums">{r.total_processed}</td>
                      <td className="py-2 text-right tabular-nums">{r.total_saved}</td>
                      <td className="py-2 text-right tabular-nums">{r.total_published}</td>
                      <td className="py-2 text-right tabular-nums text-muted-foreground">{r.total_skipped}</td>
                      <td className="py-2 text-right tabular-nums">
                        {r.total_failed > 0 ? (
                          <span className="text-amber-600 dark:text-amber-400">{r.total_failed}</span>
                        ) : (
                          "0"
                        )}
                      </td>
                      <td className="py-2 text-right tabular-nums pl-4 text-muted-foreground">
                        {formatDuration(r.duration_ms)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            <p className="text-[10px] text-muted-foreground mt-2">
              状态：失败 = 运行中断；部分失败 = 运行完成但有个别条目处理失败（悬停查看详情）。「提取」列为当次正文提取成功的条数。
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

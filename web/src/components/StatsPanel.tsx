import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { getStats } from "../api/client";
import type { StatsResult } from "../types";

export function StatsPanel() {
  const [data, setData] = useState<StatsResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    getStats()
      .then(setData)
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <div className="text-center py-16 text-muted-foreground">加载中…</div>;
  if (error) return <div className="bg-destructive/10 text-destructive text-sm rounded-xl p-4">{error}</div>;
  if (!data) return null;

  const byDay = data.by_day ?? [];
  const byCategory = data.by_category ?? [];
  const recentRuns = data.recent_runs ?? [];
  const maxDay = Math.max(1, ...byDay.map((d) => d.count));
  const maxCat = Math.max(1, ...byCategory.map((c) => c.count));

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold">统计</h2>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* By Day */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium">每日文章数（近30天）</CardTitle>
          </CardHeader>
          <CardContent>
            {byDay.length === 0 ? (
              <p className="text-sm text-muted-foreground text-center py-4">暂无数据</p>
            ) : (
              <div className="space-y-2">
                {byDay.slice(0, 15).map((d) => (
                  <div key={d.date} className="flex items-center gap-3">
                    <span className="text-xs text-muted-foreground w-24 shrink-0">{d.date}</span>
                    <div className="flex-1 bg-muted rounded-full h-1.5">
                      <div className="bg-primary h-1.5 rounded-full" style={{ width: `${(d.count / maxDay) * 100}%` }} />
                    </div>
                    <span className="text-xs text-muted-foreground w-6 text-right">{d.count}</span>
                  </div>
                ))}
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
              <div className="space-y-2">
                {byCategory.map((c) => (
                  <div key={c.category} className="flex items-center gap-3">
                    <span className="text-sm w-20 shrink-0">{c.category}</span>
                    <div className="flex-1 bg-muted rounded-full h-1.5">
                      <div className="bg-violet-500 h-1.5 rounded-full" style={{ width: `${(c.count / maxCat) * 100}%` }} />
                    </div>
                    <span className="text-xs text-muted-foreground w-8 text-right">{c.count}</span>
                  </div>
                ))}
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
                    <th className="pb-2 font-medium text-right">耗时</th>
                    <th className="pb-2 font-medium text-right">时间</th>
                  </tr>
                </thead>
                <tbody>
                  {recentRuns.map((r, i) => (
                    <>
                      <tr key={r.run_id} className="text-foreground">
                        <td className="py-2 font-mono">{r.run_id.slice(0, 8)}…</td>
                        <td className="py-2 text-right">{r.total_fetched}</td>
                        <td className="py-2 text-right">{r.total_processed}</td>
                        <td className="py-2 text-right">{r.total_saved}</td>
                        <td className="py-2 text-right">{r.total_published}</td>
                        <td className="py-2 text-right">{(r.duration_ms / 1000).toFixed(1)}s</td>
                        <td className="py-2 text-right text-muted-foreground">{new Date(r.started_at).toLocaleString("zh-CN")}</td>
                      </tr>
                      {i < recentRuns.length - 1 && (
                        <tr key={`sep-${r.run_id}`}><td colSpan={7}><Separator /></td></tr>
                      )}
                    </>
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

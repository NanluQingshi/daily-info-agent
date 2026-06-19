import { useEffect, useState } from "react";
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

  if (loading) return <div className="text-center py-16 text-slate-400">加载中…</div>;
  if (error) return <div className="bg-red-50 text-red-700 text-sm rounded-xl p-4">{error}</div>;
  if (!data) return null;

  const byDay = data.by_day ?? [];
  const byCategory = data.by_category ?? [];
  const recentRuns = data.recent_runs ?? [];

  const maxDay = Math.max(1, ...byDay.map((d) => d.count));
  const maxCat = Math.max(1, ...byCategory.map((c) => c.count));

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold text-slate-900">统计</h2>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* By Day */}
        <div className="bg-white border border-slate-200 rounded-xl p-5">
          <h3 className="text-sm font-medium text-slate-600 mb-4">每日文章数（近30天）</h3>
          {byDay.length === 0 ? (
            <p className="text-sm text-slate-400 text-center py-4">暂无数据</p>
          ) : (
            <div className="space-y-1.5">
              {byDay.slice(0, 15).map((d) => (
                <div key={d.date} className="flex items-center gap-3">
                  <span className="text-xs text-slate-400 w-24 shrink-0">{d.date}</span>
                  <div className="flex-1 bg-slate-100 rounded-full h-2">
                    <div
                      className="bg-blue-500 h-2 rounded-full"
                      style={{ width: `${(d.count / maxDay) * 100}%` }}
                    />
                  </div>
                  <span className="text-xs text-slate-600 w-6 text-right">{d.count}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* By Category */}
        <div className="bg-white border border-slate-200 rounded-xl p-5">
          <h3 className="text-sm font-medium text-slate-600 mb-4">按分类</h3>
          {byCategory.length === 0 ? (
            <p className="text-sm text-slate-400 text-center py-4">暂无数据</p>
          ) : (
            <div className="space-y-2">
              {byCategory.map((c) => (
                <div key={c.category} className="flex items-center gap-3">
                  <span className="text-sm text-slate-600 w-20 shrink-0">{c.category}</span>
                  <div className="flex-1 bg-slate-100 rounded-full h-2">
                    <div
                      className="bg-violet-500 h-2 rounded-full"
                      style={{ width: `${(c.count / maxCat) * 100}%` }}
                    />
                  </div>
                  <span className="text-xs text-slate-600 w-8 text-right">{c.count}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Recent Runs */}
      <div className="bg-white border border-slate-200 rounded-xl p-5 overflow-x-auto">
        <h3 className="text-sm font-medium text-slate-600 mb-4">最近运行记录</h3>
        {recentRuns.length === 0 ? (
          <p className="text-sm text-slate-400 text-center py-4">暂无记录</p>
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="text-slate-400 text-left border-b border-slate-100">
                <th className="pb-2 font-medium">Run ID</th>
                <th className="pb-2 font-medium">抓取</th>
                <th className="pb-2 font-medium">处理</th>
                <th className="pb-2 font-medium">保存</th>
                <th className="pb-2 font-medium">发布</th>
                <th className="pb-2 font-medium">耗时</th>
                <th className="pb-2 font-medium">时间</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-50">
              {recentRuns.map((r) => (
                <tr key={r.run_id} className="text-slate-600">
                  <td className="py-2 font-mono">{r.run_id.slice(0, 8)}…</td>
                  <td className="py-2">{r.total_fetched}</td>
                  <td className="py-2">{r.total_processed}</td>
                  <td className="py-2">{r.total_saved}</td>
                  <td className="py-2">{r.total_published}</td>
                  <td className="py-2">{(r.duration_ms / 1000).toFixed(1)}s</td>
                  <td className="py-2">{new Date(r.started_at).toLocaleString("zh-CN")}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

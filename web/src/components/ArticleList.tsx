import { useCallback, useEffect, useState } from "react";
import { listArticles } from "../api/client";
import type { ArticleFilter, ArticleListResponse, ArticleRow } from "../types";
import { ArticleCard } from "./ArticleCard";
import { ArticleDetail } from "./ArticleDetail";
import { FetchButton } from "./FetchButton";
import { FilterBar } from "./FilterBar";

export function ArticleList() {
  const [filter, setFilter] = useState<ArticleFilter>({ page: 1, page_size: 20 });
  const [data, setData] = useState<ArticleListResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<ArticleRow | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    setError(null);
    listArticles(filter)
      .then(setData)
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  }, [filter]);

  useEffect(() => { load(); }, [load]);

  const handleDeleted = (id: number) => {
    setData((prev) =>
      prev ? { ...prev, articles: prev.articles.filter((a) => a.id !== id), total: prev.total - 1 } : prev
    );
    if (selected?.id === id) setSelected(null);
  };

  const handlePublished = (id: number) => {
    setData((prev) =>
      prev
        ? { ...prev, articles: prev.articles.map((a) => a.id === id ? { ...a, status: "published" as const } : a) }
        : prev
    );
  };

  const totalPages = data?.total_pages ?? 1;
  const currentPage = filter.page ?? 1;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-slate-900">
          文章列表
          {data && <span className="ml-2 text-sm font-normal text-slate-400">共 {data.total} 篇</span>}
        </h2>
        <FetchButton onComplete={load} />
      </div>

      <FilterBar filter={filter} onChange={setFilter} />

      {error && (
        <div className="bg-red-50 border border-red-200 text-red-700 text-sm rounded-xl p-4">
          {error}
        </div>
      )}

      {loading ? (
        <div className="text-center py-16 text-slate-400">加载中…</div>
      ) : data?.articles.length === 0 ? (
        <div className="text-center py-16 text-slate-400">暂无文章</div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          {data?.articles.map((a) => (
            <ArticleCard
              key={a.id}
              article={a}
              onDeleted={handleDeleted}
              onPublished={handlePublished}
              onClick={setSelected}
            />
          ))}
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex justify-center gap-2">
          <button
            onClick={() => setFilter((f) => ({ ...f, page: Math.max(1, (f.page ?? 1) - 1) }))}
            disabled={currentPage <= 1}
            className="px-3 py-1.5 text-sm border border-slate-200 rounded-lg disabled:opacity-40 hover:bg-slate-50 transition-colors"
          >
            上一页
          </button>
          <span className="px-3 py-1.5 text-sm text-slate-500">
            {currentPage} / {totalPages}
          </span>
          <button
            onClick={() => setFilter((f) => ({ ...f, page: Math.min(totalPages, (f.page ?? 1) + 1) }))}
            disabled={currentPage >= totalPages}
            className="px-3 py-1.5 text-sm border border-slate-200 rounded-lg disabled:opacity-40 hover:bg-slate-50 transition-colors"
          >
            下一页
          </button>
        </div>
      )}

      {selected && (
        <ArticleDetail article={selected} onClose={() => setSelected(null)} />
      )}
    </div>
  );
}

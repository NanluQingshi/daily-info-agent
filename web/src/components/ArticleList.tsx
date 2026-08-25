import { useCallback, useEffect, useState } from "react";
import { Check, RefreshCw, Tags } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { batchUpdateTags, listArticles } from "../api/client";
import type { ArticleFilter, ArticleListResponse, ArticleRow } from "../types";
import { ArticleCard } from "./ArticleCard";
import { ArticleDetail } from "./ArticleDetail";
import { FetchButton } from "./FetchButton";
import { FilterBar } from "./FilterBar";
import { showToast } from "./Toast";

export function ArticleList() {
  const [filter, setFilter] = useState<ArticleFilter>({ page: 1, page_size: 20 });
  const [data, setData] = useState<ArticleListResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<ArticleRow | null>(null);
  const [checkedIds, setCheckedIds] = useState<Set<number>>(new Set());
  const [tagInput, setTagInput] = useState("");

  const load = useCallback(() => {
    setLoading(true);
    setError(null);
    listArticles(filter)
      .then(setData)
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  }, [filter]);

  useEffect(() => { load(); }, [load]);

  // Merge an updated article (bookmark/read flags) into list + detail views.
  const handleFlagsUpdated = (updated: ArticleRow) => {
    setData((prev) =>
      prev
        ? { ...prev, articles: prev.articles.map((a) => (a.id === updated.id ? { ...a, ...updated } : a)) }
        : prev
    );
    setSelected((prev) => (prev && prev.id === updated.id ? { ...prev, ...updated } : prev));
  };

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

  const handleRetried = (id: number) => {
    setData((prev) =>
      prev
        ? { ...prev, articles: prev.articles.map((a) => a.id === id ? { ...a, status: "pending" as const } : a) }
        : prev
    );
  };

  // ── Batch selection & tags ────────────────────────────────────────────

  const toggleChecked = (id: number) => {
    setCheckedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const allChecked = data?.articles.length > 0 && data.articles.every((a) => checkedIds.has(a.id));
  const toggleAll = () => {
    const articles = data?.articles ?? [];
    if (allChecked) setCheckedIds(new Set());
    else setCheckedIds(new Set(articles.map((a) => a.id)));
  };

  const applyTags = async (tags: string[]) => {
    const ids = Array.from(checkedIds);
    if (ids.length === 0) {
      showToast("info", "请先选择文章");
      return;
    }
    try {
      const res = await batchUpdateTags(ids, tags);
      showToast("success", `已更新 ${res.updated} 篇文章的标签`);
      // Update the local list optimistically.
      setData((prev) =>
        prev
          ? { ...prev, articles: prev.articles.map((a) => (checkedIds.has(a.id) ? { ...a, tags } : a)) }
          : prev
      );
      setCheckedIds(new Set());
      setTagInput("");
    } catch (e) {
      showToast("error", (e as Error).message);
    }
  };

  const handleTagSubmit = () => {
    const tags = tagInput.split(",").map((t) => t.trim()).filter(Boolean);
    if (tags.length === 0) return;
    applyTags(tags);
  };

  const totalPages = data?.total_pages ?? 1;
  const currentPage = filter.page ?? 1;
  const showBatchBar = checkedIds.size > 0;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-slate-900">
          文章列表
          {data && <span className="ml-2 text-sm font-normal text-slate-400">共 {data.total} 篇</span>}
        </h2>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={load} disabled={loading} className="h-8 px-2">
            <RefreshCw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} />
          </Button>
          <FetchButton onComplete={load} />
        </div>
      </div>

      <FilterBar filter={filter} onChange={setFilter} />

      {error && (
        <div className="bg-red-50 border border-red-200 text-red-700 text-sm rounded-xl p-4 space-y-1">
          <p>{error}</p>
          {error.includes("DATABASE_DSN") && (
            <p className="text-xs text-red-500">
              在 <code className="bg-red-100 px-1 rounded">.env</code> 中配置 DATABASE_DSN 并重启服务端即可启用文章管理功能。
            </p>
          )}
        </div>
      )}

      {/* Batch operations bar */}
      {showBatchBar && (
        <div className="flex flex-wrap items-center gap-2 p-3 bg-card border rounded-xl">
          <button
            onClick={toggleAll}
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <Check className="w-3.5 h-3.5" />
            {allChecked ? "取消全选" : "全选"}
          </button>
          <span className="text-xs text-muted-foreground">已选 {checkedIds.size} 篇</span>
          <div className="flex-1 min-w-48 flex items-center gap-2">
            <Tags className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
            <Input
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") handleTagSubmit(); }}
              placeholder="标签，逗号分隔…"
              className="h-8 flex-1"
            />
            <Button size="sm" className="h-8" onClick={handleTagSubmit} disabled={!tagInput.trim()}>
              打标签
            </Button>
            <Button
              size="sm" variant="outline" className="h-8"
              onClick={() => applyTags([])} disabled={checkedIds.size === 0}
            >
              清除标签
            </Button>
          </div>
          <Button size="sm" variant="ghost" className="h-8 text-muted-foreground" onClick={() => setCheckedIds(new Set())}>
            取消
          </Button>
        </div>
      )}

      {loading ? (
        <div className="text-center py-16 text-slate-400">加载中…</div>
      ) : (data?.articles?.length ?? 0) === 0 ? (
        <div className="text-center py-16 text-slate-400">暂无文章</div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          {data?.articles?.map((a) => (
            <div key={a.id} className="relative">
              {checkedIds.has(a.id) && (
                <button
                  onClick={() => toggleChecked(a.id)}
                  className="absolute -top-2 -right-2 z-10 w-6 h-6 rounded-full bg-primary text-primary-foreground flex items-center justify-center shadow"
                  title="取消选择"
                >
                  <Check className="w-3.5 h-3.5" />
                </button>
              )}
              <ArticleCard
                article={a}
                onDeleted={handleDeleted}
                onPublished={handlePublished}
                onRetried={handleRetried}
                onFlagsUpdated={handleFlagsUpdated}
                onClick={setSelected}
                onToggleSelect={() => toggleChecked(a.id)}
                checked={checkedIds.has(a.id)}
              />
            </div>
          ))}
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex justify-center items-center gap-2">
          <Button
            variant="outline" size="sm"
            onClick={() => setFilter((f) => ({ ...f, page: Math.max(1, (f.page ?? 1) - 1) }))}
            disabled={currentPage <= 1}
          >
            上一页
          </Button>
          <span className="text-sm text-muted-foreground px-2">{currentPage} / {totalPages}</span>
          <Button
            variant="outline" size="sm"
            onClick={() => setFilter((f) => ({ ...f, page: Math.min(totalPages, (f.page ?? 1) + 1) }))}
            disabled={currentPage >= totalPages}
          >
            下一页
          </Button>
        </div>
      )}

      {selected && (
        <ArticleDetail
          article={selected}
          onClose={() => setSelected(null)}
          onPublished={handlePublished}
          onDeleted={handleDeleted}
          onRetried={handleRetried}
        />
      )}
    </div>
  );
}

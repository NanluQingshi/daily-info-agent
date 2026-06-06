import { useState } from "react";
import { deleteArticle, publishArticle, retryArticle } from "../api/client";
import type { ArticleRow } from "../types";

interface Props {
  article: ArticleRow;
  onDeleted: (id: number) => void;
  onPublished: (id: number) => void;
  onRetried: (id: number) => void;
  onClick: (article: ArticleRow) => void;
}

const STATUS_COLORS: Record<string, string> = {
  pending: "bg-yellow-100 text-yellow-800",
  published: "bg-green-100 text-green-800",
  skipped: "bg-slate-100 text-slate-600",
  failed: "bg-red-100 text-red-700",
};

const STATUS_LABELS: Record<string, string> = {
  pending: "待发布",
  published: "已发布",
  skipped: "已跳过",
  failed: "失败",
};

export function ArticleCard({ article, onDeleted, onPublished, onRetried, onClick }: Props) {
  const [busy, setBusy] = useState(false);

  const handleRetry = async (e: React.MouseEvent) => {
    e.stopPropagation();
    setBusy(true);
    try {
      await retryArticle(article.id);
      onRetried(article.id);
    } catch (err) {
      alert((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handlePublish = async (e: React.MouseEvent) => {
    e.stopPropagation();
    setBusy(true);
    try {
      await publishArticle(article.id);
      onPublished(article.id);
    } catch (err) {
      alert((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!confirm("确定删除这篇文章？")) return;
    setBusy(true);
    try {
      await deleteArticle(article.id);
      onDeleted(article.id);
    } catch (err) {
      alert((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const score = article.credibility_score;
  const scoreColor = score >= 0.8 ? "text-green-600" : score >= 0.5 ? "text-yellow-600" : "text-red-500";

  return (
    <div
      onClick={() => onClick(article)}
      className="bg-white border border-slate-200 rounded-xl p-4 hover:border-blue-300 hover:shadow-sm cursor-pointer transition-all"
    >
      <div className="flex items-start justify-between gap-2 mb-2">
        <h3 className="text-sm font-medium text-slate-900 line-clamp-2 flex-1">
          {article.title || "(无标题)"}
        </h3>
        <span className={`text-xs px-2 py-0.5 rounded-full font-medium shrink-0 ${STATUS_COLORS[article.status] ?? "bg-slate-100"}`}>
          {STATUS_LABELS[article.status] ?? article.status}
        </span>
      </div>

      {article.summary && (
        <p className="text-xs text-slate-500 line-clamp-2 mb-3">{article.summary}</p>
      )}

      <div className="flex items-center gap-3 text-xs text-slate-400">
        <span className="truncate">{article.source_domain}</span>
        <span>{article.category}</span>
        <span className={`font-medium ${scoreColor}`}>
          {score.toFixed(2)}
        </span>
        <span className="ml-auto">{new Date(article.created_at).toLocaleDateString("zh-CN")}</span>
      </div>

      <div className="flex gap-2 mt-3">
        {article.status === "pending" && (
          <button
            onClick={handlePublish}
            disabled={busy}
            className="text-xs px-3 py-1 bg-blue-50 text-blue-700 rounded-lg hover:bg-blue-100 disabled:opacity-50 transition-colors"
          >
            发布
          </button>
        )}
        {article.status === "failed" && (
          <button
            onClick={handleRetry}
            disabled={busy}
            className="text-xs px-3 py-1 bg-orange-50 text-orange-700 rounded-lg hover:bg-orange-100 disabled:opacity-50 transition-colors"
          >
            重试
          </button>
        )}
        <button
          onClick={handleDelete}
          disabled={busy}
          className="text-xs px-3 py-1 bg-slate-50 text-slate-500 rounded-lg hover:bg-red-50 hover:text-red-600 disabled:opacity-50 transition-colors"
        >
          删除
        </button>
      </div>
    </div>
  );
}

import { useEffect, useState } from "react";
import { deleteArticle, publishArticle, retryArticle } from "../api/client";
import type { ArticleRow } from "../types";

interface Props {
  article: ArticleRow;
  onClose: () => void;
  onPublished: (id: number) => void;
  onDeleted: (id: number) => void;
  onRetried: (id: number) => void;
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

export function ArticleDetail({ article, onClose, onPublished, onDeleted, onRetried }: Props) {
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState(article.status);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const handlePublish = async () => {
    setBusy(true);
    try {
      await publishArticle(article.id);
      setStatus("published");
      onPublished(article.id);
    } catch (err) {
      alert((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async () => {
    if (!confirm("确定删除这篇文章？")) return;
    setBusy(true);
    try {
      await deleteArticle(article.id);
      onDeleted(article.id);
      onClose();
    } catch (err) {
      alert((err as Error).message);
      setBusy(false);
    }
  };

  const handleRetry = async () => {
    setBusy(true);
    try {
      await retryArticle(article.id);
      setStatus("pending");
      onRetried(article.id);
    } catch (err) {
      alert((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const score = article.credibility_score;
  const scorePct = Math.round(score * 100);
  const scoreColor =
    score >= 0.8 ? "bg-green-500" : score >= 0.5 ? "bg-yellow-400" : "bg-red-400";
  const scoreTextColor =
    score >= 0.8 ? "text-green-700" : score >= 0.5 ? "text-yellow-700" : "text-red-600";

  const showDescription =
    article.description &&
    article.description.trim() !== article.summary?.trim();

  return (
    <div
      className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4"
      onClick={onClose}
    >
      <div
        className="bg-white rounded-2xl max-w-2xl w-full max-h-[88vh] flex flex-col shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Sticky header */}
        <div className="sticky top-0 bg-white border-b border-slate-100 px-6 py-4 flex items-start justify-between gap-3 rounded-t-2xl shrink-0">
          <h2 className="text-base font-semibold text-slate-900 leading-snug">
            {article.title || "(无标题)"}
          </h2>
          <button
            onClick={onClose}
            className="text-slate-400 hover:text-slate-600 shrink-0 text-xl leading-none mt-0.5"
            aria-label="关闭"
          >
            ×
          </button>
        </div>

        {/* Scrollable body */}
        <div className="overflow-y-auto flex-1 px-6 py-5 space-y-5">

          {/* Meta badges */}
          <div className="flex flex-wrap gap-2 text-xs">
            <span className="bg-blue-50 text-blue-700 px-2.5 py-1 rounded-lg font-medium">
              {article.category}
            </span>
            <span className="bg-slate-100 text-slate-600 px-2.5 py-1 rounded-lg">
              {article.source_domain}
            </span>
            <span
              className={`px-2.5 py-1 rounded-lg font-medium ${STATUS_COLORS[status] ?? "bg-slate-100 text-slate-600"}`}
            >
              {STATUS_LABELS[status] ?? status}
            </span>
            {article.domain_hit && (
              <span className="bg-emerald-50 text-emerald-700 px-2.5 py-1 rounded-lg">
                白名单来源
              </span>
            )}
          </div>

          {/* Credibility score bar */}
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <p className="text-xs font-medium text-slate-400 uppercase tracking-wide">可信度</p>
              <span className={`text-xs font-semibold ${scoreTextColor}`}>{scorePct}%</span>
            </div>
            <div className="h-1.5 bg-slate-100 rounded-full overflow-hidden">
              <div
                className={`h-full rounded-full transition-all ${scoreColor}`}
                style={{ width: `${scorePct}%` }}
              />
            </div>
          </div>

          {/* AI summary */}
          {article.summary && (
            <div>
              <p className="text-xs font-medium text-slate-400 uppercase tracking-wide mb-1.5">
                AI 摘要
              </p>
              <p className="text-sm text-slate-700 leading-relaxed">{article.summary}</p>
            </div>
          )}

          {/* Raw description (only when different from summary) */}
          {showDescription && (
            <div>
              <p className="text-xs font-medium text-slate-400 uppercase tracking-wide mb-1.5">
                原文摘要
              </p>
              <p className="text-sm text-slate-500 leading-relaxed">{article.description}</p>
            </div>
          )}

          {/* Tags */}
          {article.tags?.length > 0 && (
            <div>
              <p className="text-xs font-medium text-slate-400 uppercase tracking-wide mb-2">
                标签
              </p>
              <div className="flex flex-wrap gap-1.5">
                {article.tags.map((t) => (
                  <span
                    key={t}
                    className="text-xs bg-slate-100 text-slate-600 px-2.5 py-0.5 rounded-full"
                  >
                    {t}
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* Metadata footer */}
          <div className="text-xs text-slate-400 space-y-1 pt-4 border-t border-slate-100">
            <div>
              来源:{" "}
              <a
                href={article.source_url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-600 hover:underline break-all"
              >
                {article.source_url}
              </a>
            </div>
            {article.published_at && (
              <div>发布时间: {new Date(article.published_at).toLocaleString("zh-CN")}</div>
            )}
            <div>抓取时间: {new Date(article.fetched_at).toLocaleString("zh-CN")}</div>
            <div>
              run_id: <span className="font-mono text-slate-500">{article.run_id}</span>
            </div>
          </div>
        </div>

        {/* Action bar */}
        <div className="shrink-0 border-t border-slate-100 px-6 py-4 flex items-center justify-end gap-2 rounded-b-2xl bg-slate-50">
          <button
            onClick={handleDelete}
            disabled={busy}
            className="text-sm px-4 py-1.5 border border-slate-200 text-slate-500 rounded-lg hover:bg-red-50 hover:text-red-600 hover:border-red-200 disabled:opacity-40 transition-colors"
          >
            删除
          </button>
          {status === "failed" && (
            <button
              onClick={handleRetry}
              disabled={busy}
              className="text-sm px-4 py-1.5 bg-orange-500 text-white rounded-lg hover:bg-orange-600 disabled:opacity-40 transition-colors"
            >
              {busy ? "重置中…" : "重试"}
            </button>
          )}
          {status === "pending" && (
            <button
              onClick={handlePublish}
              disabled={busy}
              className="text-sm px-4 py-1.5 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-40 transition-colors"
            >
              {busy ? "发布中…" : "发布"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

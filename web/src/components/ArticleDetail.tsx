import type { ArticleRow } from "../types";

interface Props {
  article: ArticleRow;
  onClose: () => void;
}

export function ArticleDetail({ article, onClose }: Props) {
  return (
    <div
      className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4"
      onClick={onClose}
    >
      <div
        className="bg-white rounded-2xl max-w-2xl w-full max-h-[85vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="sticky top-0 bg-white border-b border-slate-100 px-6 py-4 flex items-start justify-between gap-3">
          <h2 className="text-base font-semibold text-slate-900 leading-snug">{article.title}</h2>
          <button
            onClick={onClose}
            className="text-slate-400 hover:text-slate-600 shrink-0 text-xl leading-none"
          >
            ×
          </button>
        </div>

        <div className="p-6 space-y-4">
          <div className="flex flex-wrap gap-2 text-xs">
            <span className="bg-blue-50 text-blue-700 px-2 py-1 rounded-lg">{article.category}</span>
            <span className="bg-slate-100 text-slate-600 px-2 py-1 rounded-lg">{article.source_domain}</span>
            <span className="bg-slate-100 text-slate-600 px-2 py-1 rounded-lg">
              可信度 {article.credibility_score.toFixed(2)}
            </span>
            <span className="bg-slate-100 text-slate-600 px-2 py-1 rounded-lg">{article.status}</span>
          </div>

          {article.summary && (
            <div>
              <p className="text-xs font-medium text-slate-400 uppercase mb-1">AI 摘要</p>
              <p className="text-sm text-slate-700 leading-relaxed">{article.summary}</p>
            </div>
          )}

          {article.tags.length > 0 && (
            <div>
              <p className="text-xs font-medium text-slate-400 uppercase mb-2">标签</p>
              <div className="flex flex-wrap gap-1.5">
                {article.tags.map((t) => (
                  <span key={t} className="text-xs bg-slate-100 text-slate-600 px-2 py-0.5 rounded-full">
                    {t}
                  </span>
                ))}
              </div>
            </div>
          )}

          <div className="text-xs text-slate-400 space-y-1 pt-2 border-t border-slate-100">
            <div>来源: <a href={article.source_url} target="_blank" rel="noopener noreferrer" className="text-blue-600 hover:underline break-all">{article.source_url}</a></div>
            {article.published_at && <div>发布时间: {new Date(article.published_at).toLocaleString("zh-CN")}</div>}
            <div>抓取时间: {new Date(article.fetched_at).toLocaleString("zh-CN")}</div>
            <div>run_id: <span className="font-mono">{article.run_id}</span></div>
          </div>
        </div>
      </div>
    </div>
  );
}

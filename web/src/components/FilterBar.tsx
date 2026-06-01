import type { ArticleFilter } from "../types";
import { CATEGORIES, STATUSES } from "../types";

interface Props {
  filter: ArticleFilter;
  onChange: (f: ArticleFilter) => void;
}

const STATUS_LABELS: Record<string, string> = {
  pending: "待发布",
  published: "已发布",
  skipped: "已跳过",
  failed: "失败",
};

export function FilterBar({ filter, onChange }: Props) {
  const set = (patch: Partial<ArticleFilter>) =>
    onChange({ ...filter, ...patch, page: 1 });

  return (
    <div className="flex flex-wrap gap-3 p-4 bg-white border border-slate-200 rounded-xl">
      <div className="flex items-center gap-2">
        <label className="text-sm text-slate-500 whitespace-nowrap">分类</label>
        <select
          value={filter.category ?? ""}
          onChange={(e) => set({ category: e.target.value || undefined })}
          className="text-sm border border-slate-200 rounded-lg px-3 py-1.5 bg-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          <option value="">全部</option>
          {CATEGORIES.map((c) => (
            <option key={c} value={c}>{c}</option>
          ))}
        </select>
      </div>

      <div className="flex items-center gap-2">
        <label className="text-sm text-slate-500 whitespace-nowrap">状态</label>
        <select
          value={filter.status ?? ""}
          onChange={(e) => set({ status: e.target.value || undefined })}
          className="text-sm border border-slate-200 rounded-lg px-3 py-1.5 bg-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          <option value="">全部</option>
          {STATUSES.map((s) => (
            <option key={s} value={s}>{STATUS_LABELS[s]}</option>
          ))}
        </select>
      </div>

      <div className="flex items-center gap-2">
        <label className="text-sm text-slate-500 whitespace-nowrap">从</label>
        <input
          type="date"
          value={filter.date_from ?? ""}
          onChange={(e) => set({ date_from: e.target.value || undefined })}
          className="text-sm border border-slate-200 rounded-lg px-3 py-1.5 bg-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>

      <div className="flex items-center gap-2">
        <label className="text-sm text-slate-500 whitespace-nowrap">到</label>
        <input
          type="date"
          value={filter.date_to ?? ""}
          onChange={(e) => set({ date_to: e.target.value || undefined })}
          className="text-sm border border-slate-200 rounded-lg px-3 py-1.5 bg-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>

      <button
        onClick={() => onChange({ page: 1, page_size: filter.page_size })}
        className="text-sm text-slate-500 hover:text-slate-700 px-2 py-1 rounded transition-colors"
      >
        清除筛选
      </button>
    </div>
  );
}

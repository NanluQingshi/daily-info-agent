import { useEffect, useRef, useState } from "react";
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

  // Local input value for search — debounced before committing to filter
  const [searchInput, setSearchInput] = useState(filter.q ?? "");
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Sync if filter.q is cleared externally (e.g. "清除筛选")
  useEffect(() => {
    if (!filter.q) setSearchInput("");
  }, [filter.q]);

  const handleSearchChange = (value: string) => {
    setSearchInput(value);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      set({ q: value || undefined });
    }, 350);
  };

  return (
    <div className="flex flex-wrap gap-3 p-4 bg-white border border-slate-200 rounded-xl">
      {/* Keyword search */}
      <div className="flex items-center gap-2 flex-1 min-w-40">
        <label className="text-sm text-slate-500 whitespace-nowrap">搜索</label>
        <div className="relative flex-1">
          <input
            type="text"
            value={searchInput}
            onChange={(e) => handleSearchChange(e.target.value)}
            placeholder="标题或摘要关键词…"
            className="w-full text-sm border border-slate-200 rounded-lg px-3 py-1.5 pr-7 bg-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          {searchInput && (
            <button
              onClick={() => handleSearchChange("")}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600 text-base leading-none"
              aria-label="清除搜索"
            >
              ×
            </button>
          )}
        </div>
      </div>

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
        onClick={() => {
          setSearchInput("");
          onChange({ page: 1, page_size: filter.page_size });
        }}
        className="text-sm text-slate-500 hover:text-slate-700 px-2 py-1 rounded transition-colors"
      >
        清除筛选
      </button>
    </div>
  );
}

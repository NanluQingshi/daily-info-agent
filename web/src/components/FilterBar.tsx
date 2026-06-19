import { useRef, useState } from "react";
import { Search, X } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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

  const [searchInput, setSearchInput] = useState(filter.q ?? "");
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const handleSearchChange = (value: string) => {
    setSearchInput(value);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      set({ q: value || undefined });
    }, 350);
  };

  return (
    <div className="flex flex-wrap gap-2 p-3 bg-card border rounded-xl">
      {/* Search */}
      <div className="relative flex-1 min-w-40">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
        <Input
          value={searchInput}
          onChange={(e) => handleSearchChange(e.target.value)}
          placeholder="搜索标题或摘要…"
          className="pl-9 pr-8 h-9"
        />
        {searchInput && (
          <button
            onClick={() => handleSearchChange("")}
            className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        )}
      </div>

      {/* Category */}
      <Select
        value={filter.category ?? "all"}
        onValueChange={(v) => set({ category: v === "all" ? undefined : v })}
      >
        <SelectTrigger className="w-32 h-9">
          <SelectValue placeholder="全部分类" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">全部分类</SelectItem>
          {CATEGORIES.map((c) => (
            <SelectItem key={c} value={c}>{c}</SelectItem>
          ))}
        </SelectContent>
      </Select>

      {/* Status */}
      <Select
        value={filter.status ?? "all"}
        onValueChange={(v) => set({ status: v === "all" ? undefined : v })}
      >
        <SelectTrigger className="w-28 h-9">
          <SelectValue placeholder="全部状态" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">全部状态</SelectItem>
          {STATUSES.map((s) => (
            <SelectItem key={s} value={s}>{STATUS_LABELS[s]}</SelectItem>
          ))}
        </SelectContent>
      </Select>

      {/* Date range */}
      <Input
        type="date"
        value={filter.date_from ?? ""}
        onChange={(e) => set({ date_from: e.target.value || undefined })}
        className="w-36 h-9"
        placeholder="开始日期"
      />
      <Input
        type="date"
        value={filter.date_to ?? ""}
        onChange={(e) => set({ date_to: e.target.value || undefined })}
        className="w-36 h-9"
        placeholder="结束日期"
      />

      <Button
        variant="ghost"
        size="sm"
        className="h-9 text-muted-foreground"
        onClick={() => {
          setSearchInput("");
          onChange({ page: 1, page_size: filter.page_size });
        }}
      >
        清除
      </Button>
    </div>
  );
}

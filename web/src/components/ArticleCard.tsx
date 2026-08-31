import { useState } from "react";
import { Bookmark, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { deleteArticle, publishArticle, retryArticle, updateArticleFlags } from "../api/client";
import type { ArticleRow } from "../types";
import { showToast } from "./Toast";

interface Props {
  article: ArticleRow;
  onDeleted: (id: number) => void;
  onPublished: (id: number) => void;
  onRetried: (id: number) => void;
  onClick: (article: ArticleRow) => void;
  onToggleSelect?: () => void;
  checked?: boolean;
  onFlagsUpdated?: (article: ArticleRow) => void;
}

const STATUS_LABELS: Record<string, string> = {
  pending: "待发布",
  published: "已发布",
  skipped: "已跳过",
  failed: "失败",
};

const STATUS_VARIANT: Record<string, "default" | "secondary" | "outline" | "destructive"> = {
  pending: "outline",
  published: "default",
  skipped: "secondary",
  failed: "destructive",
};

export function ArticleCard({ article, onDeleted, onPublished, onRetried, onClick, onToggleSelect, checked, onFlagsUpdated }: Props) {
  const [busy, setBusy] = useState(false);
  const isRead = !!article.read_at;

  const handlePublish = async (e: React.MouseEvent) => {
    e.stopPropagation();
    setBusy(true);
    try {
      await publishArticle(article.id);
      onPublished(article.id);
    } catch (err) {
      showToast("error", (err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleRetry = async (e: React.MouseEvent) => {
    e.stopPropagation();
    setBusy(true);
    try {
      await retryArticle(article.id);
      onRetried(article.id);
    } catch (err) {
      showToast("error", (err as Error).message);
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
      showToast("error", (err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleBookmark = async (e: React.MouseEvent) => {
    e.stopPropagation();
    setBusy(true);
    try {
      const updated = await updateArticleFlags(article.id, { bookmarked: !article.bookmarked });
      onFlagsUpdated?.(updated);
    } catch (err) {
      showToast("error", (err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleMarkRead = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (isRead) return; // already read — clicking through is idempotent
    setBusy(true);
    try {
      const updated = await updateArticleFlags(article.id, { read: true });
      onFlagsUpdated?.(updated);
    } catch (err) {
      showToast("error", (err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const score = article.credibility_score;
  const scoreColor = score >= 0.8 ? "text-green-600" : score >= 0.5 ? "text-yellow-600" : "text-red-500";

  return (
    <div
      onClick={() => onClick(article)}
      className={`bg-card border rounded-xl p-4 hover:border-primary/40 hover:shadow-sm cursor-pointer transition-all ${
        isRead ? "opacity-60" : ""
      }`}
    >
      <div className="flex items-start justify-between gap-2 mb-2">
        <h3 className={`text-sm line-clamp-2 flex-1 ${isRead ? "font-normal" : "font-medium"}`}>
          {article.bookmarked && (
            <Bookmark className="inline-block w-3.5 h-3.5 mr-1 align-[-2px] fill-amber-500 text-amber-500" />
          )}
          {article.title || "(无标题)"}
        </h3>
        <div className="flex items-center gap-2 shrink-0">
          {onToggleSelect && (
            <input
              type="checkbox"
              checked={!!checked}
              onChange={onToggleSelect}
              onClick={(e) => e.stopPropagation()}
              className="w-4 h-4 accent-primary cursor-pointer"
              aria-label="选择文章"
            />
          )}
          <Badge variant={STATUS_VARIANT[article.status] ?? "outline"} className="text-xs">
            {STATUS_LABELS[article.status] ?? article.status}
          </Badge>
        </div>
      </div>

      {article.summary && (
        <p className="text-xs text-muted-foreground line-clamp-2 mb-3">{article.summary}</p>
      )}

      <div className="flex items-center gap-3 text-xs text-muted-foreground mb-3">
        <span className="truncate">{article.source_domain}</span>
        <span>{article.category}</span>
        <span className={`font-medium ${scoreColor}`}>{score.toFixed(2)}</span>
        <span className="ml-auto">{new Date(article.created_at).toLocaleDateString("zh-CN")}</span>
      </div>

      <div className="flex gap-2" onClick={(e) => e.stopPropagation()}>
        {article.status === "pending" && (
          <Button size="sm" variant="outline" onClick={handlePublish} disabled={busy} className="h-7 text-xs">
            发布
          </Button>
        )}
        {article.status === "failed" && (
          <Button size="sm" variant="outline" onClick={handleRetry} disabled={busy} className="h-7 text-xs">
            重试
          </Button>
        )}
        <Button
          size="sm"
          variant="ghost"
          onClick={handleBookmark}
          disabled={busy}
          className={`h-7 text-xs ml-auto ${article.bookmarked ? "text-amber-500 hover:text-amber-600" : "text-muted-foreground hover:text-amber-500"}`}
          title={article.bookmarked ? "取消收藏" : "收藏"}
        >
          <Bookmark className={`w-3.5 h-3.5 ${article.bookmarked ? "fill-current" : ""}`} />
        </Button>
        {!isRead && (
          <Button
            size="sm"
            variant="ghost"
            onClick={handleMarkRead}
            disabled={busy}
            className="h-7 text-xs text-muted-foreground hover:text-foreground"
            title="标记已读"
          >
            已读
          </Button>
        )}
        <Button size="sm" variant="ghost" onClick={handleDelete} disabled={busy} className="h-7 text-xs text-muted-foreground hover:text-destructive">
          <Trash2 className="w-3.5 h-3.5" />
        </Button>
      </div>
    </div>
  );
}

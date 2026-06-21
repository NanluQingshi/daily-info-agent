import { useEffect, useState } from "react";
import { ExternalLink, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { deleteArticle, publishArticle, retryArticle } from "../api/client";
import type { ArticleRow } from "../types";

interface Props {
  article: ArticleRow;
  onClose: () => void;
  onPublished: (id: number) => void;
  onDeleted: (id: number) => void;
  onRetried: (id: number) => void;
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
  const scoreColor = score >= 0.8 ? "bg-green-500" : score >= 0.5 ? "bg-yellow-400" : "bg-red-400";
  const scoreTextColor = score >= 0.8 ? "text-green-600" : score >= 0.5 ? "text-yellow-600" : "text-red-500";
  const showDescription = article.description && article.description.trim() !== article.summary?.trim();

  return (
    <div
      className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4"
      onClick={onClose}
    >
      <div
        className="bg-card rounded-2xl max-w-2xl w-full max-h-[88vh] flex flex-col shadow-2xl border"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="shrink-0 px-6 py-4 flex items-start justify-between gap-3 border-b">
          <h2 className="text-sm font-semibold leading-snug">{article.title || "(无标题)"}</h2>
          <Button variant="ghost" size="icon" onClick={onClose} className="w-7 h-7 shrink-0 -mt-0.5">
            <X className="w-4 h-4" />
          </Button>
        </div>

        {/* Body */}
        <ScrollArea className="flex-1 px-6 py-5">
          <div className="space-y-5">
            {/* Badges */}
            <div className="flex flex-wrap gap-2">
              <Badge variant="secondary">{article.category}</Badge>
              <Badge variant="outline">{article.source_domain}</Badge>
              <Badge variant={STATUS_VARIANT[status] ?? "outline"}>
                {STATUS_LABELS[status] ?? status}
              </Badge>
              {article.domain_hit && <Badge variant="outline" className="text-green-700 border-green-200 bg-green-50">白名单</Badge>}
            </div>

            {/* Credibility */}
            <div>
              <div className="flex items-center justify-between mb-1.5">
                <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">可信度</p>
                <span className={`text-xs font-semibold ${scoreTextColor}`}>{scorePct}%</span>
              </div>
              <div className="h-1.5 bg-muted rounded-full overflow-hidden">
                <div className={`h-full rounded-full ${scoreColor}`} style={{ width: `${scorePct}%` }} />
              </div>
            </div>

            {/* AI summary */}
            {article.summary && (
              <div>
                <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-1.5">AI 摘要</p>
                <p className="text-sm leading-relaxed">{article.summary}</p>
              </div>
            )}

            {/* Raw description */}
            {showDescription && (
              <div>
                <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-1.5">原文摘要</p>
                <p className="text-sm text-muted-foreground leading-relaxed">{article.description}</p>
              </div>
            )}

            {/* Tags */}
            {article.tags?.length > 0 && (
              <div>
                <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-2">标签</p>
                <div className="flex flex-wrap gap-1.5">
                  {article.tags.map((t) => <Badge key={t} variant="secondary" className="text-xs font-normal">{t}</Badge>)}
                </div>
              </div>
            )}

            <Separator />

            {/* Metadata */}
            <div className="text-xs text-muted-foreground space-y-1.5">
              <div className="flex items-center gap-1.5">
                <span>来源:</span>
                <a
                  href={article.source_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-primary hover:underline truncate flex items-center gap-1"
                >
                  {article.source_url}
                  <ExternalLink className="w-3 h-3 shrink-0" />
                </a>
              </div>
              {article.published_at && <div>发布时间: {new Date(article.published_at).toLocaleString("zh-CN")}</div>}
              <div>抓取时间: {new Date(article.fetched_at).toLocaleString("zh-CN")}</div>
              <div>run_id: <span className="font-mono">{article.run_id}</span></div>
            </div>
          </div>
        </ScrollArea>

        {/* Action bar */}
        <div className="shrink-0 border-t px-6 py-3 flex items-center justify-end gap-2 bg-muted/30 rounded-b-2xl">
          <Button variant="outline" size="sm" onClick={handleDelete} disabled={busy}
            className="text-muted-foreground hover:text-destructive hover:border-destructive">
            删除
          </Button>
          {status === "failed" && (
            <Button variant="outline" size="sm" onClick={handleRetry} disabled={busy}>
              {busy ? "重置中…" : "重试"}
            </Button>
          )}
          {status === "pending" && (
            <Button size="sm" onClick={handlePublish} disabled={busy}>
              {busy ? "发布中…" : "发布"}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

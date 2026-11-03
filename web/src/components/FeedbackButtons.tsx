import { useEffect, useState } from "react";
import { ThumbsDown, ThumbsUp } from "lucide-react";
import { Button } from "@/components/ui/button";
import { getFeedback, submitFeedback, type FeedbackKind } from "../api/client";
import type { ArticleFeedbackRow } from "../api/client";
import { showToast } from "./Toast";

const KIND_LABELS: Record<FeedbackKind, string> = {
  summary: "AI 摘要",
  category: "分类",
};

interface Props {
  articleId: number;
  kind: FeedbackKind;
}

/** 👍/👎 on one AI-generated aspect; the stored rating is echoed back. */
export function FeedbackButtons({ articleId, kind }: Props) {
  const [current, setCurrent] = useState<1 | -1 | null>(null);
  const [busy, setBusy] = useState(false);

  // Load existing feedback so the UI echoes the persisted state
  // (survives refresh/restart, per #61).
  useEffect(() => {
    getFeedback(articleId)
      .then((resp) => {
        const mine = resp.feedback.find((f) => f.kind === kind);
        if (mine) setCurrent(mine.rating);
      })
      .catch(() => {
        /* feedback is optional decoration — ignore load errors */
      });
  }, [articleId, kind]);

  const rate = async (rating: 1 | -1) => {
    setBusy(true);
    try {
      const row = await submitFeedback(articleId, kind, rating);
      setCurrent(row.rating);
      showToast("success", `已记录对「${KIND_LABELS[kind]}」的评价`);
    } catch (err) {
      showToast("error", (err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const btn = (rating: 1 | -1, Icon: typeof ThumbsUp, label: string) => (
    <Button
      size="sm"
      variant={current === rating ? "default" : "outline"}
      onClick={() => rate(rating)}
      disabled={busy}
      className="h-7 text-xs gap-1"
      title={current === rating ? "已评价" : label}
    >
      <Icon className="w-3.5 h-3.5" />
      {label}
    </Button>
  );

  return (
    <div className="flex items-center gap-2">
      <span className="text-xs text-muted-foreground">{KIND_LABELS[kind]}有帮助吗？</span>
      {btn(1, ThumbsUp, "有帮助")}
      {btn(-1, ThumbsDown, "待改进")}
    </div>
  );
}

export type { ArticleFeedbackRow };

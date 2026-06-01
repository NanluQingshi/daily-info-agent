import { useState } from "react";
import { triggerFetch } from "../api/client";

interface Props {
  onComplete?: () => void;
}

export function FetchButton({ onComplete }: Props) {
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  const handleClick = async () => {
    setLoading(true);
    setMessage(null);
    try {
      const res = await triggerFetch();
      setMessage(res.triggered ? `已触发抓取，run_id: ${res.run_id.slice(0, 8)}…` : res.message);
      if (res.triggered) onComplete?.();
    } catch (e: unknown) {
      setMessage((e as Error).message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex items-center gap-3">
      <button
        onClick={handleClick}
        disabled={loading}
        className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
      >
        {loading ? "抓取中…" : "立即抓取"}
      </button>
      {message && (
        <span className="text-sm text-slate-600">{message}</span>
      )}
    </div>
  );
}

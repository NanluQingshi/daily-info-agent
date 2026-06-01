import { useState } from "react";
import { sendChat } from "../api/client";
import type { ChatResponse } from "../types";

export function ChatPanel() {
  const [message, setMessage] = useState("");
  const [result, setResult] = useState<ChatResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSend = async () => {
    if (!message.trim()) return;
    setLoading(true);
    setError(null);
    setResult(null);
    try {
      const res = await sendChat(message.trim());
      setResult(res);
    } catch (e: unknown) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="space-y-4 max-w-2xl mx-auto">
      <h2 className="text-lg font-semibold text-slate-900">智能问答</h2>

      <div className="flex gap-2">
        <input
          type="text"
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && !loading && handleSend()}
          placeholder="输入问题，例如：最新科技AI新闻"
          className="flex-1 border border-slate-200 rounded-xl px-4 py-2.5 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <button
          onClick={handleSend}
          disabled={loading || !message.trim()}
          className="px-5 py-2.5 bg-blue-600 text-white rounded-xl text-sm font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          {loading ? "查询中…" : "查询"}
        </button>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 text-red-700 text-sm rounded-xl p-4">
          {error}
        </div>
      )}

      {result && (
        <div className="bg-white border border-slate-200 rounded-xl p-5 space-y-4">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-xs bg-blue-50 text-blue-700 px-2 py-1 rounded-lg">{result.category}</span>
            <span className="text-xs text-slate-400">{result.extracted_topic}</span>
            <span className="text-xs text-slate-300 ml-auto">{result.latency_ms}ms</span>
          </div>

          <p className="text-sm text-slate-700 leading-relaxed">{result.summary}</p>

          {result.sources.length > 0 && (
            <div>
              <p className="text-xs font-medium text-slate-400 uppercase mb-2">来源</p>
              <div className="space-y-2">
                {result.sources.map((s) => (
                  <a
                    key={s.url}
                    href={s.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="block text-sm p-3 border border-slate-100 rounded-lg hover:border-blue-200 hover:bg-blue-50 transition-colors"
                  >
                    <div className="font-medium text-slate-800 text-xs line-clamp-1">{s.title}</div>
                    <div className="flex gap-2 mt-1 text-xs text-slate-400">
                      <span>{s.source_domain}</span>
                      <span>可信度 {s.credibility_score.toFixed(2)}</span>
                    </div>
                  </a>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

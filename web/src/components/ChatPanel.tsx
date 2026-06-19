import { useEffect, useRef, useState } from "react";
import { sendChat } from "../api/client";
import type { ChatResponse } from "../types";

interface Message {
  id: number;
  role: "user" | "assistant";
  text: string;
  response?: ChatResponse;
  error?: string;
}

let nextId = 0;

export function ChatPanel() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, loading]);

  const handleSend = async () => {
    const text = input.trim();
    if (!text || loading) return;

    setMessages((prev) => [...prev, { id: ++nextId, role: "user", text }]);
    setInput("");
    setLoading(true);

    // reset textarea height
    if (textareaRef.current) textareaRef.current.style.height = "auto";

    try {
      const res = await sendChat(text);
      setMessages((prev) => [
        ...prev,
        { id: ++nextId, role: "assistant", text: res.summary, response: res },
      ]);
    } catch (e: unknown) {
      setMessages((prev) => [
        ...prev,
        { id: ++nextId, role: "assistant", text: "", error: (e as Error).message },
      ]);
    } finally {
      setLoading(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInput(e.target.value);
    // auto-grow textarea
    e.target.style.height = "auto";
    e.target.style.height = `${Math.min(e.target.scrollHeight, 128)}px`;
  };

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="h-14 shrink-0 border-b border-slate-200 bg-white px-6 flex items-center">
        <span className="font-semibold text-slate-900 text-sm">智能问答</span>
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto px-6 py-6 space-y-5">
        {messages.length === 0 && (
          <div className="h-full flex flex-col items-center justify-center text-center text-slate-400 select-none">
            <span className="text-4xl mb-3">💬</span>
            <p className="text-sm font-medium text-slate-500">问我任何新闻话题</p>
            <p className="text-xs mt-1 text-slate-400">例如：今天有什么 AI 新闻？</p>
          </div>
        )}

        {messages.map((msg) =>
          msg.role === "user" ? (
            <div key={msg.id} className="flex justify-end">
              <div className="max-w-md bg-blue-600 text-white text-sm px-4 py-2.5 rounded-2xl rounded-tr-sm leading-relaxed">
                {msg.text}
              </div>
            </div>
          ) : (
            <div key={msg.id} className="flex justify-start">
              <div className="max-w-2xl w-full">
                {msg.error ? (
                  <div className="bg-red-50 border border-red-200 text-red-700 text-sm rounded-2xl rounded-tl-sm px-4 py-3">
                    {msg.error}
                  </div>
                ) : (
                  <div className="bg-white border border-slate-200 rounded-2xl rounded-tl-sm px-4 py-4 space-y-3 shadow-sm">
                    {msg.response && (
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="text-xs bg-blue-50 text-blue-700 px-2 py-0.5 rounded-lg font-medium">
                          {msg.response.category}
                        </span>
                        <span className="text-xs text-slate-400">{msg.response.extracted_topic}</span>
                        <span className="text-xs text-slate-300 ml-auto">{msg.response.latency_ms}ms</span>
                      </div>
                    )}
                    <p className="text-sm text-slate-700 leading-relaxed">{msg.text}</p>
                    {(msg.response?.sources?.length ?? 0) > 0 && (
                      <div>
                        <p className="text-xs font-medium text-slate-400 uppercase tracking-wide mb-2">来源</p>
                        <div className="space-y-1.5">
                          {msg.response!.sources.map((s) => (
                            <a
                              key={s.url}
                              href={s.url}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="block text-xs p-2.5 border border-slate-100 rounded-xl hover:border-blue-200 hover:bg-blue-50 transition-colors"
                            >
                              <div className="font-medium text-slate-800 line-clamp-1">{s.title}</div>
                              <div className="flex gap-2 mt-0.5 text-slate-400">
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
            </div>
          )
        )}

        {loading && (
          <div className="flex justify-start">
            <div className="bg-white border border-slate-200 rounded-2xl rounded-tl-sm px-4 py-3 shadow-sm">
              <div className="flex gap-1.5 items-center h-4">
                {[0, 150, 300].map((delay) => (
                  <span
                    key={delay}
                    className="w-1.5 h-1.5 bg-slate-400 rounded-full animate-bounce"
                    style={{ animationDelay: `${delay}ms` }}
                  />
                ))}
              </div>
            </div>
          </div>
        )}

        <div ref={bottomRef} />
      </div>

      {/* Input bar */}
      <div className="shrink-0 border-t border-slate-200 bg-white px-6 py-4">
        <div className="flex gap-3 items-end max-w-3xl mx-auto">
          <textarea
            ref={textareaRef}
            value={input}
            onChange={handleInput}
            onKeyDown={handleKeyDown}
            placeholder="输入问题… (Enter 发送，Shift+Enter 换行)"
            rows={1}
            className="flex-1 border border-slate-200 rounded-xl px-4 py-2.5 text-sm resize-none focus:outline-none focus:ring-2 focus:ring-blue-500 overflow-y-auto"
            style={{ minHeight: "42px", maxHeight: "128px" }}
          />
          <button
            onClick={handleSend}
            disabled={loading || !input.trim()}
            className="px-5 py-2.5 bg-blue-600 text-white rounded-xl text-sm font-medium hover:bg-blue-700 disabled:opacity-40 disabled:cursor-not-allowed transition-colors shrink-0"
          >
            发送
          </button>
        </div>
      </div>
    </div>
  );
}

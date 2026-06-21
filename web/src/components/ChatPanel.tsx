import { useEffect, useRef, useState } from "react";
import { Loader2, Send } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Textarea } from "@/components/ui/textarea";
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
  const [sessionId, setSessionId] = useState<string | undefined>(undefined);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, loading]);

  const handleSend = async () => {
    const text = input.trim();
    if (!text || loading) return;

    setMessages((prev) => [...prev, { id: ++nextId, role: "user", text }]);
    setInput("");
    setLoading(true);

    try {
      const res = await sendChat(text, sessionId);
      // Persist the session_id for subsequent turns.
      setSessionId(res.session_id);
      setMessages((prev) => [
        ...prev,
        { id: ++nextId, role: "assistant", text: res.reply, response: res },
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

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="h-14 shrink-0 border-b flex items-center px-6 bg-card">
        <h1 className="font-semibold text-sm">智能问答</h1>
        {sessionId && (
          <span className="ml-auto text-xs text-muted-foreground font-mono">
            {sessionId.slice(0, 8)}
          </span>
        )}
      </div>

      {/* Messages — flex-1 min-h-0 是关键：让 flex item 可以收缩，overflow-y-auto 才能生效 */}
      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-6">
        {messages.length === 0 && (
          <div className="h-[calc(100vh-220px)] flex flex-col items-center justify-center text-center select-none">
            <div className="w-12 h-12 rounded-full bg-muted flex items-center justify-center mb-4">
              <span className="text-2xl">💬</span>
            </div>
            <p className="text-sm font-medium text-foreground">问我任何新闻话题</p>
            <p className="text-xs text-muted-foreground mt-1">例如：今天有什么 AI 新闻？</p>
          </div>
        )}

        <div className="space-y-6">
          {messages.map((msg) =>
            msg.role === "user" ? (
              <div key={msg.id} className="flex justify-end">
                <div className="max-w-md bg-primary text-primary-foreground text-sm px-4 py-2.5 rounded-2xl rounded-tr-sm leading-relaxed">
                  {msg.text}
                </div>
              </div>
            ) : (
              <div key={msg.id} className="flex justify-start">
                <div className="max-w-2xl w-full">
                  {msg.error ? (
                    <div className="bg-destructive/10 border border-destructive/20 text-destructive text-sm rounded-2xl rounded-tl-sm px-4 py-3">
                      {msg.error}
                    </div>
                  ) : (
                    <div className="bg-card border rounded-2xl rounded-tl-sm px-4 py-4 space-y-3 shadow-sm">
                      {/* Latency badge */}
                      <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        {msg.response?.tool_called && (
                          <span className="px-1.5 py-0.5 rounded-full bg-muted text-muted-foreground">
                            🔍 已搜索新闻
                          </span>
                        )}
                        <span className="ml-auto">{msg.response?.latency_ms}ms</span>
                      </div>

                      {/* Reply text */}
                      <p className="text-sm leading-relaxed whitespace-pre-wrap">{msg.text}</p>

                      {/* Sources */}
                      {(msg.response?.sources?.length ?? 0) > 0 && (
                        <>
                          <Separator />
                          <div>
                            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-2">
                              来源
                            </p>
                            <div className="space-y-1.5">
                              {msg.response!.sources.map((s) => (
                                <a
                                  key={s.url}
                                  href={s.url}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="block text-xs p-2.5 border rounded-lg hover:border-primary/40 hover:bg-accent transition-colors"
                                >
                                  <div className="font-medium line-clamp-1">{s.title}</div>
                                  <div className="text-muted-foreground mt-0.5">{s.source_domain}</div>
                                </a>
                              ))}
                            </div>
                          </div>
                        </>
                      )}
                    </div>
                  )}
                </div>
              </div>
            )
          )}

          {loading && (
            <div className="flex justify-start">
              <div className="bg-card border rounded-2xl rounded-tl-sm px-4 py-3 shadow-sm">
                <div className="flex gap-1.5 items-center h-5">
                  {[0, 150, 300].map((delay) => (
                    <span
                      key={delay}
                      className="w-1.5 h-1.5 bg-muted-foreground rounded-full animate-bounce"
                      style={{ animationDelay: `${delay}ms` }}
                    />
                  ))}
                </div>
              </div>
            </div>
          )}

          <div ref={bottomRef} />
        </div>
      </div>

      {/* Input bar */}
      <div className="shrink-0 border-t bg-card px-6 py-4">
        <div className="flex gap-3 items-end max-w-3xl mx-auto">
          <Textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="输入问题… (Enter 发送，Shift+Enter 换行)"
            rows={1}
            className="flex-1 resize-none min-h-[42px] max-h-32"
          />
          <Button
            onClick={handleSend}
            disabled={loading || !input.trim()}
            size="default"
          >
            {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
          </Button>
        </div>
      </div>
    </div>
  );
}

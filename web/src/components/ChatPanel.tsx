import { useEffect, useRef, useState } from "react";
import { Loader2, Send } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Textarea } from "@/components/ui/textarea";
import { sendChatStream } from "../api/client";
import type { ChatSource } from "../types";

// ── Message shape ─────────────────────────────────────────────────────────────

interface AssistantMessage {
  id: number;
  role: "assistant";
  // Streaming state
  streaming: boolean;
  text: string;
  // Populated after "done" event
  sources: ChatSource[];
  toolCalled: boolean;
  latencyMs?: number;
  error?: string;
  // Ephemeral tool indicator shown while a tool is running
  activeTool?: string;
}

interface UserMessage {
  id: number;
  role: "user";
  text: string;
}

type Message = UserMessage | AssistantMessage;

let nextId = 0;

function newAssistant(): AssistantMessage {
  return {
    id: ++nextId,
    role: "assistant",
    streaming: true,
    text: "",
    sources: [],
    toolCalled: false,
  };
}

// ── Component ─────────────────────────────────────────────────────────────────

export function ChatPanel() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const [sessionId, setSessionId] = useState<string | undefined>(undefined);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  const updateLast = (updater: (prev: AssistantMessage) => AssistantMessage) => {
    setMessages((msgs) => {
      const copy = [...msgs];
      const last = copy[copy.length - 1];
      if (last?.role === "assistant") {
        copy[copy.length - 1] = updater(last as AssistantMessage);
      }
      return copy;
    });
  };

  const handleSend = async () => {
    const text = input.trim();
    if (!text || loading) return;

    setMessages((prev) => [...prev, { id: ++nextId, role: "user", text }]);
    setInput("");
    setLoading(true);

    const placeholder = newAssistant();
    setMessages((prev) => [...prev, placeholder]);

    try {
      await sendChatStream(text, sessionId, (ev) => {
        switch (ev.type) {
          case "thinking":
            // No-op: the typing indicator is already visible
            break;

          case "tool":
            updateLast((m) => ({ ...m, toolCalled: true, activeTool: ev.tool }));
            break;

          case "delta":
            updateLast((m) => ({
              ...m,
              activeTool: undefined,
              text: m.text + (ev.content ?? ""),
            }));
            break;

          case "done":
            if (ev.session_id) setSessionId(ev.session_id);
            updateLast((m) => ({
              ...m,
              streaming: false,
              activeTool: undefined,
              sources: ev.sources ?? [],
              toolCalled: ev.tool_called ?? m.toolCalled,
              latencyMs: ev.latency_ms,
            }));
            break;

          case "error":
            updateLast((m) => ({
              ...m,
              streaming: false,
              activeTool: undefined,
              error: ev.content ?? "未知错误",
            }));
            break;
        }
      });
    } catch (e: unknown) {
      updateLast((m) => ({
        ...m,
        streaming: false,
        error: (e as Error).message,
      }));
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

      {/* Messages — flex-1 min-h-0 is required for overflow-y-auto to work */}
      <div className="flex-1 min-h-0 overflow-y-auto px-6 py-6">
        {messages.length === 0 && (
          <div className="h-full flex flex-col items-center justify-center text-center select-none">
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
              <UserBubble key={msg.id} text={msg.text} />
            ) : (
              <AssistantBubble key={msg.id} msg={msg as AssistantMessage} />
            )
          )}
          <div ref={bottomRef} />
        </div>
      </div>

      {/* Input */}
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
          <Button onClick={handleSend} disabled={loading || !input.trim()}>
            {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
          </Button>
        </div>
      </div>
    </div>
  );
}

// ── Sub-components ────────────────────────────────────────────────────────────

function UserBubble({ text }: { text: string }) {
  return (
    <div className="flex justify-end">
      <div className="max-w-md bg-primary text-primary-foreground text-sm px-4 py-2.5 rounded-2xl rounded-tr-sm leading-relaxed">
        {text}
      </div>
    </div>
  );
}

function AssistantBubble({ msg }: { msg: AssistantMessage }) {
  return (
    <div className="flex justify-start">
      <div className="max-w-2xl w-full">
        {msg.error ? (
          <div className="bg-destructive/10 border border-destructive/20 text-destructive text-sm rounded-2xl rounded-tl-sm px-4 py-3">
            {msg.error}
          </div>
        ) : (
          <div className="bg-card border rounded-2xl rounded-tl-sm px-4 py-4 space-y-3 shadow-sm">
            {/* Status row */}
            <div className="flex items-center gap-2 text-xs text-muted-foreground min-h-[20px]">
              {msg.activeTool && (
                <span className="flex items-center gap-1.5 px-1.5 py-0.5 rounded-full bg-muted animate-pulse">
                  🔍 正在搜索新闻…
                </span>
              )}
              {!msg.activeTool && msg.toolCalled && !msg.streaming && (
                <span className="px-1.5 py-0.5 rounded-full bg-muted">
                  🔍 已搜索新闻
                </span>
              )}
              {!msg.streaming && msg.latencyMs !== undefined && (
                <span className="ml-auto">{msg.latencyMs}ms</span>
              )}
            </div>

            {/* Text (streaming or final) */}
            {msg.text ? (
              <p className="text-sm leading-relaxed whitespace-pre-wrap">
                {msg.text}
                {msg.streaming && (
                  <span className="inline-block w-0.5 h-4 ml-0.5 bg-foreground animate-pulse align-text-bottom" />
                )}
              </p>
            ) : (
              msg.streaming && !msg.activeTool && (
                // Thinking dots before first token
                <div className="flex gap-1.5 items-center h-5">
                  {[0, 150, 300].map((delay) => (
                    <span
                      key={delay}
                      className="w-1.5 h-1.5 bg-muted-foreground rounded-full animate-bounce"
                      style={{ animationDelay: `${delay}ms` }}
                    />
                  ))}
                </div>
              )
            )}

            {/* Sources */}
            {msg.sources.length > 0 && (
              <>
                <Separator />
                <div>
                  <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-2">
                    来源
                  </p>
                  <div className="space-y-1.5">
                    {msg.sources.map((s) => (
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
  );
}

import { useState, useCallback } from "react";
import { deleteSession, sendChatStream } from "../api/client";
import type { Conversation } from "../types";
import {
  ChatPanel,
  type Message,
  type AssistantMessage,
  newAssistant,
} from "./ChatPanel";
import { ConversationList } from "./ConversationList";

// ── Helpers ───────────────────────────────────────────────────────────────────

let convCounter = 0;

function makeConversation(): Conversation {
  return {
    localId: `conv-${++convCounter}-${Date.now()}`,
    title: "新对话",
    createdAt: Date.now(),
  };
}

// ── Component ─────────────────────────────────────────────────────────────────

export function ChatView() {
  const initial = makeConversation();

  const [conversations, setConversations] = useState<Conversation[]>([initial]);
  const [activeId, setActiveId] = useState<string>(initial.localId);
  const [messagesMap, setMessagesMap] = useState<Record<string, Message[]>>({
    [initial.localId]: [],
  });
  const [loadingMap, setLoadingMap] = useState<Record<string, boolean>>({});
  const [inputMap, setInputMap] = useState<Record<string, string>>({});

  const activeConv = conversations.find((c) => c.localId === activeId)!;
  const messages = messagesMap[activeId] ?? [];
  const loading = loadingMap[activeId] ?? false;
  const input = inputMap[activeId] ?? "";

  // ── Mutation helpers ────────────────────────────────────────────────────────

  const updateConv = useCallback((localId: string, patch: Partial<Conversation>) => {
    setConversations((prev) =>
      prev.map((c) => (c.localId === localId ? { ...c, ...patch } : c))
    );
  }, []);

  const setMessages = useCallback((localId: string, msgs: Message[]) => {
    setMessagesMap((prev) => ({ ...prev, [localId]: msgs }));
  }, []);

  const updateLastMessage = useCallback(
    (localId: string, updater: (m: AssistantMessage) => AssistantMessage) => {
      setMessagesMap((prev) => {
        const msgs = [...(prev[localId] ?? [])];
        const last = msgs[msgs.length - 1];
        if (last?.role === "assistant") {
          msgs[msgs.length - 1] = updater(last as AssistantMessage);
        }
        return { ...prev, [localId]: msgs };
      });
    },
    []
  );

  // ── Send message ────────────────────────────────────────────────────────────

  const handleSend = useCallback(() => {
    const text = (inputMap[activeId] ?? "").trim();
    if (!text || loadingMap[activeId]) return;

    const convId = activeId;
    const conv = conversations.find((c) => c.localId === convId)!;

    // Add user message
    const userMsg: Message = { id: Date.now(), role: "user", text };
    const placeholder = newAssistant();

    setMessagesMap((prev) => ({
      ...prev,
      [convId]: [...(prev[convId] ?? []), userMsg, placeholder],
    }));
    setInputMap((prev) => ({ ...prev, [convId]: "" }));
    setLoadingMap((prev) => ({ ...prev, [convId]: true }));

    // Set title from first user message
    if (conv.title === "新对话") {
      updateConv(convId, {
        title: text.slice(0, 20) + (text.length > 20 ? "…" : ""),
      });
    }

    sendChatStream(text, conv.sessionId, (ev) => {
      switch (ev.type) {
        case "tool":
          updateLastMessage(convId, (m) => ({
            ...m,
            toolCalled: true,
            activeTool: ev.tool,
          }));
          break;
        case "delta":
          updateLastMessage(convId, (m) => ({
            ...m,
            activeTool: undefined,
            text: m.text + (ev.content ?? ""),
          }));
          break;
        case "done":
          if (ev.session_id) updateConv(convId, { sessionId: ev.session_id });
          updateLastMessage(convId, (m) => ({
            ...m,
            streaming: false,
            activeTool: undefined,
            sources: ev.sources ?? [],
            toolCalled: ev.tool_called ?? m.toolCalled,
            latencyMs: ev.latency_ms,
          }));
          setLoadingMap((prev) => ({ ...prev, [convId]: false }));
          break;
        case "error":
          updateLastMessage(convId, (m) => ({
            ...m,
            streaming: false,
            activeTool: undefined,
            error: ev.content ?? "未知错误",
          }));
          setLoadingMap((prev) => ({ ...prev, [convId]: false }));
          break;
      }
    }).catch((e: unknown) => {
      updateLastMessage(convId, (m) => ({
        ...m,
        streaming: false,
        error: (e as Error).message,
      }));
      setLoadingMap((prev) => ({ ...prev, [convId]: false }));
    });
  }, [activeId, inputMap, loadingMap, conversations, updateConv, updateLastMessage]);

  // ── Conversation management ─────────────────────────────────────────────────

  const handleCreate = useCallback(() => {
    const conv = makeConversation();
    setConversations((prev) => [conv, ...prev]);
    setMessagesMap((prev) => ({ ...prev, [conv.localId]: [] }));
    setActiveId(conv.localId);
  }, []);

  const handleDelete = useCallback(
    (localId: string) => {
      const conv = conversations.find((c) => c.localId === localId);
      if (conv?.sessionId) deleteSession(conv.sessionId);

      setConversations((prev) => {
        const remaining = prev.filter((c) => c.localId !== localId);
        if (remaining.length === 0) {
          const fresh = makeConversation();
          setMessagesMap({ [fresh.localId]: [] });
          setActiveId(fresh.localId);
          return [fresh];
        }
        if (activeId === localId) {
          setActiveId(remaining[0].localId);
        }
        return remaining;
      });
      setMessagesMap((prev) => {
        const next = { ...prev };
        delete next[localId];
        return next;
      });
    },
    [activeId, conversations]
  );

  // ── Render ──────────────────────────────────────────────────────────────────

  return (
    <div className="flex h-full">
      <ConversationList
        conversations={conversations}
        activeId={activeId}
        onSelect={setActiveId}
        onCreate={handleCreate}
        onDelete={handleDelete}
      />
      <div className="flex-1 overflow-hidden">
        <ChatPanel
          key={activeId}
          conversation={activeConv}
          messages={messages}
          loading={loading}
          input={input}
          onInputChange={(v) =>
            setInputMap((prev) => ({ ...prev, [activeId]: v }))
          }
          onSend={handleSend}
        />
      </div>
    </div>
  );
}

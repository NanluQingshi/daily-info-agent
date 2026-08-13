import { useState, useCallback, useEffect, useRef } from "react";
import { deleteSession, sendChatStream } from "../api/client";
import type { Conversation } from "../types";
import {
  ChatPanel,
  type Message,
  type AssistantMessage,
  newAssistant,
} from "./ChatPanel";
import { ConversationList } from "./ConversationList";

// ── localStorage persistence ─────────────────────────────────────────────

const STORAGE_KEY = "dia.chat_state";

// Size limits to keep localStorage usage bounded (~5MB browser quota):
// - Keep at most MAX_CONVERSATIONS conversations (oldest dropped).
// - Keep at most MAX_MESSAGES_PER_CONV messages per conversation.
const MAX_CONVERSATIONS = 20;
const MAX_MESSAGES_PER_CONV = 100;

interface PersistedState {
  conversations: Conversation[];
  messagesMap: Record<string, Message[]>;
  activeId: string;
  inputMap: Record<string, string>;
}

function loadState(): PersistedState | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    return JSON.parse(raw) as PersistedState;
  } catch {
    return null;
  }
}

// trimState enforces the size limits: drops the oldest conversations beyond
// MAX_CONVERSATIONS and truncates message lists beyond MAX_MESSAGES_PER_CONV.
function trimState(state: PersistedState): PersistedState {
  const conversations = [...state.conversations].sort(
    (a, b) => (b.createdAt ?? 0) - (a.createdAt ?? 0)
  );
  const kept = conversations.slice(0, MAX_CONVERSATIONS);
  const keptIds = new Set(kept.map((c) => c.localId));

  const messagesMap: Record<string, Message[]> = {};
  for (const conv of kept) {
    const msgs = state.messagesMap[conv.localId] ?? [];
    messagesMap[conv.localId] =
      msgs.length > MAX_MESSAGES_PER_CONV
        ? msgs.slice(msgs.length - MAX_MESSAGES_PER_CONV)
        : msgs;
  }

  const inputMap: Record<string, string> = {};
  for (const [id, v] of Object.entries(state.inputMap ?? {})) {
    if (keptIds.has(id)) inputMap[id] = v;
  }

  return {
    conversations: kept,
    messagesMap,
    activeId: keptIds.has(state.activeId) ? state.activeId : (kept[0]?.localId ?? ""),
    inputMap,
  };
}

function saveState(state: PersistedState) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(trimState(state)));
  } catch {
    // localStorage full or unavailable — silently ignore
  }
}

export function clearChatHistory() {
  localStorage.removeItem(STORAGE_KEY);
}

// ── Helpers ──────────────────────────────────────────────────────────────

let convCounter = 0;

function makeConversation(): Conversation {
  return {
    localId: `conv-${++convCounter}-${Date.now()}`,
    title: "新对话",
    createdAt: Date.now(),
  };
}

// ── Component ────────────────────────────────────────────────────────────

export function ChatView() {
  const [initialized, setInitialized] = useState(false);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [activeId, setActiveId] = useState<string>("");
  const [messagesMap, setMessagesMap] = useState<Record<string, Message[]>>({});
  const [loadingMap, setLoadingMap] = useState<Record<string, boolean>>({});
  const [inputMap, setInputMap] = useState<Record<string, string>>({});

  // Load persisted state on mount
  useEffect(() => {
    const saved = loadState();
    if (saved && saved.conversations.length > 0) {
      setConversations(saved.conversations);
      setMessagesMap(saved.messagesMap);
      setActiveId(saved.activeId);
      setInputMap(saved.inputMap ?? {});
    } else {
      const fresh = makeConversation();
      setConversations([fresh]);
      setActiveId(fresh.localId);
      setMessagesMap({ [fresh.localId]: [] });
    }
    setInitialized(true);
  }, []);

  // Persist to localStorage whenever state changes (with debounce)
  const persistTimer = useRef<ReturnType<typeof setTimeout>>();
  useEffect(() => {
    if (!initialized) return;
    if (persistTimer.current) clearTimeout(persistTimer.current);
    persistTimer.current = setTimeout(() => {
      saveState({
        conversations,
        messagesMap,
        activeId,
        inputMap,
      });
    }, 300);
    return () => {
      if (persistTimer.current) clearTimeout(persistTimer.current);
    };
  }, [conversations, messagesMap, activeId, inputMap, initialized]);

  const activeConv = conversations.find((c) => c.localId === activeId);
  const messages = activeId ? (messagesMap[activeId] ?? []) : [];
  const loading = activeId ? (loadingMap[activeId] ?? false) : false;
  const input = activeId ? (inputMap[activeId] ?? "") : "";

  // ── Mutation helpers ───────────────────────────────────────────────────

  const updateConv = useCallback((localId: string, patch: Partial<Conversation>) => {
    setConversations((prev) =>
      prev.map((c) => (c.localId === localId ? { ...c, ...patch } : c))
    );
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

  // ── Send message ───────────────────────────────────────────────────────

  const abortRef = useRef<AbortController | null>(null);

  const handleStop = useCallback(() => {
    if (abortRef.current) {
      abortRef.current.abort();
      abortRef.current = null;
    }
    // Mark all loading conversations as done
    setLoadingMap((prev) => {
      const next = { ...prev };
      for (const k of Object.keys(next)) {
        if (next[k]) {
          next[k] = false;
          // Update the last message to show it was stopped
          updateLastMessage(k, (m) => ({
            ...m,
            streaming: false,
            activeTool: undefined,
            text: m.text || "（已取消）",
          }));
        }
      }
      return next;
    });
  }, [updateLastMessage]);

  const handleSend = useCallback(() => {
    if (!activeId) return;
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

    const controller = new AbortController();
    abortRef.current = controller;

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
          abortRef.current = null;
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
          abortRef.current = null;
          updateLastMessage(convId, (m) => ({
            ...m,
            streaming: false,
            activeTool: undefined,
            error: ev.content ?? "未知错误",
          }));
          setLoadingMap((prev) => ({ ...prev, [convId]: false }));
          break;
      }
    }, controller.signal).catch((e: unknown) => {
      abortRef.current = null;
      // Ignore abort errors
      if ((e as Error).name === "AbortError") return;
      updateLastMessage(convId, (m) => ({
        ...m,
        streaming: false,
        error: (e as Error).message,
      }));
      setLoadingMap((prev) => ({ ...prev, [convId]: false }));
    });
  }, [activeId, inputMap, loadingMap, conversations, updateConv, updateLastMessage]);

  // ── Conversation management ────────────────────────────────────────────

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

  // ── Render ─────────────────────────────────────────────────────────────

  if (!activeConv) return null;

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
          onStop={handleStop}
        />
      </div>
    </div>
  );
}
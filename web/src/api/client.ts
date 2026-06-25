import type {
  ArticleFilter,
  ArticleListResponse,
  ArticleRow,
  ChatResponse,
  FetchTriggerResponse,
  StatsResult,
  StreamEvent,
} from "../types";

const BASE = "/api";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, init);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.message || `HTTP ${res.status}`);
  }
  if (res.status === 204) return undefined as unknown as T;
  return res.json();
}

function buildQuery(params: Record<string, string | number | undefined>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== "") q.set(k, String(v));
  }
  const s = q.toString();
  return s ? "?" + s : "";
}

export function listArticles(f: ArticleFilter = {}): Promise<ArticleListResponse> {
  return request(`/articles${buildQuery(f as Record<string, string | number | undefined>)}`);
}

export function getArticle(id: number): Promise<ArticleRow> {
  return request(`/articles/${id}`);
}

export function publishArticle(id: number): Promise<{ published: boolean; external_id: number }> {
  return request(`/articles/${id}/publish`, { method: "POST" });
}

export function deleteArticle(id: number): Promise<void> {
  return request(`/articles/${id}`, { method: "DELETE" });
}

export function retryArticle(id: number): Promise<{ retried: boolean; id: number }> {
  return request(`/articles/${id}/retry`, { method: "POST" });
}

export function triggerFetch(): Promise<FetchTriggerResponse> {
  return request("/fetch", { method: "POST" });
}

export function getStats(since?: string): Promise<StatsResult> {
  return request(`/stats${since ? `?since=${since}` : ""}`);
}

/** Delete a backend session (best-effort, ignores errors). */
export async function deleteSession(sessionId: string): Promise<void> {
  await fetch(`/api/sessions/${encodeURIComponent(sessionId)}`, {
    method: "DELETE",
  }).catch(() => {/* fire-and-forget */});
}

export function sendChat(message: string, sessionId?: string): Promise<ChatResponse> {
  return request("/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message, session_id: sessionId }),
  });
}

/**
 * sendChatStream opens a streaming connection to POST /api/chat/stream
 * and calls onEvent for each SSE event. Returns when the stream ends.
 */
export async function sendChatStream(
  message: string,
  sessionId: string | undefined,
  onEvent: (ev: StreamEvent) => void
): Promise<void> {
  const res = await fetch("/api/chat/stream", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message, session_id: sessionId }),
  });

  if (!res.ok || !res.body) {
    const body = await res.json().catch(() => ({}));
    throw new Error((body as { message?: string }).message ?? `HTTP ${res.status}`);
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? ""; // keep incomplete last line

    for (const line of lines) {
      if (!line.startsWith("data: ")) continue;
      const data = line.slice(6).trim();
      if (!data) continue;
      try {
        onEvent(JSON.parse(data) as StreamEvent);
      } catch {
        // ignore malformed chunks
      }
    }
  }
}

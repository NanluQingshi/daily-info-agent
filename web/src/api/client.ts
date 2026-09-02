import type {
  ArticleFilter,
  ArticleListResponse,
  ArticleRow,
  ChatResponse,
  FetchTriggerResponse,
  RunListResponse,
  SourceHealthResponse,
  SourceListResponse,
  SourceRow,
  StatsResult,
  StreamEvent,
} from "../types";

const BASE = "/api";
const TOKEN_KEY = "dia.chat_api_token";

/** Returns the chat API token stored in localStorage, if any. */
export function getApiToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? "";
}

/** Stores (or clears, when empty) the chat API token in localStorage. */
export function setApiToken(token: string): void {
  const trimmed = token.trim();
  if (trimmed) localStorage.setItem(TOKEN_KEY, trimmed);
  else localStorage.removeItem(TOKEN_KEY);
}

/** Merges the stored API token into a headers object when one is set. */
function withAuthHeaders(headers: Record<string, string> = {}): Record<string, string> {
  const token = getApiToken();
  if (token) headers["X-Api-Token"] = token;
  return headers;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = withAuthHeaders(
    init?.headers as Record<string, string> | undefined
  );
  const res = await fetch(BASE + path, { ...init, headers });
  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    // Only try JSON parsing when Content-Type looks like JSON.
    const ct = res.headers.get("Content-Type") ?? "";
    if (ct.includes("application/json")) {
      try {
        const body = await res.json();
        if (body.message) message = body.message;
      } catch { /* keep status-based message */ }
    } else if (res.status === 404) {
      message = "API endpoint not found — is the backend running?";
    }
    throw new Error(message);
  }
  if (res.status === 204) return undefined as unknown as T;
  return res.json();
}

function buildQuery(params: Record<string, string | number | boolean | undefined>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== false && v !== "") q.set(k, String(v));
  }
  const s = q.toString();
  return s ? "?" + s : "";
}

export function listArticles(f: ArticleFilter = {}): Promise<ArticleListResponse> {
  return request(`/articles${buildQuery(f as Record<string, string | number | boolean | undefined>)}`);
}

/** Format of the article export download. "md" renders a readable archive. */
export type ExportFormat = "csv" | "json" | "md";

/** Triggers a browser download of the article export. */
export async function exportArticles(filter: ArticleFilter, format: ExportFormat): Promise<void> {
  // Export always covers every matching row: client-side pagination is
  // intentionally not forwarded.
  const params: Record<string, string | number | undefined> = {
    category: filter.category,
    status: filter.status,
    date_from: filter.date_from,
    date_to: filter.date_to,
    q: filter.q,
    format,
  };
  const res = await fetch(BASE + "/articles/export" + buildQuery(params), {
    headers: withAuthHeaders(),
  });
  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    try {
      const body = await res.json();
      if (body.message) message = body.message;
    } catch { /* keep status-based message */ }
    throw new Error(message);
  }
  const blob = await res.blob();
  const cd = res.headers.get("Content-Disposition") ?? "";
  const m = /filename="?([^";]+)"?/.exec(cd);
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = m?.[1] ?? `articles.${format}`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

export function getArticle(id: number): Promise<ArticleRow> {
  return request(`/articles/${id}`);
}

/** Update bookmark / read flags; omitted fields keep their current value. */
export function updateArticleFlags(
  id: number,
  flags: { bookmarked?: boolean; read?: boolean },
): Promise<ArticleRow> {
  return request(`/articles/${id}/flags`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(flags),
  });
}

/** Rating kinds users can give feedback on. */
export type FeedbackKind = "summary" | "category";

export interface ArticleFeedbackRow {
  id: number;
  article_id: number;
  kind: FeedbackKind;
  rating: 1 | -1;
  created_at: string;
}

/** Store a 👍/👎 for one aspect; repeat clicks overwrite (latest wins). */
export function submitFeedback(
  id: number,
  kind: FeedbackKind,
  rating: 1 | -1,
): Promise<ArticleFeedbackRow> {
  return request(`/articles/${id}/feedback`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ kind, rating }),
  });
}

/** Current feedback state of one article (for UI echo-back). */
export function getFeedback(id: number): Promise<{ feedback: ArticleFeedbackRow[] }> {
  return request(`/articles/${id}/feedback`);
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

export function batchUpdateTags(articleIds: number[], tags: string[]): Promise<{ updated: number }> {
  return request("/articles/tags", {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ article_ids: articleIds, tags }),
  });
}

export function triggerFetch(): Promise<FetchTriggerResponse> {
  return request("/fetch", { method: "POST" });
}

/** Recent pipeline runs for the run history panel. */
export function getRuns(limit = 30): Promise<RunListResponse> {
  return request<RunListResponse>(`/runs?limit=${limit}`);
}

/** Per-source fetch health for the source health panel. */
export function getSourceHealth(): Promise<SourceHealthResponse> {
  return request<SourceHealthResponse>("/sources/health");
}

/** Managed RSS sources (issue #80): list / add / toggle / remove. */
export function listSources(): Promise<SourceListResponse> {
  return request<SourceListResponse>("/sources");
}

export function addSource(url: string): Promise<SourceRow> {
  return request<SourceRow>("/sources", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url }),
  });
}

export function setSourceEnabled(id: number, enabled: boolean): Promise<SourceRow> {
  return request<SourceRow>(`/sources/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ enabled }),
  });
}

export async function removeSource(id: number): Promise<void> {
  await request(`/sources/${id}`, { method: "DELETE" });
}

export function getStats(since?: string): Promise<StatsResult> {
  return request(`/stats${since ? `?since=${since}` : ""}`);
}

/** Delete a backend session (best-effort, ignores errors). */
export async function deleteSession(sessionId: string): Promise<void> {
  await fetch(`/api/sessions/${encodeURIComponent(sessionId)}`, {
    method: "DELETE",
    headers: withAuthHeaders(),
  }).catch(() => {/* fire-and-forget */});
}

export function sendChat(message: string, sessionId?: string, lang?: string): Promise<ChatResponse> {
  return request("/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message, session_id: sessionId, lang }),
  });
}

/**
 * sendChatStream opens a streaming connection to POST /api/chat/stream
 * and calls onEvent for each SSE event. Returns when the stream ends.
 * lang optionally sets the reply language ("zh" | "en" | "auto").
 */
export async function sendChatStream(
  message: string,
  sessionId: string | undefined,
  onEvent: (ev: StreamEvent) => void,
  signal?: AbortSignal,
  lang?: string
): Promise<void> {
  const res = await fetch("/api/chat/stream", {
    method: "POST",
    headers: withAuthHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify({ message, session_id: sessionId, lang }),
    signal,
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

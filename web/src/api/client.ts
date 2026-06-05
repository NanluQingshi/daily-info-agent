import type {
  ArticleFilter,
  ArticleListResponse,
  ArticleRow,
  ChatResponse,
  FetchTriggerResponse,
  StatsResult,
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

export function sendChat(message: string): Promise<ChatResponse> {
  return request("/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message }),
  });
}

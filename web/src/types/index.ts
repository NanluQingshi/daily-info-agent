export interface ArticleRow {
  id: number;
  run_id: string;
  source_url: string;
  title: string;
  description: string;
  content: string;
  summary: string;
  category: string;
  source_domain: string;
  source_type: string;
  credibility_score: number;
  tags: string[];
  language: string;
  detected_language: string;
  agent_version: string;
  verification_pass: boolean;
  skip_reason: string;
  domain_hit: boolean;
  status: "pending" | "published" | "skipped" | "failed";
  external_id: number | null;
  published_at: string | null;
  fetched_at: string;
  created_at: string;
  updated_at: string;
}

export interface ArticleListResponse {
  articles: ArticleRow[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

export interface RunLogRow {
  run_id: string;
  total_fetched: number;
  total_processed: number;
  total_saved: number;
  total_published: number;
  total_skipped: number;
  total_failed: number;
  duration_ms: number;
  fatal_error: string;
  started_at: string;
  finished_at: string;
}

export interface DayStat {
  date: string;
  count: number;
}

export interface CategoryStat {
  category: string;
  count: number;
}

export interface StatsResult {
  by_day: DayStat[];
  by_category: CategoryStat[];
  recent_runs: RunLogRow[];
}

export interface FetchTriggerResponse {
  run_id: string;
  triggered: boolean;
  message: string;
}

export interface ChatSource {
  url: string;
  title: string;
  source_domain: string;
}

export interface ChatResponse {
  session_id: string;
  reply: string;
  sources: ChatSource[];
  tool_called: boolean;
  fetched_at: string;
  latency_ms: number;
}

export interface ArticleFilter {
  category?: string;
  status?: string;
  date_from?: string;
  date_to?: string;
  q?: string;
  page?: number;
  page_size?: number;
}

export const CATEGORIES = ["金融", "政治", "经济", "科技/AI", "国际"] as const;
export const STATUSES = ["pending", "published", "skipped", "failed"] as const;

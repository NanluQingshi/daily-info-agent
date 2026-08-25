# Daily Info Agent — Product Requirements Document

**Version**: 2.0  
**Date**: 2026-07-03  
**Status**: Living  
**Module**: `github.com/user/daily-info-agent`

---

## 1. Project Background & Goals

### Background

Information overload is a persistent problem for individuals who need to stay informed across multiple domains — finance, politics, economics, technology, and international affairs — in both Chinese and English media. Manually aggregating and reading raw feeds is time-consuming and produces low signal-to-noise output. This project automates the full pipeline: fetch → process → verify → store → publish → notify, delivering a curated, AI-summarized digest to both a personal website and email inbox every morning, and responding to ad-hoc curiosity throughout the day via a conversational API with a management GUI.

### Problem Statement

- Raw RSS/news feeds contain hundreds of items daily; most are low-value or duplicate
- No single source covers all required categories in Chinese
- Verifying source credibility manually is impractical at scale
- Storing and browsing historical articles requires a persistent database
- Publishing formatted articles to a personal site requires a repeatable, automated workflow

### Goals (v2)

| Goal | Target |
|------|--------|
| Deliver a daily digest to the website and/or email every morning | By 09:00 CST daily |
| Process at least 200 news items per scheduled run | With < 5% missed publishes due to system error |
| Provide on-demand conversational fetch | Response within 30 seconds for a single-topic query |
| Provide a management GUI for browsing articles and triggering fetches | Accessible via browser; all CRUD operations supported |
| Persist articles in PostgreSQL for historical query and full-text search | All processed articles stored; searchable within 2 seconds |
| Enforce source credibility filtering | Zero items published with AI score < 0.7 AND not on whitelist |
| Send daily digest email | Configurable SMTP; includes summary stats and hot articles |

### Success Metrics

- **Coverage**: >= 3 published articles per category per scheduled run
- **Reliability**: >= 99% of scheduled GitHub Actions runs complete without a fatal error
- **Quality**: AI credibility score average >= 0.75 across published items
- **Latency**: Conversational API P95 response time <= 30 seconds
- **Deduplication**: < 2% duplicate articles published in a rolling 7-day window
- **DB durability**: 100% of processed articles persisted when DATABASE_DSN is configured
- **Frontend availability**: Web GUI loads in < 3 seconds on first visit

---

## 2. User Stories

### P0 — Must-Have

| ID | Story | Value |
|----|-------|-------|
| US-001 | As a **site owner**, I want a daily digest of categorized news automatically published to my website at 9am CST so that I can read a curated briefing without manual effort. | Saves 30–60 min/day of manual aggregation |
| US-002 | As a **site owner**, I want each published article to include a concise Chinese summary so that I can quickly understand the gist without reading the full source. | High information density |
| US-003 | As a **site owner**, I want source credibility enforced before publishing so that low-quality or unreliable content never appears on my site. | Protects site reputation |
| US-004 | As a **site owner**, I want the agent to POST new articles to my website API so that content appears on my site without manual copy-paste. | Full automation of the last mile |
| US-005 | As a **site owner**, I want the system to retry failed API calls so that transient network errors do not silently drop articles. | Reliability |
| US-006 | As a **developer**, I want all secrets (API keys, tokens) stored as environment variables and never logged so that credentials are not leaked. | Security baseline |
| US-007 | As a **site visitor**, I want to send a chat message asking about a specific topic and receive a fresh, AI-summarized response so that I can get on-demand news without browsing. | Conversational UX |

### P1 — Should-Have

| ID | Story | Value |
|----|-------|-------|
| US-008 | As a **developer**, I want structured JSON logs for every pipeline step so that I can debug failures quickly. | Observability |
| US-009 | As a **site owner**, I want the default categories configurable via environment variables so that I can adjust scope without code changes. | Operational flexibility |
| US-010 | As a **developer**, I want deduplication across runs so that the same article is not published twice within a 7-day window. | Content quality |
| US-011 | As a **site owner**, I want the conversational API to maintain session context so that follow-up questions can refine the previous query. | Better UX |
| US-012 | As a **site owner**, I want all processed articles stored in a database so that I can browse and search historical content via a GUI. | Historical access |
| US-013 | As a **site owner**, I want a management GUI for browsing articles, triggering fetches, and viewing stats so that I can manage the system without SSH or curl. | Operational convenience |
| US-014 | As a **site owner**, I want an optional daily email digest so that I can stay informed even when I don't visit the website. | Multi-channel delivery |
| US-015 | As a **developer**, I want a streaming chat endpoint (SSE) so that long responses start rendering before completion. | UX responsiveness |

### P2 — Nice-to-Have

| ID | Story | Value |
|----|-------|-------|
| US-016 | As a **site owner**, I want WeChat public account articles fetched via RSSHub so that Chinese-language social media content is included. | Source diversity |
| US-017 | As a **developer**, I want Prometheus-compatible metrics exposed on a `/metrics` endpoint so that I can monitor pipeline health in a dashboard. | Advanced observability |
| US-018 | As a **site owner**, I want the system to send alerts if a scheduled run fails or produces no articles. | Proactive monitoring |

---

## 3. Architecture Overview

```
                    ┌──────────────────────────────────────────────────┐
                    │              Go Agent Binary                     │
                    │  ┌─────────┐  ┌──────────┐  ┌────────────────┐  │
                    │  │ Fetcher │→│Processor │→│  Verifier       │  │
                    │  │(RSS,    │  │(DeepSeek │  │(Whitelist+     │  │
                    │  │NewsAPI, │  │ AI)      │  │ Score check)   │  │
                    │  │RSSHub)  │  └──────────┘  └───────┬────────┘  │
                    │  └─────────┘                        │           │
                    │                                     ▼           │
                    │  ┌──────────┐  ┌──────────┐  ┌────────────────┐  │
                    │  │ Notifier │← │ Store    │← │ Publisher      │  │
                    │  │(SMTP)    │  │(Postgres)│  │(Website API)   │  │
                    │  └──────────┘  └──────────┘  └────────────────┘  │
                    │                                                   │
                    │  ┌────────────────────────────────────────────┐  │
                    │  │  HTTP Server (Echo)                        │  │
                    │  │  /api/chat  /api/chat/stream              │  │
                    │  │  /api/articles/*  /api/fetch  /api/stats  │  │
                    │  │  /health  / (static frontend)             │  │
                    │  └────────────────────────────────────────────┘  │
                    └──────────────────────────────────────────────────┘
                               │                    │
                      ┌────────┴────────┐  ┌────────┴────────┐
                      │  External APIs  │  │  Web Frontend   │
                      │  - DeepSeek LLM │  │  React + TS     │
                      │  - NewsAPI      │  │  shadcn/ui      │
                      │  - RSS Feeds    │  │  Vite            │
                      │  - RSSHub       │  │                  │
                      │  - Website API  │  │                  │
                      │  - SMTP         │  │                  │
                      └─────────────────┘  └──────────────────┘
```

### Core Data Flow

```
Trigger → Fetch (parallel) → Deduplicate → AI Process (batch) →
Verify (whitelist + score) → Store (PostgreSQL) → Publish (website API) →
Notify (email digest)
```

### Two Runtime Modes

| Mode | Flag | Purpose |
|------|------|---------|
| Schedule | `--mode=schedule` | One-shot full pipeline: fetch → process → verify → store → publish → notify |
| Server | `--mode=server` | Long-running HTTP server: chat API + management REST API + static frontend |

---

## 4. Functional Requirements

### FR-Scheduled: Daily Scheduled Execution

| ID | Requirement | Acceptance Criteria |
|----|-------------|---------------------|
| FR-SCH-001 | The service MUST be triggerable via a GitHub Actions cron job at 09:00 CST (01:00 UTC) daily. | GitHub Actions workflow file present; cron expression `0 1 * * *`; job completes exit 0 on success. |
| FR-SCH-002 | On scheduled trigger, the agent MUST fetch all configured default categories. | All configured categories are processed in a single run unless explicitly overridden. |
| FR-SCH-003 | Default categories MUST be configurable via `DEFAULT_CATEGORIES` (comma-separated). | Changing `DEFAULT_CATEGORIES=科技/AI,金融` and re-running processes only those two categories. |
| FR-SCH-004 | The scheduled run MUST complete all fetching, processing, verification, and publishing within 15 minutes. | GitHub Actions run wall-clock time <= 15 min under normal load. |
| FR-SCH-005 | The agent MUST deduplicate articles within a 7-day rolling window using a URL-based fingerprint. | Re-running the same day's job does not re-publish already-published URLs. |

### FR-Conversational: On-Demand Chat API

| ID | Requirement | Acceptance Criteria |
|----|-------------|---------------------|
| FR-CON-001 | The service MUST expose `POST /api/chat` accepting JSON `{"message": "<user text>"}`. | Endpoint returns HTTP 200 with a JSON body containing a summary when a valid topic is extracted. |
| FR-CON-002 | The service MUST expose `POST /api/chat/stream` returning SSE events for progressive rendering. | Client receives `data:` events as the response is generated; final event signals `[DONE]`. |
| FR-CON-003 | The agent MUST use the LLM to extract the topic/intent from the user's message before fetching. | Given input "Tell me about AI chip news today", the system fetches from tech/AI category sources. |
| FR-CON-004 | The chat endpoint MUST return a structured JSON response including extracted topic, sources used, and AI-generated summary. | Response schema matches FR-CON spec; all fields present. |
| FR-CON-005 | The chat endpoint MUST respond within 30 seconds under normal conditions. | P95 latency <= 30 s measured over 20 test requests. |
| FR-CON-006 | The chat endpoint MUST support token-based authentication when `CHAT_API_TOKEN` is configured. | Requests without a valid token return HTTP 401. |
| FR-CON-007 | The chat endpoint SHOULD apply per-IP rate limiting when `CHAT_RATE_LIMIT_PER_MIN` is set. | Exceeding the rate limit returns HTTP 429. |
| FR-CON-008 | The conversational API SHOULD maintain session context across follow-up questions. | A follow-up "tell me more about that" refines the previous query without re-extracting topic. |

### FR-Management: REST API

| ID | Requirement | Acceptance Criteria |
|----|-------------|---------------------|
| FR-MGT-001 | The service MUST expose `GET /api/articles` for listing articles with pagination, category filter, and status filter. | `/api/articles?page=1&page_size=20&category=科技/AI&status=published` returns filtered results. |
| FR-MGT-002 | The service MUST expose `GET /api/articles/:id` for article detail. | Returns full article fields including summary and metadata. |
| FR-MGT-003 | The service MUST expose `POST /api/articles/:id/publish` to manually push an article to the website API. | Returns 200 on success; article status changes to "published". |
| FR-MGT-004 | The service MUST expose `DELETE /api/articles/:id` to remove an article. | Article is deleted from the database. |
| FR-MGT-005 | The service MUST expose `POST /api/fetch` to trigger a full scheduled fetch run via the API. | Returns a `run_id`; articles appear in the database after completion. |
| FR-MGT-006 | The service MUST expose `POST /api/fetch/:category` to trigger a fetch for a specific category. | Only articles matching the given category are fetched and processed. |
| FR-MGT-007 | The service MUST expose `GET /api/stats` returning daily run statistics. | Returns fetch count, publish count, skip count, and source distribution. |
| FR-MGT-008 | The service MUST persist per-article bookmark and read state. | `PATCH /api/articles/:id/flags` with `{bookmarked?, read?}`; omitted fields unchanged; `read=false` undoes; list API supports `bookmarked` / `unread` filters. |
| FR-MGT-008 | The service MUST expose SSE `GET /api/fetch/stream` for real-time fetch progress. | Client receives progress events as fetching and processing proceed. |

### FR-Fetching: Data Source Adapters

| ID | Requirement | Acceptance Criteria |
|----|-------------|---------------------|
| FR-FET-001 | The agent MUST implement an RSS adapter that fetches and parses RSS 2.0 / Atom feeds from a configurable list of URLs. | Given a valid RSS URL, the adapter returns a list of `NewsItem` structs with title, URL, published date, and raw content. |
| FR-FET-002 | RSS feed URLs MUST be configurable via `RSS_FEEDS` (semicolon-separated). | Adding a new URL to `RSS_FEEDS` without code changes causes it to be fetched on next run. |
| FR-FET-003 | The agent MUST implement a NewsAPI adapter using the `v2/everything` endpoint, authenticated via `NEWSAPI_KEY`. | Adapter returns items matching a keyword query; returns empty list (not error) if API returns 0 results. |
| FR-FET-004 | The agent MUST implement an RSSHub adapter that consumes RSSHub-generated feeds. Routes configurable via `RSSHUB_ROUTES`. | Given a valid RSSHub route, adapter returns parsed items identical in shape to the RSS adapter output. |
| FR-FET-005 | All adapters MUST enforce a per-request HTTP timeout of 10 seconds. | A mock server that hangs returns an error (not a hang) after 10 s. |
| FR-FET-006 | All adapters MUST return a typed error if the source is unreachable, without crashing the pipeline. | Killing one source's network endpoint causes the others to continue processing. |
| FR-FET-007 | The agent MUST deduplicate fetched items by URL against a rolling cache file. | An identical URL fetched in a subsequent run within 7 days is excluded. |

### FR-AI-Processing: LLM Integration

| ID | Requirement | Acceptance Criteria |
|----|-------------|---------------------|
| FR-AI-001 | The agent MUST call an OpenAI-compatible LLM API using `LLM_API_KEY`, `LLM_BASE_URL`, and `LLM_MODEL_ID`. | API calls use `Authorization: Bearer $LLM_API_KEY` header; model field equals env var value. |
| FR-AI-002 | The agent MUST categorize each item into exactly one of: 金融, 政治, 经济, 科技/AI, 国际. | Each processed item has a non-empty `Category` field set to one of the five values. |
| FR-AI-003 | The agent MUST generate a concise Chinese-language summary (target: 100–200 Chinese characters) for each item. | Each published article contains a `summary` field of 100–200 Chinese characters. |
| FR-AI-004 | The agent MUST assign a source credibility score (float64, 0.0–1.0) to each item by asking the LLM to evaluate the source domain. | Every processed item has a `credibility_score` between 0.0 and 1.0 inclusive. |
| FR-AI-005 | AI processing MUST use batched API calls where possible (up to 10 items per request) to reduce latency and API cost. | A run of 50 items makes no more than 5 categorization API calls. |
| FR-AI-006 | If the LLM API is unavailable or returns a non-2xx error, the agent MUST log the error and skip AI-dependent steps for affected items (graceful degradation). | Mocking LLM to return HTTP 503 results in zero panics; affected items are logged as skipped. |
| FR-AI-007 | The agent MUST support function/tool calling for structured output parsing. | LLM responses are parsed from JSON embedded in tool call arguments, not free-form text. |

### FR-Verification: Source Credibility

| ID | Requirement | Acceptance Criteria |
|----|-------------|---------------------|
| FR-VER-001 | The agent MUST maintain a domain whitelist configurable via `TRUSTED_DOMAINS` (comma-separated). | Default list includes trusted Chinese and international sources. Adding a domain via env var takes effect without code changes. |
| FR-VER-002 | An item MUST be published if its source domain is in the whitelist, regardless of AI score. | An item from `reuters.com` with AI score 0.3 is still published. |
| FR-VER-003 | An item from a non-whitelisted domain MUST only be published if its AI credibility score >= 0.7. | An item from an unknown blog with score 0.65 is skipped; score 0.72 is published. |
| FR-VER-004 | All skipped items MUST be logged with reason (`low_credibility_score`, `domain_not_whitelisted_and_score_below_threshold`). | Log output contains structured field `skip_reason` for every skipped item. |
| FR-VER-005 | The verification step MUST be skippable per-run via `SKIP_VERIFICATION=true` for debugging purposes. | Setting `SKIP_VERIFICATION=true` causes all items to pass verification regardless of score. |

### FR-Publishing: Website API Integration

| ID | Requirement | Acceptance Criteria |
|----|-------------|---------------------|
| FR-PUB-001 | The agent MUST POST each verified article to `POST $WEBSITE_API_BASE_URL/api/agent/articles` with a Bearer token from `WEBSITE_API_TOKEN`. | HTTP request contains `Authorization: Bearer <token>` header; body matches API contract. |
| FR-PUB-002 | On HTTP 4xx response from the website API (except 409 Conflict), the agent MUST log the error and skip the item (no retry). | Mock 400 response causes item to be logged as permanently failed. |
| FR-PUB-003 | On HTTP 409 (duplicate), the agent MUST log the item as already-published and continue without retry. | Mock 409 response causes log entry `already_published=true`; no panic. |
| FR-PUB-004 | On HTTP 5xx or network error, the agent MUST retry up to 3 times with exponential backoff (1s, 2s, 4s). | Mock 503 that clears after 2 attempts results in successful publish on 3rd attempt. |
| FR-PUB-005 | The agent MUST log the HTTP response status and article URL for every publish attempt. | Log output contains `status`, `url`, and `attempt` fields for each publish call. |
| FR-PUB-006 | Publishing MUST be disableable by leaving `WEBSITE_API_BASE_URL` / `WEBSITE_API_TOKEN` empty (local-first mode). | With empty env vars, the pipeline completes without attempting to publish. |

### FR-Storage: Database Persistence

| ID | Requirement | Acceptance Criteria |
|----|-------------|---------------------|
| FR-DB-001 | The agent MUST persist all processed articles to PostgreSQL when `DATABASE_DSN` is configured. | Articles table contains all fields (title, summary, category, score, status, timestamps, run_id). |
| FR-DB-002 | The agent MUST support full-text search on articles using PostgreSQL FTS (Chinese + English). | A search query matches article titles and summaries within 2 seconds. |
| FR-DB-003 | The agent MUST log each pipeline run in a `run_logs` table with duration, counts, and error messages. | After a scheduled run, the run_logs table contains one row with all metrics. |
| FR-DB-004 | Database migrations MUST be embedded in the binary and auto-applied at startup. | Running the binary with a fresh database creates all required tables automatically. |

### FR-Notification: Email Digest

| ID | Requirement | Acceptance Criteria |
|----|-------------|---------------------|
| FR-NOT-001 | The agent MUST send a daily summary email when SMTP is configured. | Email is sent after scheduled run completes; contains article count per category and top articles. |
| FR-NOT-002 | The agent MUST respect SMTP configuration: host, port, user, password, from address, and recipient. | Changing `NOTIFY_EMAIL` changes the recipient without code changes. |
| FR-NOT-003 | Email notification MUST be disableable by leaving SMTP fields empty. | With empty SMTP config, the pipeline completes without attempting to send email. |

### FR-Frontend: Web GUI

| ID | Requirement | Acceptance Criteria |
|----|-------------|---------------------|
| FR-UI-001 | The GUI MUST provide a chat view for conversational news queries (`/api/chat`). | User can type a question and receive an AI-generated summary with source links. |
| FR-UI-002 | The GUI MUST provide an article list view with category and status filtering. | Articles are displayed in a table/list with filter controls, pagination, and search. |
| FR-UI-003 | The GUI MUST provide a statistics dashboard showing daily run metrics. | Dashboard shows total fetched, published, skipped, and failed counts with source distribution. |
| FR-UI-004 | The GUI MUST provide a settings panel for viewing and toggling configuration. | Settings panel displays current config values (read-only or toggle switches for boolean flags). |
| FR-UI-005 | The GUI MUST be served as static files by the Go binary in server mode. | Navigating to `http://localhost:8080/` in a browser renders the React app. |

---

## 5. Non-Functional Requirements

### 5.1 Performance

| Requirement | Target |
|-------------|--------|
| Per-article AI processing latency | <= 3 seconds average (batched) |
| Full scheduled pipeline throughput | >= 20 items/minute |
| Conversational API P95 response time | <= 30 seconds |
| Scheduled run total wall-clock time | <= 15 minutes for up to 500 raw items |
| HTTP adapter request timeout | 10 seconds per source request |
| Article search latency (FTS) | <= 2 seconds |

### 5.2 Reliability

| Requirement | Policy |
|-------------|--------|
| Publishing retry | 3 retries, exponential backoff: 1s / 2s / 4s |
| LLM API unavailability | Graceful degradation: skip AI steps, log affected items, do not crash pipeline |
| NewsAPI unavailability | Log error, continue with RSS-only sources for that run |
| Source unavailability | Log fetch error at WARN, continue with remaining sources |
| All sources unavailable | Exit 1 with fatal error |
| Database unavailability | Log error, exit 1 (articles would be lost) |
| Deduplication cache | Persist between runs using file-based cache |
| GitHub Actions failure | Exit non-zero so GitHub marks the run as failed |
| Chat rate limiting | Per-IP token bucket; HTTP 429 when exceeded |

### 5.3 Security

| Requirement | Implementation |
|-------------|---------------|
| All API keys and tokens | Stored as environment variables; never hardcoded |
| No secrets in logs | Log sanitization: config layer masks `*_key`, `*_token`, `*_secret` before they reach any handler |
| Chat API authentication | Optional shared token via `X-Api-Token` or `Authorization: Bearer` header |
| HTTP server binding | Default to `127.0.0.1` (loopback only) unless `BIND_ADDR` overrides |

### 5.4 Observability

| Requirement | Detail |
|-------------|--------|
| Structured logging | JSON-formatted logs using `log/slog`; fields: `time`, `level`, `msg`, `run_id`, `component` |
| Log levels | `DEBUG` for item-level; `INFO` for stage completions; `WARN` for skipped items; `ERROR` for retryable failures |
| Run ID | Each pipeline run tagged with a UUID propagated through all log lines |
| Stage timing | Each stage (fetch, process, verify, publish) logs duration in milliseconds |
| Fetch progress streaming | SSE endpoint `GET /api/fetch/stream` emits real-time progress events |

---

## 6. Website API Contract

The Daily Info Agent is the **caller**. The Java Spring Boot website is the **implementer**. Publishing is optional — when `WEBSITE_API_BASE_URL` is empty, the agent operates in local-first mode with database-only storage.

### 6.1 Create Article

**Endpoint**: `POST /api/agent/articles`

**Authentication**: Bearer token in `Authorization` header.

```
Authorization: Bearer <WEBSITE_API_TOKEN>
Content-Type: application/json
```

**Request Body**:

```json
{
  "source_url": "https://www.reuters.com/technology/ai-chip-...",
  "title": "Article title in original language",
  "summary": "AI生成的中文摘要，100到200个汉字，简洁概括文章核心内容。",
  "category": "科技/AI",
  "source_domain": "reuters.com",
  "credibility_score": 0.92,
  "published_at": "2026-07-03T01:30:00Z",
  "fetched_at": "2026-07-03T01:05:12Z",
  "run_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "tags": ["AI", "chip", "semiconductor"],
  "language": "en",
  "agent_version": "2.0.0"
}
```

**Request Field Schema**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `source_url` | string (URL) | Yes | Canonical URL of the original article. Used for deduplication on the Java side. |
| `title` | string | Yes | Original article title, max 512 chars. |
| `summary` | string | Yes | AI-generated Chinese summary, 100–200 Chinese characters. |
| `category` | string enum | Yes | One of: `金融`, `政治`, `经济`, `科技/AI`, `国际` |
| `source_domain` | string | Yes | Registered domain of source, e.g. `reuters.com` |
| `credibility_score` | float (0.0–1.0) | Yes | AI-assigned credibility score. |
| `published_at` | string (ISO 8601 UTC) | Yes | Original publication timestamp from the source feed. |
| `fetched_at` | string (ISO 8601 UTC) | Yes | Timestamp when the agent fetched this item. |
| `run_id` | string (UUID) | Yes | Pipeline run identifier for tracing. |
| `tags` | array of strings | No | Optional keyword tags extracted by AI. Max 10 tags, each max 50 chars. |
| `language` | string (BCP-47) | No | Detected language of original article, e.g. `en`, `zh`. Defaults to `en`. |
| `agent_version` | string | No | Semver of the agent that produced this article, e.g. `2.0.0`. |

**Success Response** — HTTP 201 Created:

```json
{
  "id": 12345,
  "source_url": "https://www.reuters.com/technology/ai-chip-...",
  "created_at": "2026-07-03T01:06:00Z",
  "status": "published"
}
```

**Conflict Response** — HTTP 409 Conflict (article with this `source_url` already exists):

```json
{
  "error": "duplicate_article",
  "message": "An article with this source_url already exists.",
  "existing_id": 12300
}
```

**Validation Error** — HTTP 400 Bad Request:

```json
{
  "error": "validation_error",
  "message": "Field 'category' must be one of: 金融, 政治, 经济, 科技/AI, 国际",
  "field": "category"
}
```

### 6.2 Idempotency Requirement

The Java API MUST treat `source_url` as a unique key. Duplicate POSTs for the same URL MUST return HTTP 409 (not 200 or 201).

### 6.3 Rate Limiting

The Java side SHOULD apply a rate limit of 60 requests/minute per token. The agent already includes a 100ms delay between consecutive publish calls.

---

## 7. Out of Scope (v2)

| Item | Rationale |
|------|-----------|
| Full-text article archiving | Storage cost and legal complexity; summaries are sufficient |
| Multi-user support | Personal project; single API token per service is sufficient |
| Real-time streaming via webhooks or SSE from sources | Polling/cron is sufficient for daily digest use case |
| User-facing feed management UI | Configuration via environment variables is acceptable |
| Sentiment analysis | Credibility scoring is the higher-priority signal |
| Push notifications (mobile) | Website + email covers the primary use cases |
| Self-hosted LLM fallback | DeepSeek API is the primary provider; configurable via LLM_BASE_URL |
| Image/media extraction from articles | Text-only summaries are sufficient |
| Prometheus metrics endpoint | Structured logs are sufficient for v2 observability |
| Support for non-RSS sources (Twitter/X, Telegram) | RSSHub covers social proxying |
| Desktop/mobile app | Web GUI is sufficient for the management use case |

---

## 8. Environment Variable Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `LLM_API_KEY` | Yes | — | LLM API authentication key |
| `LLM_MODEL_ID` | Yes | — | LLM model identifier |
| `LLM_BASE_URL` | No | `https://api.deepseek.com/v1` | LLM API base URL |
| `NEWSAPI_KEY` | No | — | NewsAPI v2 API key (blank = skip NewsAPI) |
| `RSSHUB_BASE_URL` | No | `https://rsshub.app` | Base URL for RSSHub instance |
| `RSSHUB_ROUTES` | No | built-in list | Semicolon-separated RSSHub route paths |
| `RSS_FEEDS` | No | built-in list | Semicolon-separated RSS feed URLs |
| `TRUSTED_DOMAINS` | No | built-in list | Comma-separated trusted domain whitelist |
| `SKIP_VERIFICATION` | No | `false` | Bypass credibility checks (debug only) |
| `DEFAULT_CATEGORIES` | No | all five categories | Comma-separated categories for scheduled mode |
| `DATABASE_DSN` | No | — | PostgreSQL DSN (blank = no persistence) |
| `WEBSITE_API_BASE_URL` | No | — | Java website base URL (blank = no publishing) |
| `WEBSITE_API_TOKEN` | No | — | Bearer token for website API |
| `SMTP_HOST` | No | — | SMTP server hostname (blank = no email) |
| `SMTP_PORT` | No | `587` | SMTP server port |
| `SMTP_USER` | No | — | SMTP authentication user |
| `SMTP_PASSWORD` | No | — | SMTP authentication password |
| `SMTP_FROM` | No | `SMTP_USER` | Sender address |
| `NOTIFY_EMAIL` | No | — | Recipient for daily digest email |
| `BIND_ADDR` | No | `127.0.0.1:8080` | HTTP server listen address |
| `CHAT_API_TOKEN` | No | — | Chat API auth token (blank = no auth) |
| `CHAT_RATE_LIMIT_PER_MIN` | No | `0` | Per-IP chat rate limit (0 = unlimited) |
| `CORS_ORIGINS` | No | `*` | Comma-separated allowed CORS origins |
| `SEARCH_ENGINE_URL` | No | — | Web search URL for keyword-based fetching (e.g. DuckDuckGo) |
| `LOG_LEVEL` | No | `INFO` | Minimum log level |
| `CACHE_FILE_PATH` | No | `cache/dedup.json` | Deduplication cache file path |
| `AGENT_VERSION` | No | `1.0.0` | Agent version string |

---

*End of PRD v2.0*

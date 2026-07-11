# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- `.dockerignore` to speed up Docker builds
- `CONTRIBUTING.md` for open-source contributors
- `make help` target with auto-documented targets
- `make docker-build`, `make db-drop`, `make db-connect`, `make web-lint` targets
- `CORS_ORIGINS` env var now correctly parsed in `config.Load()`

### Fixed
- `docker-compose.yml` env var `CHAT_RATE_LIMIT` → `CHAT_RATE_LIMIT_PER_MIN` to match actual config

## [2.0.0] — 2026-07-08

### Added
- Containerized deployment via Dockerfile and docker-compose (PostgreSQL + Agent)
- CORS middleware with configurable origins (`CORS_ORIGINS` env var)
- Full-text search ranking by relevance using PostgreSQL `ts_rank`
- Search engine fetcher (DuckDuckGo HTML scrape, no API key required)
- Agent `search_stored_articles` tool bridging scheduled data with chat
- Agent `search_news` tool for on-demand web search
- Agent `get_current_time` tool for temporal context
- `GET /api/fetch/:run_id` status endpoint for run tracking
- SessionStore LRU eviction (max 1000 sessions)
- Chat API per-IP rate limiting (`CHAT_RATE_LIMIT_PER_MIN`)
- Chat API token authentication (`CHAT_API_TOKEN`)
- SSE streaming chat (`POST /api/chat/stream`)
- Multiple conversation support with create/delete in the frontend
- Markdown rendering for assistant messages
- Frontend settings panel for Chat API token management
- Email notifications via SMTP (`notifier` package)
- PostgreSQL storage with migrations (`store` package)
- FTS migration for Chinese + English full-text search
- User-Agent header on all outbound fetches

### Changed
- NewsAPI is now optional (gracefully skipped when `NEWSAPI_KEY` is empty)
- API routes register without database, returning clear error messages
- `DEFAULT_CATEGORIES` validated on config load
- RSS content truncated on UTF-8 rune boundary to avoid JSON corruption
- SMTP send bounded by context with a 30s ceiling
- `RunLogRow` and `ArticleRow` now have snake_case JSON tags
- Agent version configurable via `AGENT_VERSION` env var
- Frontend error messages improved for common failure scenarios

### Fixed
- DB tags NULL error when articles have no tags
- NewsAPI placeholder detection in `main.go`
- Dedup `ByTitle` output made deterministic (stable sort by cluster index)
- Agent stream hitting max iterations mid-tool now forces a final answer
- LLM content/reasoning_content fallback for reasoning models
- Processor no longer publishes articles the LLM didn't enrich
- Scheduler never publishes articles without AI-processed data
- Fetcher cache write skipped when nothing changed
- React key warning in StatsPanel recentRuns table
- Store integration test isolation
- CI npm registry mismatch causing lockfile issues

### Performance
- Batch AI processing (up to 10 items per LLM call)
- Concurrent fetching across sources
- PostgreSQL FTS replaces ILIKE scan for keyword search

### Documentation
- PRD v2.0, Design v2.0, Dev Guide, and Roadmap rewritten to match current codebase
- `.env.example` synced with `config.go`

## [1.0.0] — 2026-06-19

### Added
- Initial project scaffolding
- Core pipeline: fetch → process → verify → publish
- RSS feed parsing via `gofeed`
- NewsAPI integration
- AI processing via OpenAI-compatible API (DeepSeek)
- Source credibility verification with domain whitelist
- Deduplication by URL with file-based rolling cache
- Near-duplicate title detection (Jaccard + Union-Find)
- Publisher client for Java website API with exponential backoff
- HTTP server with Echo framework
- Chat API (`POST /api/chat`)
- Session management with history retention
- React frontend with Vite + Tailwind CSS + shadcn/ui
- GitHub Actions: daily cron fetch + CI
- Structured logging with `log/slog`
- Configuration via environment variables
- Standalone tool calling in the agent (no LLM framework dependency)

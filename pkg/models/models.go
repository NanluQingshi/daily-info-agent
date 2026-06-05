package models

import "time"

// -----------------------------------------------------------------------
// Category
// -----------------------------------------------------------------------

// Category represents one of the five news categories.
type Category string

const (
	CategoryFinance       Category = "金融"
	CategoryPolitics      Category = "政治"
	CategoryEconomy       Category = "经济"
	CategoryTechAI        Category = "科技/AI"
	CategoryInternational Category = "国际"
)

// AllCategories is the canonical ordered list used for validation and defaults.
var AllCategories = []Category{
	CategoryFinance,
	CategoryPolitics,
	CategoryEconomy,
	CategoryTechAI,
	CategoryInternational,
}

// DisplayName returns a bilingual label used in prompts and logs.
func (c Category) DisplayName() string {
	switch c {
	case CategoryFinance:
		return "金融 (Finance)"
	case CategoryPolitics:
		return "政治 (Politics)"
	case CategoryEconomy:
		return "经济 (Economy)"
	case CategoryTechAI:
		return "科技/AI (Tech/AI)"
	case CategoryInternational:
		return "国际 (International)"
	default:
		return string(c)
	}
}

// IsValid reports whether c is one of the five known categories.
func (c Category) IsValid() bool {
	for _, known := range AllCategories {
		if c == known {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------
// RawItem — output of any Fetcher, before AI processing
// -----------------------------------------------------------------------

// SourceType identifies which adapter produced the item.
type SourceType string

const (
	SourceTypeRSS     SourceType = "rss"
	SourceTypeNewsAPI SourceType = "newsapi"
	SourceTypeRSSHub  SourceType = "rsshub"
)

// RawItem is the normalised output of any data-source adapter.
// All fields from heterogeneous sources are mapped into this common shape.
type RawItem struct {
	// Identity
	URL          string     `json:"url"`           // canonical article URL (used as dedup key)
	SourceDomain string     `json:"source_domain"` // registered domain, e.g. "reuters.com"
	SourceType   SourceType `json:"source_type"`

	// Content
	Title       string `json:"title"`
	Description string `json:"description"` // raw excerpt / feed description
	Content     string `json:"content"`     // full text if available; may be empty

	// Timing
	PublishedAt time.Time `json:"published_at"`
	FetchedAt   time.Time `json:"fetched_at"`

	// Language hint from feed metadata (BCP-47, e.g. "en", "zh")
	Language string `json:"language"`
}

// -----------------------------------------------------------------------
// FetchConfig — per-source configuration
// -----------------------------------------------------------------------

// FetchConfig holds all parameters for a single source endpoint.
type FetchConfig struct {
	Type       SourceType
	URL        string            // feed URL or NewsAPI endpoint
	Categories []Category        // categories this source is expected to cover
	Params     map[string]string // extra query params (e.g. NewsAPI "q", "language")
	Timeout    time.Duration     // defaults to 10s if zero
}

// -----------------------------------------------------------------------
// AIBatchRequest / AIBatchResponse — internal processor types
// -----------------------------------------------------------------------

// AIBatchRequest groups up to 10 RawItems for a single DeepSeek API call.
type AIBatchRequest struct {
	Items []*RawItem
	RunID string
}

// AIItemResult holds the AI output for one RawItem.
type AIItemResult struct {
	URL              string   `json:"url"`               // echoed back for correlation
	Category         Category `json:"category"`
	Summary          string   `json:"summary"`           // 100–200 Chinese characters
	CredibilityScore float64  `json:"credibility_score"` // 0.0 – 1.0
	Tags             []string `json:"tags"`              // up to 10 keywords
	Language         string   `json:"language"`          // detected BCP-47 language
}

// -----------------------------------------------------------------------
// VerificationResult
// -----------------------------------------------------------------------

// SkipReason is a machine-readable explanation for why an item was not published.
type SkipReason string

const (
	SkipReasonLowScore       SkipReason = "low_credibility_score"
	SkipReasonNotWhitelisted SkipReason = "domain_not_whitelisted_and_score_below_threshold"
)

// VerificationResult is produced by the verifier for every processed article.
type VerificationResult struct {
	Pass       bool
	SkipReason SkipReason // empty when Pass == true
	DomainHit  bool       // true if domain was in whitelist
}

// -----------------------------------------------------------------------
// ProcessedArticle — fully enriched, ready-to-publish item
// -----------------------------------------------------------------------

// ProcessedArticle is a RawItem enriched with AI outputs and verification.
type ProcessedArticle struct {
	// Embedded raw data
	Raw *RawItem

	// AI results
	Category         Category
	Summary          string
	CredibilityScore float64
	Tags             []string
	DetectedLanguage string

	// Verification
	Verification VerificationResult

	// Pipeline provenance
	RunID        string
	AgentVersion string
}

// -----------------------------------------------------------------------
// PublishRequest — wire format POSTed to the Java website API
// -----------------------------------------------------------------------

// PublishRequest is the exact JSON body sent to POST /api/agent/articles.
// Field names use snake_case to match the Java API contract.
type PublishRequest struct {
	SourceURL        string   `json:"source_url"`
	Title            string   `json:"title"`
	Summary          string   `json:"summary"`
	Category         string   `json:"category"`          // string, not Category type, for JSON portability
	SourceDomain     string   `json:"source_domain"`
	CredibilityScore float64  `json:"credibility_score"`
	PublishedAt      string   `json:"published_at"`      // ISO 8601 UTC
	FetchedAt        string   `json:"fetched_at"`        // ISO 8601 UTC
	RunID            string   `json:"run_id"`
	Tags             []string `json:"tags,omitempty"`
	Language         string   `json:"language,omitempty"`
	AgentVersion     string   `json:"agent_version,omitempty"`
}

// PublishResponse is the HTTP 201 body returned by the Java API.
type PublishResponse struct {
	ID        int64  `json:"id"`
	SourceURL string `json:"source_url"`
	CreatedAt string `json:"created_at"`
	Status    string `json:"status"`
}

// PublishErrorResponse is the body of 4xx / 5xx responses.
type PublishErrorResponse struct {
	Error      string `json:"error"`
	Message    string `json:"message"`
	Field      string `json:"field,omitempty"`       // validation errors
	ExistingID int64  `json:"existing_id,omitempty"` // 409 only
}

// -----------------------------------------------------------------------
// Chat API types
// -----------------------------------------------------------------------

// ChatRequest is the JSON body of POST /api/chat.
type ChatRequest struct {
	Message string `json:"message"`
}

// ChatSource is a single source article referenced in a chat response.
type ChatSource struct {
	URL          string  `json:"url"`
	Title        string  `json:"title"`
	SourceDomain string  `json:"source_domain"`
	CredScore    float64 `json:"credibility_score"`
}

// ChatResponse is the JSON body returned by POST /api/chat.
type ChatResponse struct {
	ExtractedTopic string       `json:"extracted_topic"`
	Category       string       `json:"category"`
	Summary        string       `json:"summary"`     // AI-generated aggregate summary in Chinese
	Sources        []ChatSource `json:"sources"`
	FetchedAt      string       `json:"fetched_at"`  // ISO 8601
	LatencyMs      int64        `json:"latency_ms"`
}

// ChatErrorResponse is the JSON body returned on errors by POST /api/chat.
type ChatErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// -----------------------------------------------------------------------
// RunResult — summary returned by scheduler after a scheduled run
// -----------------------------------------------------------------------

// RunResult is returned by scheduler.Run and used for exit-code decisions.
type RunResult struct {
	RunID          string
	TotalFetched   int
	TotalProcessed int
	TotalSaved     int
	TotalPublished int
	TotalSkipped   int
	TotalFailed    int
	DurationMs     int64
	FatalError     error // non-nil causes exit 1
}

// -----------------------------------------------------------------------
// Database / persistence types
// -----------------------------------------------------------------------

// ArticleRow is the flat database projection of a stored article.
type ArticleRow struct {
	ID               int64
	RunID            string
	SourceURL        string
	Title            string
	Description      string
	Content          string
	Summary          string
	Category         Category
	SourceDomain     string
	SourceType       string
	CredibilityScore float64
	Tags             []string
	Language         string
	DetectedLanguage string
	AgentVersion     string
	VerificationPass bool
	SkipReason       SkipReason
	DomainHit        bool
	Status           string // "pending" | "published" | "skipped" | "failed"
	ExternalID       *int64 // nullable; set after publishing to Java API
	PublishedAt      *time.Time
	FetchedAt        time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ArticleFilter holds optional filter and pagination parameters for ListArticles.
// Zero/nil values mean "no filter".
type ArticleFilter struct {
	Category *Category
	Status   *string
	DateFrom *time.Time
	DateTo   *time.Time
	Query    string // full-text search on title and summary (ILIKE); empty means no filter
	Page     int    // 1-based; defaults to 1
	PageSize int    // defaults to 20; max 100
}

// ArticleListResponse is the JSON body returned by GET /api/articles.
type ArticleListResponse struct {
	Articles   []ArticleRow `json:"articles"`
	Total      int          `json:"total"`
	Page       int          `json:"page"`
	PageSize   int          `json:"page_size"`
	TotalPages int          `json:"total_pages"`
}

// RunLogRow maps to the run_logs table.
type RunLogRow struct {
	RunID          string
	TotalFetched   int
	TotalProcessed int
	TotalSaved     int
	TotalPublished int
	TotalSkipped   int
	TotalFailed    int
	DurationMs     int64
	FatalError     string
	StartedAt      time.Time
	FinishedAt     time.Time
}

// StatsResult is returned by GET /api/stats.
type StatsResult struct {
	ByDay      []DayStat      `json:"by_day"`
	ByCategory []CategoryStat `json:"by_category"`
	RecentRuns []RunLogRow    `json:"recent_runs"`
}

// DayStat holds article count for a single day.
type DayStat struct {
	Date  string `json:"date"`  // "2026-06-01"
	Count int    `json:"count"`
}

// CategoryStat holds article count for a single category.
type CategoryStat struct {
	Category Category `json:"category"`
	Count    int      `json:"count"`
}

// FetchTriggerResponse is returned by POST /api/fetch.
type FetchTriggerResponse struct {
	RunID     string `json:"run_id"`
	Triggered bool   `json:"triggered"`
	Message   string `json:"message"`
}

// ProgressEvent is an SSE event streamed to the client during a pipeline run.
// Stage values: fetch | process | verify | publish | done | error
// Status values: running | done | error
type ProgressEvent struct {
	Stage   string `json:"stage"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Count   int    `json:"count,omitempty"`
	Passed  int    `json:"passed,omitempty"`
	Skipped int    `json:"skipped,omitempty"`
	Failed  int    `json:"failed,omitempty"`
	RunID   string `json:"run_id,omitempty"`
}

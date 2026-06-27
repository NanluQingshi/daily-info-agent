package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/user/daily-info-agent/pkg/models"
)

// defaultRSSFeeds is the built-in list used when RSS_FEEDS is not set.
// defaultRSSFeeds is the built-in list used when RSS_FEEDS is not set.
// All feeds have been verified accessible from mainland China (2026-06).
var defaultRSSFeeds = []string{
	"https://36kr.com/feed",                         // 36氪 — 科技/创投
	"https://sspai.com/feed",                        // 少数派 — 科技/效率
	"https://www.ifanr.com/feed",                    // 爱范儿 — 科技消费
	"https://feeds.feedburner.com/cnbeta",           // cnBeta — 科技资讯
	"https://rss.huxiu.com/",                        // 虎嗅 — 科技深度
	"https://www.guancha.cn/rss.xml",               // 观察者网 — 国际/政治
	"https://www.pingwest.com/feed",                 // PingWest — 科技（双语）
	"http://www.people.com.cn/rss/politics.xml",    // 人民日报 — 政治
	"http://www.people.com.cn/rss/finance.xml",     // 人民日报 — 财经
}

// defaultRSSHubRoutes is the built-in list of RSSHub route paths used when
// RSSHUB_ROUTES is not set. These are appended to RSSHUB_BASE_URL at runtime.
// Set RSSHUB_BASE_URL to your own RSSHub instance; the public rsshub.app is
// blocked in mainland China.
var defaultRSSHubRoutes = []string{
	"/wallstreetcn/news/global",    // 华尔街见闻 — 全球财经
	"/cls/telegraph",               // 财联社电报 — 实时财经
	"/jin10/flash_news",            // 金十数据 — 财经快讯
	"/36kr/news/technology",        // 36氪科技
	"/huxiu/article",               // 虎嗅文章
	"/zaobao/realtime/china",       // 联合早报 — 中国新闻
	"/xinhua/world",                // 新华社国际
}

// defaultTrustedDomains is the built-in whitelist used when TRUSTED_DOMAINS is not set.
var defaultTrustedDomains = []string{
	"xinhua.net",
	"people.com.cn",
	"gov.cn",
	"reuters.com",
	"bbc.com",
	"theverge.com",
	"apnews.com",
	"ft.com",
	"wsj.com",
	"economist.com",
}

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	// LLM (OpenAI-compatible API)
	LLMAPIKey  string
	LLMModelID string
	LLMBaseURL string // default: "https://api.deepseek.com/v1"

	// Data sources
	NewsAPIKey    string
	RSSHubBaseURL string   // default: "https://rsshub.app"
	RSSFeeds      []string // parsed from semicolon-separated env var
	RSSHubRoutes  []string // parsed from semicolon-separated env var (route paths)

	// Verification
	TrustedDomains    []string // parsed from comma-separated env var
	SkipVerification  bool
	DefaultCategories []models.Category

	// Publishing (optional — leave blank to disable Java API publishing)
	WebsiteAPIBaseURL    string
	WebsiteAPIToken      string
	DisableJavaPublisher bool // true when WebsiteAPIBaseURL or WebsiteAPIToken is empty

	// Database (optional — leave blank to disable persistence)
	DatabaseDSN string // postgres://user:pass@localhost:5432/daily_info?sslmode=disable

	// Email notifications (optional — leave blank to disable)
	SMTPHost        string
	SMTPPort        int    // default: 587
	SMTPUser        string
	SMTPPassword    string
	SMTPFrom        string // defaults to SMTPUser when empty
	NotifyEmail     string
	DisableNotifier bool // true when any required SMTP field is missing

	// HTTP server
	BindAddr string // default: "127.0.0.1:8080"

	// Observability
	LogLevel     slog.Level
	AgentVersion string // injected at build time via -ldflags

	// Runtime
	CacheFilePath string // default: "cache/dedup.json"
}

// MissingConfigError is returned when one or more required environment variables are absent.
type MissingConfigError struct {
	Vars []string
}

func (e *MissingConfigError) Error() string {
	return fmt.Sprintf("missing required environment variables: %s", strings.Join(e.Vars, ", "))
}

// Load reads all environment variables and returns a validated Config.
// It tries to load a .env file first (for local development); if absent, it is silently ignored.
// Returns MissingConfigError listing all missing required variables.
func Load() (*Config, error) {
	// Try loading .env — ignore error if file does not exist
	_ = godotenv.Load()

	cfg := &Config{}
	var missing []string

	// Required fields
	cfg.LLMAPIKey = os.Getenv("LLM_API_KEY")
	if cfg.LLMAPIKey == "" {
		missing = append(missing, "LLM_API_KEY")
	}

	cfg.LLMModelID = os.Getenv("LLM_MODEL_ID")
	if cfg.LLMModelID == "" {
		missing = append(missing, "LLM_MODEL_ID")
	}

	cfg.NewsAPIKey = os.Getenv("NEWSAPI_KEY")
	if cfg.NewsAPIKey == "" {
		missing = append(missing, "NEWSAPI_KEY")
	}

	if len(missing) > 0 {
		return nil, &MissingConfigError{Vars: missing}
	}

	// Optional publishing config
	cfg.WebsiteAPIBaseURL = os.Getenv("WEBSITE_API_BASE_URL")
	cfg.WebsiteAPIToken = os.Getenv("WEBSITE_API_TOKEN")
	cfg.DisableJavaPublisher = cfg.WebsiteAPIBaseURL == "" || cfg.WebsiteAPIToken == ""

	// Optional database config
	cfg.DatabaseDSN = os.Getenv("DATABASE_DSN")

	// Optional email notifier config
	cfg.SMTPHost = os.Getenv("SMTP_HOST")
	cfg.SMTPPort = parsePort(os.Getenv("SMTP_PORT"), 587)
	cfg.SMTPUser = os.Getenv("SMTP_USER")
	cfg.SMTPPassword = os.Getenv("SMTP_PASSWORD")
	cfg.SMTPFrom = os.Getenv("SMTP_FROM")
	cfg.NotifyEmail = os.Getenv("NOTIFY_EMAIL")
	cfg.DisableNotifier = cfg.SMTPHost == "" || cfg.SMTPUser == "" || cfg.SMTPPassword == "" || cfg.NotifyEmail == ""

	// Optional with defaults
	cfg.LLMBaseURL = envOr("LLM_BASE_URL", "https://api.deepseek.com/v1")
	cfg.RSSHubBaseURL = envOr("RSSHUB_BASE_URL", "https://rsshub.app")
	cfg.BindAddr = envOr("BIND_ADDR", "127.0.0.1:8080")
	cfg.CacheFilePath = envOr("CACHE_FILE_PATH", "cache/dedup.json")
	cfg.AgentVersion = envOr("AGENT_VERSION", "1.0.0")

	// RSSHub routes
	if raw := os.Getenv("RSSHUB_ROUTES"); raw != "" {
		for _, r := range strings.Split(raw, ";") {
			r = strings.TrimSpace(r)
			if r != "" {
				cfg.RSSHubRoutes = append(cfg.RSSHubRoutes, r)
			}
		}
	} else {
		cfg.RSSHubRoutes = defaultRSSHubRoutes
	}

	// RSS feeds
	if raw := os.Getenv("RSS_FEEDS"); raw != "" {
		feeds := strings.Split(raw, ";")
		for _, f := range feeds {
			f = strings.TrimSpace(f)
			if f != "" {
				cfg.RSSFeeds = append(cfg.RSSFeeds, f)
			}
		}
	} else {
		cfg.RSSFeeds = defaultRSSFeeds
	}

	// Trusted domains
	if raw := os.Getenv("TRUSTED_DOMAINS"); raw != "" {
		domains := strings.Split(raw, ",")
		for _, d := range domains {
			d = strings.TrimSpace(d)
			if d != "" {
				cfg.TrustedDomains = append(cfg.TrustedDomains, d)
			}
		}
	} else {
		cfg.TrustedDomains = defaultTrustedDomains
	}

	// Default categories
	if raw := os.Getenv("DEFAULT_CATEGORIES"); raw != "" {
		parts := strings.Split(raw, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.DefaultCategories = append(cfg.DefaultCategories, models.Category(p))
			}
		}
	} else {
		cfg.DefaultCategories = []models.Category{
			models.CategoryFinance,
			models.CategoryPolitics,
			models.CategoryEconomy,
			models.CategoryTechAI,
			models.CategoryInternational,
		}
	}

	// Skip verification
	cfg.SkipVerification = strings.ToLower(os.Getenv("SKIP_VERIFICATION")) == "true"

	// Log level
	cfg.LogLevel = parseLogLevel(os.Getenv("LOG_LEVEL"))

	return cfg, nil
}

// envOr returns the value of the env var name, or fallback if it is unset/empty.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// parsePort parses a port number string, returning fallback on failure.
func parsePort(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	var p int
	if _, err := fmt.Sscanf(raw, "%d", &p); err != nil || p <= 0 || p > 65535 {
		return fallback
	}
	return p
}

// parseLogLevel converts a string log level to slog.Level, defaulting to INFO.
func parseLogLevel(raw string) slog.Level {
	switch strings.ToUpper(raw) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

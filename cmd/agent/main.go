// Package main is the entry point for the daily-info-agent binary.
//
// Usage:
//
//	./agent [--mode=schedule|server]
//
// Flags:
//
//	--mode=schedule  Run the scheduled pipeline once and exit.
//	--mode=server    Start the conversational HTTP server (default).
//
// All runtime configuration is read from environment variables.
// A local .env file is loaded automatically if present.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	openai "github.com/sashabaranov/go-openai"
	"github.com/user/daily-info-agent/internal/agent"
	"github.com/user/daily-info-agent/internal/api"
	"github.com/user/daily-info-agent/internal/chat"
	"github.com/user/daily-info-agent/internal/extract"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/internal/notifier"
	"github.com/user/daily-info-agent/internal/processor"
	"github.com/user/daily-info-agent/internal/publisher"
	"github.com/user/daily-info-agent/internal/scheduler"
	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/internal/verifier"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/metrics"
)

// version is overridden at build time with: -ldflags="-X main.version=x.y.z"
var version = "1.0.0"

func main() {
	modeFlag := flag.String("mode", "server", "Operation mode: schedule or server")
	flag.Parse()

	// ---- Configuration ----
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: configuration error: %v\n", err)
		os.Exit(1)
	}
	cfg.AgentVersion = version

	// ---- Logger ----
	var handler slog.Handler
	if isCI() {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})
	}
	logger := slog.New(handler)

	logger.Info("starting daily-info-agent",
		slog.String("mode", *modeFlag),
		slog.String("version", version),
	)

	// ---- Build shared HTTP client ----
	httpClient := fetcher.WithUserAgent(&http.Client{Timeout: 10 * time.Second}, fetcher.DefaultUserAgent)

	// ---- Build fetchers ----
	fetchers := []fetcher.Fetcher{fetcher.NewRSSFetcher(httpClient)}

	// Only register NewsAPI when the key looks like a real token (not empty,
	// not a placeholder URL from .env.example, and not a dummy placeholder).
	if isPlaceholderKey(cfg.NewsAPIKey) {
		logger.Info("NewsAPI fetcher disabled (NEWSAPI_KEY not set or is a placeholder)")
	} else {
		fetchers = append(fetchers, fetcher.NewNewsAPIFetcher(cfg.NewsAPIKey, httpClient))
		logger.Info("NewsAPI fetcher enabled")
	}

	fetchers = append(fetchers, fetcher.NewRSSHubFetcher(cfg.RSSHubBaseURL, httpClient))

	// Search engine fetcher (optional — set SEARCH_ENGINE_URL to enable)
	if cfg.SearchEngineEnabled {
		fetchers = append(fetchers, fetcher.NewSearchFetcher(
			cfg.SearchEngineURL,
			httpClient,
			logger.With(slog.String("component", "search")),
		))
		logger.Info("search engine fetcher enabled", slog.String("url", cfg.SearchEngineURL))
	} else {
		logger.Info("search engine fetcher disabled (SEARCH_ENGINE_URL not set)")
	}

	mgr := fetcher.NewManager(
		fetchers,
		cfg.RSSFeeds,
		cfg.RSSHubRoutes,
		cfg.CacheFilePath,
		logger.With(slog.String("component", "fetcher")),
	)

	// ---- Build processor ----
	openAICfg := openai.DefaultConfig(cfg.LLMAPIKey)
	openAICfg.BaseURL = cfg.LLMBaseURL
	openAICfg.HTTPClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		},
	}
	aiClient := openai.NewClientWithConfig(openAICfg)

	proc := processor.New(
		aiClient,
		cfg.LLMModelID,
		logger.With(slog.String("component", "processor")),
	)

	// Optional local LLM fallback (e.g. Ollama) when the primary API is down.
	if cfg.LLMFallbackBaseURL != "" && cfg.LLMFallbackModelID != "" {
		fbCfg := openai.DefaultConfig("ollama") // local instances ignore the key
		fbCfg.BaseURL = cfg.LLMFallbackBaseURL
		fbClient := openai.NewClientWithConfig(fbCfg)
		proc = proc.WithFallback(fbClient, cfg.LLMFallbackModelID)
		logger.Info("local LLM fallback enabled",
			slog.String("url", cfg.LLMFallbackBaseURL),
			slog.String("model", cfg.LLMFallbackModelID),
		)
	}

	// ---- Build verifier ----
	ver := verifier.New(
		cfg.TrustedDomains,
		cfg.SkipVerification,
		logger.With(slog.String("component", "verifier")),
	)

	// ---- Build publisher (optional) ----
	var pub *publisher.Client
	if !cfg.DisableJavaPublisher {
		pub = publisher.New(
			cfg.WebsiteAPIBaseURL,
			cfg.WebsiteAPIToken,
			&http.Client{Timeout: 30 * time.Second},
			logger.With(slog.String("component", "publisher")),
		)
	} else {
		logger.Info("Java API publishing disabled (WEBSITE_API_BASE_URL / WEBSITE_API_TOKEN not set)")
	}

	// ---- Build store (optional) ----
	var articleStore store.ArticleStore
	if cfg.DatabaseDSN != "" {
		// Run migrations first
		if err := runMigrations(cfg.DatabaseDSN, logger); err != nil {
			logger.Error("database migration failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		pg, err := store.NewPostgresStore(context.Background(), cfg.DatabaseDSN)
		if err != nil {
			logger.Error("failed to connect to database", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer pg.Close()
		articleStore = pg
		logger.Info("database connected", slog.String("dsn", maskDSN(cfg.DatabaseDSN)))
	} else {
		logger.Info("database persistence disabled (DATABASE_DSN not set)")
	}

	// ---- Build notifier (optional — schedule mode only) ----
	var notif *notifier.Notifier
	if !cfg.DisableNotifier {
		notif = notifier.New(
			cfg.SMTPHost, cfg.SMTPPort,
			cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPFrom,
			cfg.NotifyEmail,
			logger.With(slog.String("component", "notifier")),
		)
		logger.Info("email notifier enabled", slog.String("notify_email", cfg.NotifyEmail))
	} else {
		logger.Info("email notifier disabled (SMTP_HOST / SMTP_USER / SMTP_PASSWORD / NOTIFY_EMAIL not set)")
	}

	// ---- Build full-text extractor (optional) ----
	var extractor *extract.Extractor
	if cfg.FulltextEnabled {
		extractor = extract.New(
			httpClient,
			cfg.FulltextMaxItems,
			cfg.FulltextConcurrency,
			logger.With(slog.String("component", "extract")),
		)
		logger.Info("full-text extraction enabled",
			slog.Int("max_items_per_run", cfg.FulltextMaxItems),
			slog.Int("concurrency", cfg.FulltextConcurrency),
		)
	} else {
		logger.Info("full-text extraction disabled (FULLTEXT_ENABLED=false)")
	}

	// ---- Dispatch mode ----
	switch *modeFlag {
	case "schedule":
		runScheduleMode(cfg, mgr, proc, ver, pub, articleStore, notif, extractor, logger)
	case "server":
		runServerMode(cfg, mgr, proc, ver, pub, articleStore, extractor, logger)
	default:
		fmt.Fprintf(os.Stderr, "FATAL: unknown mode %q (use 'schedule' or 'server')\n", *modeFlag)
		os.Exit(1)
	}
}

// runScheduleMode executes the full scheduled pipeline and exits with appropriate code.
func runScheduleMode(
	cfg *config.Config,
	mgr *fetcher.Manager,
	proc *processor.Processor,
	ver *verifier.Verifier,
	pub *publisher.Client,
	st store.ArticleStore,
	notif *notifier.Notifier,
	extractor *extract.Extractor,
	logger *slog.Logger,
) {
	sched := scheduler.New(
		mgr, proc, ver, pub, st, cfg,
		logger.With(slog.String("component", "scheduler")),
	).WithExtractor(extractor)
	if notif != nil {
		sched.WithNotifier(notif)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	result := sched.Run(ctx)

	logger.Info("scheduled run finished",
		slog.String("run_id", result.RunID),
		slog.Int("fetched", result.TotalFetched),
		slog.Int("processed", result.TotalProcessed),
		slog.Int("saved", result.TotalSaved),
		slog.Int("published", result.TotalPublished),
		slog.Int("skipped", result.TotalSkipped),
		slog.Int("failed", result.TotalFailed),
		slog.Int64("duration_ms", result.DurationMs),
	)

	if result.FatalError != nil {
		logger.Error("fatal error in scheduled run",
			slog.String("error", result.FatalError.Error()),
		)
		os.Exit(1)
	}
}

// runServerMode starts the Echo HTTP server and blocks.
func runServerMode(
	cfg *config.Config,
	mgr *fetcher.Manager,
	proc *processor.Processor,
	ver *verifier.Verifier,
	pub *publisher.Client,
	st store.ArticleStore,
	extractor *extract.Extractor,
	logger *slog.Logger,
) {
	// Pass the Postgres store as the session persistence backend when the
	// database is configured, so chat history survives server restarts.
	var sessionPersist agent.SessionPersistence
	if pg, ok := st.(*store.PostgresStore); ok {
		sessionPersist = pg
	}
	agentRunner := agent.NewWithSessionPersistence(
		cfg.LLMBaseURL,
		cfg.LLMAPIKey,
		cfg.LLMModelID,
		mgr,
		st, // nil when DATABASE_DSN is not set; search_stored_articles is disabled
		logger.With(slog.String("component", "agent")),
		sessionPersist,
	)
	chatHandler := chat.New(
		agentRunner,
		cfg.ChatAPIToken,
		cfg.ChatRateLimitPerMin,
		logger.With(slog.String("component", "chat")),
	)

	startTime := time.Now()

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.RequestID())
	e.Use(middleware.TimeoutWithConfig(middleware.TimeoutConfig{
		Timeout: 30 * time.Second,
		Skipper: func(c echo.Context) bool {
			// /api/chat can take 30–60 s when the agent calls tools;
			// /stream endpoints are SSE and must never be cut off.
			return c.Path() == "/api/chat" || strings.HasSuffix(c.Path(), "/stream")
		},
	}))
	e.Use(slogMiddleware(logger))
	e.Use(middleware.Recover())

	// CORS — allow the configured origin(s) or all in development.
	corsOrigins := cfg.CORSOrigins
	if corsOrigins == "" {
		corsOrigins = "*"
	}
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     strings.Split(corsOrigins, ","),
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Api-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Chat endpoints
	e.POST("/api/chat", chatHandler.Handle)
	e.POST("/api/chat/stream", chatHandler.HandleStream)
	e.DELETE("/api/sessions/:id", chatHandler.HandleDeleteSession)
	e.GET("/health", healthHandler(version, st, startTime, cfg.CacheFilePath, cfg.LLMAPIKey != ""))
	e.GET("/metrics", echo.WrapHandler(metricsHandlerWithSources(mgr)))

	// Article management API (database-dependent endpoints return clear
	// errors when DATABASE_DSN is not configured).
	sched := scheduler.New(
		mgr, proc, ver, pub, st, cfg,
		logger.With(slog.String("component", "scheduler")),
	).WithExtractor(extractor)
	apiHandler := api.New(st, sched, pub, cfg, logger.With(slog.String("component", "api")))
	apiHandler.Register(e.Group("/api"))

	// Serve React frontend static files
	serveStaticFrontend(e)

	go func() {
		logger.Info("starting HTTP server", slog.String("addr", cfg.BindAddr))
		if err := e.Start(cfg.BindAddr); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	// Graceful shutdown on SIGINT / SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutdown signal received", slog.String("signal", sig.String()))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
	}
	logger.Info("server stopped")
}

// serveStaticFrontend serves the React build from web/dist if it exists.
// Falls back gracefully when web/dist is absent (e.g. during backend-only development).
func serveStaticFrontend(e *echo.Echo) {
	distFS := os.DirFS("web/dist")
	// Check the directory exists before registering routes.
	if _, err := fs.Stat(distFS, "."); err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(distFS))
	e.GET("/assets/*", echo.WrapHandler(fileServer))
	e.GET("/*", func(c echo.Context) error {
		path := c.Request().URL.Path
		if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/health") {
			return echo.ErrNotFound
		}
		// SPA fallback: serve index.html for all non-API routes
		f, err := distFS.Open("index.html")
		if err != nil {
			return echo.ErrNotFound
		}
		defer f.Close()
		return c.Stream(http.StatusOK, "text/html; charset=utf-8", f)
	})
}

// runMigrations applies all pending database migrations.
func runMigrations(dsn string, logger *slog.Logger) error {
	migrationsDir := os.DirFS("migrations")
	d, err := iofs.New(migrationsDir, ".")
	if err != nil {
		return fmt.Errorf("migrations source: %w", err)
	}
	// golang-migrate's pgx/v5 driver registers as "pgx5://", but users
	// naturally write "postgres://" DSNs — rewrite the scheme here.
	migrateDSN := strings.NewReplacer(
		"postgres://", "pgx5://",
		"postgresql://", "pgx5://",
	).Replace(dsn)
	m, err := migrate.NewWithSourceInstance("iofs", d, migrateDSN)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	logger.Info("database migrations applied")
	return nil
}

// healthHandler returns a /health endpoint that reports process status,
// database connectivity, LLM configuration, and the dedup cache file state.
func healthHandler(ver string, st store.ArticleStore, startTime time.Time, cacheFilePath string, llmKeySet bool) echo.HandlerFunc {
	return func(c echo.Context) error {
		body := map[string]interface{}{
			"status":  "ok",
			"version": ver,
			"time":    time.Now().UTC().Format(time.RFC3339),
			"uptime":  time.Since(startTime).String(),
		}
		if st != nil {
			if err := st.Ping(c.Request().Context()); err != nil {
				body["db"] = "error: " + err.Error()
			} else {
				body["db"] = "ok"
			}
		} else {
			body["db"] = "disabled"
		}
		// LLM configuration status (no live call — that would add latency).
		if llmKeySet {
			body["llm"] = "configured"
		} else {
			body["llm"] = "missing_key"
		}
		// Dedup cache file state.
		if info, err := os.Stat(cacheFilePath); err == nil {
			body["cache"] = map[string]interface{}{
				"status": "ok",
				"size":   info.Size(),
				"path":   cacheFilePath,
			}
		} else if os.IsNotExist(err) {
			body["cache"] = map[string]interface{}{"status": "empty", "path": cacheFilePath}
		} else {
			body["cache"] = map[string]interface{}{"status": "error", "path": cacheFilePath, "error": err.Error()}
		}
		return c.JSON(http.StatusOK, body)
	}
}

// slogMiddleware returns an Echo middleware that logs every request with slog.
func slogMiddleware(logger *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			req := c.Request()
			res := c.Response()

			status := res.Status
			if err != nil {
				if he, ok := err.(*echo.HTTPError); ok {
					status = he.Code
				}
			}

			logger.Info("http request",
				slog.String("method", req.Method),
				slog.String("path", req.URL.Path),
				slog.Int("status", status),
				slog.Int64("latency_ms", time.Since(start).Milliseconds()),
				slog.String("request_id", c.Response().Header().Get(echo.HeaderXRequestID)),
			)
			return err
		}
	}
}

// isCI detects a CI environment to select JSON log format.
func isCI() bool {
	return os.Getenv("CI") != "" ||
		os.Getenv("GITHUB_ACTIONS") != "" ||
		os.Getenv("GITLAB_CI") != ""
}

// isPlaceholderKey reports whether an API key is empty or a known placeholder
// value (from .env.example or common dummy values), so the fetcher using it
// can be skipped at startup.
func isPlaceholderKey(key string) bool {
	return key == "" ||
		strings.HasPrefix(key, "http") ||
		strings.EqualFold(key, "placeholder") ||
		strings.EqualFold(key, "test-key") ||
		strings.EqualFold(key, "test")
}

// maskDSN replaces the password in a DSN string with "***" for logging.
func maskDSN(dsn string) string {
	if idx := strings.Index(dsn, "@"); idx != -1 {
		prefix := dsn[:idx]
		if at := strings.LastIndex(prefix, ":"); at != -1 {
			return prefix[:at+1] + "***" + dsn[idx:]
		}
	}
	return dsn
}

// metricsHandler exposes Go runtime metrics and application counters in
// a simple text/plain format. Compatible with Prometheus text format parsers.
func metricsHandler(w http.ResponseWriter, r *http.Request) {
	writeMetrics(w, r, nil)
}

// metricsHandlerWithSources additionally exposes per-source fetch health
// gauges from the fetcher manager (nil manager skips that section).
func metricsHandlerWithSources(mgr *fetcher.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeMetrics(w, r, mgr)
	}
}

func writeMetrics(w http.ResponseWriter, r *http.Request, mgr *fetcher.Manager) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Fprintf(w, "# HELP go_goroutines Number of goroutines\n")
	fmt.Fprintf(w, "# TYPE go_goroutines gauge\n")
	fmt.Fprintf(w, "go_goroutines %d\n", runtime.NumGoroutine())

	fmt.Fprintf(w, "# HELP go_mem_alloc_bytes Heap memory allocated\n")
	fmt.Fprintf(w, "# TYPE go_mem_alloc_bytes gauge\n")
	fmt.Fprintf(w, "go_mem_alloc_bytes %d\n", m.Alloc)

	fmt.Fprintf(w, "# HELP go_mem_sys_bytes System memory obtained\n")
	fmt.Fprintf(w, "# TYPE go_mem_sys_bytes gauge\n")
	fmt.Fprintf(w, "go_mem_sys_bytes %d\n", m.Sys)

	fmt.Fprintf(w, "# HELP go_gc_total Total number of GC cycles\n")
	fmt.Fprintf(w, "# TYPE go_gc_total counter\n")
	fmt.Fprintf(w, "go_gc_total %d\n", m.NumGC)

	fmt.Fprintf(w, "# HELP go_cgo_calls Number of cgo calls\n")
	fmt.Fprintf(w, "# TYPE go_cgo_calls gauge\n")
	fmt.Fprintf(w, "go_cgo_calls %d\n", runtime.NumCgoCall())

	// ── Application metrics ─────────────────────────────────────────────
	mc := &metrics.App
	fmt.Fprintf(w, "# HELP dia_items_fetched Total raw items fetched\n")
	fmt.Fprintf(w, "# TYPE dia_items_fetched counter\n")
	fmt.Fprintf(w, "dia_items_fetched %d\n", mc.ItemsFetched.Load())

	fmt.Fprintf(w, "# HELP dia_items_deduped Items removed by title dedup\n")
	fmt.Fprintf(w, "# TYPE dia_items_deduped counter\n")
	fmt.Fprintf(w, "dia_items_deduped %d\n", mc.ItemsDeduped.Load())

	fmt.Fprintf(w, "# HELP dia_items_extracted Pages whose full text was extracted\n")
	fmt.Fprintf(w, "# TYPE dia_items_extracted counter\n")
	fmt.Fprintf(w, "dia_items_extracted %d\n", mc.ItemsExtracted.Load())

	fmt.Fprintf(w, "# HELP dia_extract_failed Page extractions that failed (fell back to summary)\n")
	fmt.Fprintf(w, "# TYPE dia_extract_failed counter\n")
	fmt.Fprintf(w, "dia_extract_failed %d\n", mc.ExtractFailed.Load())

	fmt.Fprintf(w, "# HELP dia_items_processed Items through AI processing\n")
	fmt.Fprintf(w, "# TYPE dia_items_processed counter\n")
	fmt.Fprintf(w, "dia_items_processed %d\n", mc.ItemsProcessed.Load())

	fmt.Fprintf(w, "# HELP dia_llm_calls Total successful LLM API calls\n")
	fmt.Fprintf(w, "# TYPE dia_llm_calls counter\n")
	fmt.Fprintf(w, "dia_llm_calls %d\n", mc.LLMCalls.Load())

	fmt.Fprintf(w, "# HELP dia_llm_errors Total failed LLM API calls\n")
	fmt.Fprintf(w, "# TYPE dia_llm_errors counter\n")
	fmt.Fprintf(w, "dia_llm_errors %d\n", mc.LLMErrors.Load())

	fmt.Fprintf(w, "# HELP dia_items_passed Articles passing verification\n")
	fmt.Fprintf(w, "# TYPE dia_items_passed counter\n")
	fmt.Fprintf(w, "dia_items_passed %d\n", mc.ItemsPassed.Load())

	fmt.Fprintf(w, "# HELP dia_items_skipped Articles skipped\n")
	fmt.Fprintf(w, "# TYPE dia_items_skipped counter\n")
	fmt.Fprintf(w, "dia_items_skipped %d\n", mc.ItemsSkipped.Load())

	fmt.Fprintf(w, "# HELP dia_items_published Articles published\n")
	fmt.Fprintf(w, "# TYPE dia_items_published counter\n")
	fmt.Fprintf(w, "dia_items_published %d\n", mc.ItemsPublished.Load())

	fmt.Fprintf(w, "# HELP dia_publish_failed Articles failed to publish\n")
	fmt.Fprintf(w, "# TYPE dia_publish_failed counter\n")
	fmt.Fprintf(w, "dia_publish_failed %d\n", mc.PublishFailed.Load())

	fmt.Fprintf(w, "# HELP dia_publish_retried Articles published after retry\n")
	fmt.Fprintf(w, "# TYPE dia_publish_retried counter\n")
	fmt.Fprintf(w, "dia_publish_retried %d\n", mc.PublishRetried.Load())

	fmt.Fprintf(w, "# HELP dia_runs_completed Pipeline runs completed\n")
	fmt.Fprintf(w, "# TYPE dia_runs_completed counter\n")
	fmt.Fprintf(w, "dia_runs_completed %d\n", mc.RunsCompleted.Load())

	fmt.Fprintf(w, "# HELP dia_runs_failed Pipeline runs aborted\n")
	fmt.Fprintf(w, "# TYPE dia_runs_failed counter\n")
	fmt.Fprintf(w, "dia_runs_failed %d\n", mc.RunsFailed.Load())

	// ── Per-source fetch health (manager in-memory state) ───────────────
	if mgr != nil {
		snaps := mgr.Health()
		if len(snaps) > 0 {
			fmt.Fprintf(w, "# HELP dia_source_consecutive_failures Consecutive fetch failures per source\n")
			fmt.Fprintf(w, "# TYPE dia_source_consecutive_failures gauge\n")
			for _, s := range snaps {
				fmt.Fprintf(w, "dia_source_consecutive_failures{source=%q} %d\n", s.Source, s.ConsecutiveFailures)
			}
			fmt.Fprintf(w, "# HELP dia_source_disabled 1 when the source is auto-disabled after repeated failures\n")
			fmt.Fprintf(w, "# TYPE dia_source_disabled gauge\n")
			for _, s := range snaps {
				v := 0
				if s.Skipped {
					v = 1
				}
				fmt.Fprintf(w, "dia_source_disabled{source=%q} %d\n", s.Source, v)
			}
		}
	}
}

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
	"net/http"
	"os"
	"strings"
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
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/internal/notifier"
	"github.com/user/daily-info-agent/internal/processor"
	"github.com/user/daily-info-agent/internal/publisher"
	"github.com/user/daily-info-agent/internal/scheduler"
	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/internal/verifier"
	"github.com/user/daily-info-agent/pkg/config"
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
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// ---- Build fetchers ----
	fetchers := []fetcher.Fetcher{fetcher.NewRSSFetcher(httpClient)}

	// Only register NewsAPI when the key looks like a real token (not empty
	// and not the placeholder URL from .env.example).
	if cfg.NewsAPIKey != "" && !strings.HasPrefix(cfg.NewsAPIKey, "http") {
		fetchers = append(fetchers, fetcher.NewNewsAPIFetcher(cfg.NewsAPIKey, httpClient))
		logger.Info("NewsAPI fetcher enabled")
	} else {
		logger.Info("NewsAPI fetcher disabled (NEWSAPI_KEY not set or is a placeholder)")
	}

	fetchers = append(fetchers, fetcher.NewRSSHubFetcher(cfg.RSSHubBaseURL, httpClient))

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
	aiClient := openai.NewClientWithConfig(openAICfg)

	proc := processor.New(
		aiClient,
		cfg.LLMModelID,
		logger.With(slog.String("component", "processor")),
	)

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

	// ---- Dispatch mode ----
	switch *modeFlag {
	case "schedule":
		runScheduleMode(cfg, mgr, proc, ver, pub, articleStore, notif, logger)
	case "server":
		runServerMode(cfg, mgr, proc, ver, pub, articleStore, logger)
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
	logger *slog.Logger,
) {
	sched := scheduler.New(
		mgr, proc, ver, pub, st, cfg,
		logger.With(slog.String("component", "scheduler")),
	)
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
	logger *slog.Logger,
) {
	agentRunner := agent.New(
		cfg.LLMBaseURL,
		cfg.LLMAPIKey,
		cfg.LLMModelID,
		mgr,
		st, // nil when DATABASE_DSN is not set; search_stored_articles is disabled
		logger.With(slog.String("component", "agent")),
	)
	chatHandler := chat.New(
		agentRunner,
		logger.With(slog.String("component", "chat")),
	)

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

	// Chat endpoints
	e.POST("/api/chat", chatHandler.Handle)
	e.POST("/api/chat/stream", chatHandler.HandleStream)
	e.DELETE("/api/sessions/:id", chatHandler.HandleDeleteSession)
	e.GET("/health", healthHandler(version, st))

	// New article management API (requires database)
	if st != nil {
		sched := scheduler.New(
			mgr, proc, ver, pub, st, cfg,
			logger.With(slog.String("component", "scheduler")),
		)
		apiHandler := api.New(st, sched, pub, cfg, logger.With(slog.String("component", "api")))
		apiHandler.Register(e.Group("/api"))
	}

	// Serve React frontend static files
	serveStaticFrontend(e)

	logger.Info("starting HTTP server", slog.String("addr", cfg.BindAddr))
	if err := e.Start(cfg.BindAddr); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", slog.String("error", err.Error()))
		os.Exit(1)
	}
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

// healthHandler returns a /health endpoint that also reports DB connectivity.
func healthHandler(ver string, st store.ArticleStore) echo.HandlerFunc {
	return func(c echo.Context) error {
		body := map[string]string{
			"status":  "ok",
			"version": ver,
			"time":    time.Now().UTC().Format(time.RFC3339),
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

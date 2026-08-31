// Package retention prunes old run_logs and articles according to the
// RETENTION_DAYS policy (#74). Zero days disables pruning entirely.
package retention

import (
	"context"
	"log/slog"
	"time"

	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/pkg/metrics"
)

// Interval is how often the server-mode loop prunes.
const Interval = 24 * time.Hour

// Runner executes the retention policy against the store.
type Runner struct {
	days   int
	store  store.ArticleStore
	logger *slog.Logger
	now    func() time.Time // injectable clock for tests
}

// New builds a Runner; days <= 0 means disabled.
func New(days int, st store.ArticleStore, logger *slog.Logger) *Runner {
	return &Runner{days: days, store: st, logger: logger, now: time.Now}
}

// Enabled reports whether the policy is active.
func (r *Runner) Enabled() bool {
	return r.days > 0
}

// Cutoff returns the timestamp before which rows are deleted.
func (r *Runner) Cutoff() time.Time {
	return r.now().AddDate(0, 0, -r.days)
}

// Run prunes once and records the removed counts in /metrics. It is a
// no-op when disabled or the store is nil (DATABASE_DSN unset).
func (r *Runner) Run(ctx context.Context) {
	if !r.Enabled() || r.store == nil {
		return
	}

	logs, err := r.store.PruneRunLogs(ctx, r.Cutoff())
	if err != nil {
		r.logger.Error("prune run_logs failed", slog.String("error", err.Error()))
	} else if logs > 0 {
		metrics.App.RunLogsPruned.Add(logs)
		r.logger.Info("pruned run_logs", slog.Int64("count", logs))
	}

	articles, err := r.store.PruneArticles(ctx, r.Cutoff())
	if err != nil {
		r.logger.Error("prune articles failed", slog.String("error", err.Error()))
	} else if articles > 0 {
		metrics.App.ArticlesPruned.Add(articles)
		r.logger.Info("pruned articles", slog.Int64("count", articles))
	}
}

// RunForever blocks, pruning immediately and then every Interval until
// the context is cancelled (server mode).
func (r *Runner) RunForever(ctx context.Context) {
	if !r.Enabled() || r.store == nil {
		return
	}
	r.Run(ctx)
	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Run(ctx)
		}
	}
}

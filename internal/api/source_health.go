// Package api — source health endpoint (GET /api/sources/health).
package api

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/pkg/models"
)

// SourceHealthProvider supplies live per-source fetch health from the
// fetcher manager. *scheduler.Scheduler satisfies it via SourceHealth().
type SourceHealthProvider interface {
	SourceHealth() []fetcher.HealthSnapshot
}

// sourceHealthWindow is how far back the DB activity lookup reaches.
const sourceHealthWindowDays = 7

// hostOf extracts the host (sans port) used to join in-memory health with
// DB activity. Non-URL source keys (e.g. RSSHub route paths) return "".
func hostOf(source string) string {
	u, err := url.Parse(source)
	if err != nil || u.Host == "" {
		return ""
	}
	return normalizeHost(u.Host)
}

// normalizeHost strips the port and lower-cases the host.
func normalizeHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(host)
}

// healthStatus maps a snapshot to the display status.
func healthStatus(s fetcher.HealthSnapshot) string {
	switch {
	case s.Skipped:
		return "disabled"
	case s.ConsecutiveFailures > 0:
		return "warning"
	default:
		return "ok"
	}
}

// timePtr converts a zero-able time into an omit-able pointer.
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// rankSourceHealth orders the merged rows: disabled first, then warning,
// then ok/unknown; ties broken alphabetically for stable display.
func rankSourceHealth(rows []models.SourceHealthRow) {
	weight := map[string]int{"disabled": 0, "warning": 1, "ok": 2, "unknown": 3}
	sort.SliceStable(rows, func(i, j int) bool {
		wi, wj := weight[rows[i].Status], weight[rows[j].Status]
		if wi != wj {
			return wi < wj
		}
		return rows[i].Domain+rows[i].Source < rows[j].Domain+rows[j].Source
	})
}

// GetSourceHealth handles GET /api/sources/health — the source health panel
// data source. Merges live in-memory fetch health (consecutive failures,
// auto-disable state) with recent DB article activity per domain. Degrades
// gracefully: with no scheduler/manager or no store the other half still
// renders; a DB error is logged, not fatal.
func (h *Handler) GetSourceHealth(c echo.Context) error {
	now := time.Now()
	window := sourceHealthWindowDays * 24 * time.Hour

	// Live half — in-memory manager state.
	snapshots := []fetcher.HealthSnapshot{}
	if h.sourceHealth != nil {
		snapshots = h.sourceHealth.SourceHealth()
	}

	// DB half — per-domain article activity in the window.
	activity := []models.SourceActivity{}
	if h.store != nil {
		a, err := h.store.SourceActivity(c.Request().Context(), now.Add(-window))
		if err != nil {
			// The panel is still useful from live data alone.
			h.logger.Warn("source activity query failed; serving live health only",
				slog.String("error", err.Error()))
		} else {
			activity = a
		}
	}

	activityByDomain := make(map[string]models.SourceActivity, len(activity))
	for _, a := range activity {
		activityByDomain[normalizeHost(a.Domain)] = a
	}

	rows := make([]models.SourceHealthRow, 0, len(snapshots)+len(activity))
	seenDomain := make(map[string]struct{}, len(snapshots))

	for _, s := range snapshots {
		domain := hostOf(s.Source)
		row := models.SourceHealthRow{
			Source:              s.Source,
			Domain:              domain,
			Status:              healthStatus(s),
			ConsecutiveFailures: s.ConsecutiveFailures,
			TotalAttempts:       s.TotalAttempts,
			TotalFailures:       s.TotalFailures,
			LastOutcome:         s.LastOutcome,
			LastError:           s.LastError,
			LastAttemptAt:       timePtr(s.LastAttemptAt),
			LastSuccessAt:       timePtr(s.LastSuccessAt),
		}
		if domain != "" {
			seenDomain[domain] = struct{}{}
			if a, ok := activityByDomain[domain]; ok {
				row.RecentArticles = a.Articles
				if t := timePtr(a.LastFetchedAt); t != nil {
					row.LastArticleAt = t
				}
			}
		}
		rows = append(rows, row)
	}

	// Domains with DB activity but no live state (fresh restart, or fetched
	// before health tracking existed): show what the DB knows.
	for _, a := range activity {
		d := normalizeHost(a.Domain)
		if _, ok := seenDomain[d]; ok {
			continue
		}
		row := models.SourceHealthRow{
			Source:         d,
			Domain:         d,
			Status:         "unknown",
			RecentArticles: a.Articles,
		}
		if t := timePtr(a.LastFetchedAt); t != nil {
			row.LastArticleAt = t
		}
		rows = append(rows, row)
	}

	rankSourceHealth(rows)
	return c.JSON(http.StatusOK, models.SourceHealthResponse{
		Sources:    rows,
		WindowDays: sourceHealthWindowDays,
	})
}

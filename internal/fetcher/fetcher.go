// Package fetcher provides data-source adapters for RSS, NewsAPI, and RSSHub.
package fetcher

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/user/daily-info-agent/pkg/models"
)

const defaultFetchTimeout = 10 * time.Second

// Fetcher is the common interface for all data-source adapters.
type Fetcher interface {
	// Fetch retrieves items from the source. Returns a typed FetchError on failure;
	// never panics. An empty slice with nil error is valid (source returned no items).
	Fetch(ctx context.Context, cfg models.FetchConfig) ([]models.RawItem, error)
	// Name returns a human-readable identifier used in logs.
	Name() string
}

// FetchError is the typed error returned by adapters on source failure.
type FetchError struct {
	Source  string
	URL     string
	Wrapped error
}

func (e *FetchError) Error() string {
	return fmt.Sprintf("fetcher %q failed on %q: %v", e.Source, e.URL, e.Wrapped)
}

func (e *FetchError) Unwrap() error { return e.Wrapped }

// newHTTPClient returns an *http.Client with the given timeout.
// If timeout is zero, defaultFetchTimeout is used.
func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout == 0 {
		timeout = defaultFetchTimeout
	}
	return &http.Client{Timeout: timeout}
}

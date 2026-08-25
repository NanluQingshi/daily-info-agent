package main

import (
	"github.com/user/daily-info-agent/pkg/metrics"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// isPlaceholderKey
// ---------------------------------------------------------------------------

func TestIsPlaceholderKey_Empty(t *testing.T) {
	assert.True(t, isPlaceholderKey(""))
}

func TestIsPlaceholderKey_HTTPURL(t *testing.T) {
	assert.True(t, isPlaceholderKey("http://example.com/key"))
	assert.True(t, isPlaceholderKey("https://example.com/key"))
}

func TestIsPlaceholderKey_CommonValues(t *testing.T) {
	assert.True(t, isPlaceholderKey("placeholder"))
	assert.True(t, isPlaceholderKey("test-key"))
	assert.True(t, isPlaceholderKey("test"))
	// Case-insensitive
	assert.True(t, isPlaceholderKey("PLACEHOLDER"))
	assert.True(t, isPlaceholderKey("Test-Key"))
}

func TestIsPlaceholderKey_RealKey(t *testing.T) {
	assert.False(t, isPlaceholderKey("sk-abc123def456"))
	assert.False(t, isPlaceholderKey("a1b2c3d4e5"))
}

// ---------------------------------------------------------------------------
// maskDSN
// ---------------------------------------------------------------------------

func TestMaskDSN_WithPassword(t *testing.T) {
	dsn := "postgres://user:secretpass@localhost:5432/db?sslmode=disable"
	masked := maskDSN(dsn)
	assert.Equal(t, "postgres://user:***@localhost:5432/db?sslmode=disable", masked)
	assert.NotContains(t, masked, "secretpass")
}

func TestMaskDSN_NoPassword(t *testing.T) {
	dsn := "postgres://localhost:5432/db"
	assert.Equal(t, dsn, maskDSN(dsn))
}

func TestMaskDSN_Empty(t *testing.T) {
	assert.Equal(t, "", maskDSN(""))
}

func TestMaskDSN_NoAtSign(t *testing.T) {
	dsn := "postgres:password@localhost"
	// Password section before "@" is masked.
	assert.Equal(t, "postgres:***@localhost", maskDSN(dsn))
}

// ---------------------------------------------------------------------------
// metricsHandler
// ---------------------------------------------------------------------------

func TestMetricsHandler_FeedbackCounters(t *testing.T) {
	metrics.App.FeedbackUp.Store(0)
	metrics.App.FeedbackDown.Store(0)
	metrics.App.FeedbackUp.Add(3)
	metrics.App.FeedbackDown.Add(2)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	metricsHandler(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "dia_feedback_up 3")
	assert.Contains(t, body, "dia_feedback_down 2")
}

func TestMetricsHandler_ReturnsRuntimeMetrics(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	metricsHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")

	body := rec.Body.String()
	assert.Contains(t, body, "go_goroutines")
	assert.Contains(t, body, "go_mem_alloc_bytes")
	assert.Contains(t, body, "go_mem_sys_bytes")
	assert.Contains(t, body, "go_gc_total")
	assert.Contains(t, body, "go_cgo_calls")
}

func TestMetricsHandler_ContainsValidGaugeValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	metricsHandler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// go_goroutines must be a non-negative integer
	body := rec.Body.String()
	assert.Regexp(t, `go_goroutines \d+`, body)
}

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/user/daily-info-agent/pkg/config"
)

// serveAPI mounts the full /api group on a fresh echo and performs a
// request, exercising group middleware (auth, rate limit) like production.
func serveAPI(t *testing.T, cfg *config.Config, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	h := newHandlerWithCfg(cfg)
	e := newEcho()
	h.Register(e.Group("/api"))

	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// authedCfg returns a config requiring the given management token.
func authedCfg(token string) *config.Config {
	return &config.Config{APIToken: token}
}

func TestTokenAuth_DisabledByDefault(t *testing.T) {
	// No API_TOKEN → open API, identical to pre-auth behaviour.
	rec := serveAPI(t, &config.Config{}, http.MethodGet, "/api/articles", nil)
	assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
}

func TestTokenAuth_RejectsMissingOrWrongToken(t *testing.T) {
	cfg := authedCfg("s3cret")

	rec := serveAPI(t, cfg, http.MethodGet, "/api/articles", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "unauthorized")

	rec = serveAPI(t, cfg, http.MethodGet, "/api/articles",
		map[string]string{"Authorization": "Bearer wrong"})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Scheme missing entirely
	rec = serveAPI(t, cfg, http.MethodGet, "/api/articles",
		map[string]string{"Authorization": "s3cret"})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTokenAuth_AcceptsCorrectToken(t *testing.T) {
	cfg := authedCfg("s3cret")
	rec := serveAPI(t, cfg, http.MethodGet, "/api/articles",
		map[string]string{"Authorization": "Bearer s3cret"})
	assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
}

func TestTokenAuth_CoversAllManagementRoutes(t *testing.T) {
	cfg := authedCfg("s3cret")
	for _, target := range []string{
		"/api/articles",
		"/api/articles/1",
		"/api/stats",
		"/api/fetch",
	} {
		rec := serveAPI(t, cfg, http.MethodGet, target, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, target)
	}
}

func TestTokenAuth_WriteOpsProtected(t *testing.T) {
	cfg := authedCfg("s3cret")
	for _, target := range []string{
		"/api/articles/1/publish",
		"/api/articles/1/retry",
		"/api/articles/1",
		"/api/articles/tags",
	} {
		method := http.MethodDelete
		if target != "/api/articles/1" {
			method = http.MethodPost
		}
		if target == "/api/articles/tags" {
			method = http.MethodPatch
		}
		rec := serveAPI(t, cfg, method, target, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, target)
	}
}

func TestTokenAuth_DirectHandlerStillWorks(t *testing.T) {
	// Handler methods invoked without the group (internal wiring/tests)
	// are unaffected by the middleware itself.
	h := newHandlerWithCfg(authedCfg("s3cret"))
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	assert.Error(t, h.ListArticles(c)) // requireStore error — not an auth error
}

func TestTokenAuth_RunsBeforeRateLimit(t *testing.T) {
	// A rejected request must not consume per-IP quota: fill the limiter
	// with unauthenticated calls, then verify a valid token still passes.
	cfg := &config.Config{APIToken: "s3cret", APIRateLimitPerMin: 1}
	for i := 0; i < 3; i++ {
		rec := serveAPI(t, cfg, http.MethodGet, "/api/articles", nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	}
	rec := serveAPI(t, cfg, http.MethodGet, "/api/articles",
		map[string]string{"Authorization": "Bearer s3cret"})
	assert.NotEqual(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
}

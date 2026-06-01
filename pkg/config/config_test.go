package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/models"
)

// setRequiredEnvVars sets all five required env vars and returns a cleanup
// function that is not needed — callers use t.Setenv which auto-restores.
func setRequiredEnvVars(t *testing.T) {
	t.Helper()
	t.Setenv("DEEPSEEK_API_KEY", "sk-test-key")
	t.Setenv("DEEPSEEK_MODEL_ID", "deepseek-chat")
	t.Setenv("NEWSAPI_KEY", "newsapi-test-key")
	t.Setenv("WEBSITE_API_BASE_URL", "http://localhost:8081")
	t.Setenv("WEBSITE_API_TOKEN", "website-token")
}

func clearRequiredEnvVars(t *testing.T) {
	t.Helper()
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("DEEPSEEK_MODEL_ID", "")
	t.Setenv("NEWSAPI_KEY", "")
	t.Setenv("WEBSITE_API_BASE_URL", "")
	t.Setenv("WEBSITE_API_TOKEN", "")
}

// ---------------------------------------------------------------------------
// Loading from env vars
// ---------------------------------------------------------------------------

func TestLoad_AllRequiredEnvVars_Succeeds(t *testing.T) {
	setRequiredEnvVars(t)

	cfg, err := config.Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "sk-test-key", cfg.DeepSeekAPIKey)
	assert.Equal(t, "deepseek-chat", cfg.DeepSeekModelID)
	assert.Equal(t, "newsapi-test-key", cfg.NewsAPIKey)
	assert.Equal(t, "http://localhost:8081", cfg.WebsiteAPIBaseURL)
	assert.Equal(t, "website-token", cfg.WebsiteAPIToken)
}

// ---------------------------------------------------------------------------
// Missing required fields
// ---------------------------------------------------------------------------

func TestLoad_MissingDeepSeekAPIKey_ReturnsError(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("DEEPSEEK_API_KEY", "")

	_, err := config.Load()
	require.Error(t, err)

	var missingErr *config.MissingConfigError
	require.ErrorAs(t, err, &missingErr)
	assert.Contains(t, missingErr.Vars, "DEEPSEEK_API_KEY")
}

func TestLoad_MissingDeepSeekModelID_ReturnsError(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("DEEPSEEK_MODEL_ID", "")

	_, err := config.Load()
	require.Error(t, err)

	var missingErr *config.MissingConfigError
	require.ErrorAs(t, err, &missingErr)
	assert.Contains(t, missingErr.Vars, "DEEPSEEK_MODEL_ID")
}

func TestLoad_MissingNewsAPIKey_ReturnsError(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("NEWSAPI_KEY", "")

	_, err := config.Load()
	require.Error(t, err)

	var missingErr *config.MissingConfigError
	require.ErrorAs(t, err, &missingErr)
	assert.Contains(t, missingErr.Vars, "NEWSAPI_KEY")
}

func TestLoad_MissingWebsiteBaseURL_DisablesPublisher(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("WEBSITE_API_BASE_URL", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.DisableJavaPublisher)
}

func TestLoad_MissingWebsiteToken_DisablesPublisher(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("WEBSITE_API_TOKEN", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.DisableJavaPublisher)
}

func TestLoad_MultipleFieldsMissing_AllListedInError(t *testing.T) {
	clearRequiredEnvVars(t)

	_, err := config.Load()
	require.Error(t, err)

	var missingErr *config.MissingConfigError
	require.ErrorAs(t, err, &missingErr)
	assert.Len(t, missingErr.Vars, 3)
	assert.Contains(t, missingErr.Vars, "DEEPSEEK_API_KEY")
	assert.Contains(t, missingErr.Vars, "DEEPSEEK_MODEL_ID")
	assert.Contains(t, missingErr.Vars, "NEWSAPI_KEY")
}

func TestLoad_MissingConfigError_ErrorMessage(t *testing.T) {
	clearRequiredEnvVars(t)

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required environment variables")
}

// ---------------------------------------------------------------------------
// Default values
// ---------------------------------------------------------------------------

func TestLoad_DefaultDeepSeekBaseURL(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("DEEPSEEK_BASE_URL", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "https://api.deepseek.com/v1", cfg.DeepSeekBaseURL)
}

func TestLoad_DefaultRSSHubBaseURL(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("RSSHUB_BASE_URL", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "https://rsshub.app", cfg.RSSHubBaseURL)
}

func TestLoad_DefaultBindAddr(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("BIND_ADDR", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:8080", cfg.BindAddr)
}

func TestLoad_DefaultCacheFilePath(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("CACHE_FILE_PATH", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "cache/dedup.json", cfg.CacheFilePath)
}

func TestLoad_DefaultAgentVersion(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("AGENT_VERSION", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", cfg.AgentVersion)
}

func TestLoad_DefaultRSSFeeds_FallbackBuiltin(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("RSS_FEEDS", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.RSSFeeds)
	// Check that at least one well-known default feed is present.
	var found bool
	for _, f := range cfg.RSSFeeds {
		if f == "https://feeds.reuters.com/reuters/topNews" {
			found = true
		}
	}
	assert.True(t, found, "default RSS feeds should include Reuters top-news feed")
}

func TestLoad_DefaultTrustedDomains_FallbackBuiltin(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("TRUSTED_DOMAINS", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.TrustedDomains)
	assert.Contains(t, cfg.TrustedDomains, "reuters.com")
	assert.Contains(t, cfg.TrustedDomains, "bbc.com")
}

func TestLoad_DefaultCategories_AllFive(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("DEFAULT_CATEGORIES", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, models.AllCategories, cfg.DefaultCategories)
}

// ---------------------------------------------------------------------------
// Custom / overridden values
// ---------------------------------------------------------------------------

func TestLoad_CustomDeepSeekBaseURL(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("DEEPSEEK_BASE_URL", "http://my-proxy.example.com/v1")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "http://my-proxy.example.com/v1", cfg.DeepSeekBaseURL)
}

func TestLoad_CustomRSSFeeds_SemicolonSeparated(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("RSS_FEEDS", "http://feed1.example.com/rss;http://feed2.example.com/rss")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.RSSFeeds, 2)
	assert.Equal(t, "http://feed1.example.com/rss", cfg.RSSFeeds[0])
	assert.Equal(t, "http://feed2.example.com/rss", cfg.RSSFeeds[1])
}

func TestLoad_RSSFeeds_WhitespaceAroundEntries_Trimmed(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("RSS_FEEDS", "  http://feed1.example.com/rss  ;  http://feed2.example.com/rss  ")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.RSSFeeds, 2)
	assert.Equal(t, "http://feed1.example.com/rss", cfg.RSSFeeds[0])
}

func TestLoad_CustomTrustedDomains_CommaSeparated(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("TRUSTED_DOMAINS", "example.com,trusted.org, another.net ")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.TrustedDomains, 3)
	assert.Contains(t, cfg.TrustedDomains, "example.com")
	assert.Contains(t, cfg.TrustedDomains, "trusted.org")
	assert.Contains(t, cfg.TrustedDomains, "another.net")
}

func TestLoad_CustomDefaultCategories(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("DEFAULT_CATEGORIES", "金融,科技/AI")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.DefaultCategories, 2)
	assert.Equal(t, models.CategoryFinance, cfg.DefaultCategories[0])
	assert.Equal(t, models.CategoryTechAI, cfg.DefaultCategories[1])
}

func TestLoad_SkipVerification_TrueString(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("SKIP_VERIFICATION", "true")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.SkipVerification)
}

func TestLoad_SkipVerification_FalseByDefault(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("SKIP_VERIFICATION", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.False(t, cfg.SkipVerification)
}

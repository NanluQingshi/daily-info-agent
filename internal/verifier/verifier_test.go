package verifier_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/verifier"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var defaultTrustedDomains = []string{
	"xinhua.net",
	"people.com.cn",
	"gov.cn",
	"reuters.com",
	"bbc.com",
	"theverge.com",
}

func newVerifier(domains []string, skip bool) *verifier.Verifier {
	return verifier.New(domains, skip, slog.Default())
}

func makeArticle(domain string, score float64) models.ProcessedArticle {
	return models.ProcessedArticle{
		Raw: &models.RawItem{
			URL:          "http://" + domain + "/article",
			SourceDomain: domain,
		},
		CredibilityScore: score,
	}
}

// ---------------------------------------------------------------------------
// IsTrustedDomain — table-driven
// ---------------------------------------------------------------------------

func TestVerifier_IsTrustedDomain_TableDriven(t *testing.T) {
	v := newVerifier(defaultTrustedDomains, false)

	tests := []struct {
		domain    string
		wantTrust bool
	}{
		{"xinhua.net", true},
		{"people.com.cn", true},
		{"gov.cn", true},
		{"reuters.com", true},
		{"bbc.com", true},
		{"theverge.com", true},
		// www. prefix should be stripped automatically.
		{"www.reuters.com", true},
		{"www.bbc.com", true},
		// Unknown domains.
		{"unknown-blog.io", false},
		{"fake-news.ru", false},
		{"", false},
		// Subdomain of trusted domain is NOT trusted (only exact match).
		{"subdomain.reuters.com", false},
	}

	for _, tc := range tests {
		t.Run(tc.domain, func(t *testing.T) {
			got := v.IsTrustedDomain(tc.domain)
			assert.Equal(t, tc.wantTrust, got,
				"IsTrustedDomain(%q) should be %v", tc.domain, tc.wantTrust)
		})
	}
}

// ---------------------------------------------------------------------------
// Verify — whitelist priority
// ---------------------------------------------------------------------------

func TestVerifier_Verify_WhitelistedDomain_PassesRegardlessOfScore(t *testing.T) {
	v := newVerifier(defaultTrustedDomains, false)

	// Even with score 0.0, whitelisted domain must pass.
	article := makeArticle("reuters.com", 0.0)

	results := v.Verify([]models.ProcessedArticle{article})
	require.Len(t, results, 1)

	assert.True(t, results[0].Verification.Pass)
	assert.True(t, results[0].Verification.DomainHit)
	assert.Empty(t, results[0].Verification.SkipReason)
}

func TestVerifier_Verify_WhitelistedDomain_LowScore_StillPasses(t *testing.T) {
	v := newVerifier(defaultTrustedDomains, false)
	article := makeArticle("xinhua.net", 0.1)

	results := v.Verify([]models.ProcessedArticle{article})
	require.Len(t, results, 1)
	assert.True(t, results[0].Verification.Pass)
	assert.True(t, results[0].Verification.DomainHit)
}

// ---------------------------------------------------------------------------
// Verify — AI score threshold
// ---------------------------------------------------------------------------

func TestVerifier_Verify_ScoreAtThreshold_Passes(t *testing.T) {
	v := newVerifier(defaultTrustedDomains, false)
	// Threshold is 0.7; exactly at threshold should pass.
	article := makeArticle("unknown-outlet.com", 0.7)

	results := v.Verify([]models.ProcessedArticle{article})
	require.Len(t, results, 1)
	assert.True(t, results[0].Verification.Pass)
	assert.False(t, results[0].Verification.DomainHit)
}

func TestVerifier_Verify_ScoreAboveThreshold_LikelyTrusted_Passes(t *testing.T) {
	v := newVerifier(defaultTrustedDomains, false)
	article := makeArticle("respectable-journal.com", 0.8)

	results := v.Verify([]models.ProcessedArticle{article})
	require.Len(t, results, 1)
	assert.True(t, results[0].Verification.Pass)
	assert.False(t, results[0].Verification.DomainHit)
}

func TestVerifier_Verify_ScoreJustBelowThreshold_Fails(t *testing.T) {
	v := newVerifier(defaultTrustedDomains, false)
	// 0.69 is just below the 0.7 threshold.
	article := makeArticle("marginal-blog.com", 0.69)

	results := v.Verify([]models.ProcessedArticle{article})
	require.Len(t, results, 1)
	assert.False(t, results[0].Verification.Pass)
	assert.Equal(t, models.SkipReasonNotWhitelisted, results[0].Verification.SkipReason)
	assert.False(t, results[0].Verification.DomainHit)
}

func TestVerifier_Verify_LowScore_Skipped(t *testing.T) {
	v := newVerifier(defaultTrustedDomains, false)
	article := makeArticle("suspect-source.net", 0.3)

	results := v.Verify([]models.ProcessedArticle{article})
	require.Len(t, results, 1)
	assert.False(t, results[0].Verification.Pass)
	assert.Equal(t, models.SkipReasonNotWhitelisted, results[0].Verification.SkipReason)
}

func TestVerifier_Verify_ZeroScore_Skipped(t *testing.T) {
	v := newVerifier(defaultTrustedDomains, false)
	article := makeArticle("spam-farm.com", 0.0)

	results := v.Verify([]models.ProcessedArticle{article})
	require.Len(t, results, 1)
	assert.False(t, results[0].Verification.Pass)
}

// ---------------------------------------------------------------------------
// Verify — skip verification mode
// ---------------------------------------------------------------------------

func TestVerifier_Verify_SkipVerification_AllPass(t *testing.T) {
	v := newVerifier([]string{}, true) // empty whitelist but skip=true
	articles := []models.ProcessedArticle{
		makeArticle("completely-unknown.io", 0.0),
		makeArticle("another-unknown.ru", 0.1),
	}

	results := v.Verify(articles)
	require.Len(t, results, 2)
	assert.True(t, results[0].Verification.Pass)
	assert.True(t, results[1].Verification.Pass)
	// DomainHit is false even in skip mode.
	assert.False(t, results[0].Verification.DomainHit)
}

// ---------------------------------------------------------------------------
// Verify — batch / mixed results
// ---------------------------------------------------------------------------

func TestVerifier_Verify_MixedBatch_CorrectlyClassified(t *testing.T) {
	v := newVerifier(defaultTrustedDomains, false)

	articles := []models.ProcessedArticle{
		makeArticle("reuters.com", 0.0),       // whitelist → pass
		makeArticle("unknown-blog.io", 0.8),   // high score → pass
		makeArticle("suspect-site.net", 0.3),  // low score → skip
	}

	results := v.Verify(articles)
	require.Len(t, results, 3)

	assert.True(t, results[0].Verification.Pass, "reuters.com should pass (whitelist)")
	assert.True(t, results[1].Verification.Pass, "score 0.8 should pass")
	assert.False(t, results[2].Verification.Pass, "score 0.3 should be skipped")
}

func TestVerifier_Verify_EmptySlice_ReturnsEmptySlice(t *testing.T) {
	v := newVerifier(defaultTrustedDomains, false)

	results := v.Verify([]models.ProcessedArticle{})
	assert.Empty(t, results)
}

// ---------------------------------------------------------------------------
// Domain normalisation
// ---------------------------------------------------------------------------

func TestVerifier_IsTrustedDomain_NormaliseUppercase(t *testing.T) {
	v := newVerifier([]string{"REUTERS.COM"}, false)
	// Both uppercase domain in whitelist and mixed-case lookup should work.
	assert.True(t, v.IsTrustedDomain("reuters.com"))
	assert.True(t, v.IsTrustedDomain("REUTERS.COM"))
	assert.True(t, v.IsTrustedDomain("Reuters.Com"))
}

func TestVerifier_IsTrustedDomain_WhitespaceInDomain_Trimmed(t *testing.T) {
	v := newVerifier([]string{"  reuters.com  "}, false)
	assert.True(t, v.IsTrustedDomain("reuters.com"))
}

// ---------------------------------------------------------------------------
// New — constructor smoke tests
// ---------------------------------------------------------------------------

func TestVerifier_New_EmptyDomains_NoDomainEverTrusted(t *testing.T) {
	v := newVerifier([]string{}, false)
	assert.False(t, v.IsTrustedDomain("reuters.com"))
	assert.False(t, v.IsTrustedDomain("bbc.com"))
}

func TestVerifier_New_NilDomains_DoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		v := verifier.New(nil, false, slog.Default())
		_ = v.IsTrustedDomain("reuters.com")
	})
}

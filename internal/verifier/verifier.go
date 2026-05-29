// Package verifier applies source-credibility policy to processed articles.
package verifier

import (
	"log/slog"
	"strings"

	"github.com/user/daily-info-agent/pkg/models"
)

const credibilityThreshold = 0.7

// Verifier applies source credibility rules to processed articles.
type Verifier struct {
	trustedDomains   map[string]struct{}
	skipVerification bool
	logger           *slog.Logger
}

// New creates a Verifier with the given whitelist domains and skip flag.
// Domains are normalised to lowercase with any leading "www." stripped.
func New(trustedDomains []string, skipVerification bool, logger *slog.Logger) *Verifier {
	dm := make(map[string]struct{}, len(trustedDomains))
	for _, d := range trustedDomains {
		d = strings.ToLower(strings.TrimSpace(d))
		d = strings.TrimPrefix(d, "www.")
		if d != "" {
			dm[d] = struct{}{}
		}
	}
	return &Verifier{
		trustedDomains:   dm,
		skipVerification: skipVerification,
		logger:           logger,
	}
}

// Verify annotates each article with a VerificationResult.
// All articles are returned; callers should check article.Verification.Pass.
func (v *Verifier) Verify(articles []models.ProcessedArticle) []models.ProcessedArticle {
	for i := range articles {
		articles[i].Verification = v.verify(&articles[i])
	}
	return articles
}

// verify returns the VerificationResult for a single article.
func (v *Verifier) verify(article *models.ProcessedArticle) models.VerificationResult {
	if v.skipVerification {
		return models.VerificationResult{Pass: true, DomainHit: false}
	}

	domain := normaliseDomain(article.Raw.SourceDomain)
	inWhitelist := v.IsTrustedDomain(domain)

	if inWhitelist {
		v.logger.Debug("verification pass: whitelisted domain",
			slog.String("domain", domain),
			slog.String("url", article.Raw.URL),
		)
		return models.VerificationResult{Pass: true, DomainHit: true}
	}

	// Not in whitelist — check AI score.
	if article.CredibilityScore >= credibilityThreshold {
		v.logger.Debug("verification pass: ai score threshold met",
			slog.String("domain", domain),
			slog.Float64("credibility_score", article.CredibilityScore),
			slog.String("url", article.Raw.URL),
		)
		return models.VerificationResult{Pass: true, DomainHit: false}
	}

	// Skip.
	reason := models.SkipReasonNotWhitelisted
	v.logger.Warn("verification skip",
		slog.String("domain", domain),
		slog.Float64("credibility_score", article.CredibilityScore),
		slog.String("skip_reason", string(reason)),
		slog.String("url", article.Raw.URL),
	)
	return models.VerificationResult{
		Pass:       false,
		SkipReason: reason,
		DomainHit:  false,
	}
}

// IsTrustedDomain reports whether the given registered domain is in the whitelist.
// The input is normalised before comparison.
func (v *Verifier) IsTrustedDomain(domain string) bool {
	d := normaliseDomain(domain)
	_, ok := v.trustedDomains[d]
	return ok
}

// normaliseDomain lowercases and strips leading "www." from a domain string.
func normaliseDomain(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	return strings.TrimPrefix(d, "www.")
}

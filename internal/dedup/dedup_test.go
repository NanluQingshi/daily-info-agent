package dedup

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/daily-info-agent/pkg/models"
)

func item(title, domain, desc string) models.RawItem {
	return models.RawItem{Title: title, SourceDomain: domain, Description: desc}
}

func TestByTitle_RemovesDuplicates(t *testing.T) {
	// Three articles about the same Fed rate decision from different sources.
	// All share enough title tokens (Jaccard ≥ 0.5) to be clustered together.
	items := []models.RawItem{
		item("Federal Reserve raises interest rates 25 basis points Wednesday decision", "bloomberg.com", "short"),
		item("Federal Reserve raises interest rates by 25 basis points meeting", "reuters.com", "longer description here"),
		item("Fed raises interest rates 25 basis points Federal Reserve decision meeting", "bbc.com", ""),
	}
	trusted := []string{"reuters.com", "bbc.com"}

	got, removed := ByTitle(items, trusted)

	assert.Equal(t, 2, removed)
	assert.Len(t, got, 1)
	// reuters.com is trusted and has more content than bbc.com
	assert.Equal(t, "reuters.com", got[0].SourceDomain)
}

func TestByTitle_KeepsDistinctArticles(t *testing.T) {
	items := []models.RawItem{
		item("Federal Reserve raises interest rates by 25 basis points", "reuters.com", ""),
		item("Tesla unveils new electric vehicle model affordable price", "theverge.com", ""),
		item("China GDP growth slows fourth quarter economic data", "ft.com", ""),
	}

	got, removed := ByTitle(items, nil)

	assert.Equal(t, 0, removed)
	assert.Len(t, got, 3)
}

func TestByTitle_PrefersMoreContentWhenTrustEqual(t *testing.T) {
	items := []models.RawItem{
		item("US inflation data consumer price index rises sharply", "site-a.com", "short"),
		item("US inflation data consumer price index rises sharply today", "site-b.com", "much longer description with more context"),
	}

	got, removed := ByTitle(items, nil)

	assert.Equal(t, 1, removed)
	assert.Len(t, got, 1)
	assert.Equal(t, "site-b.com", got[0].SourceDomain)
}

func TestByTitle_ShortTitlesNotMerged(t *testing.T) {
	// Titles with fewer than minTokens (3) words are never merged even if identical
	items := []models.RawItem{
		item("AI boom", "site-a.com", ""),
		item("AI boom", "site-b.com", ""),
	}

	got, removed := ByTitle(items, nil)

	assert.Equal(t, 0, removed)
	assert.Len(t, got, 2)
}

func TestByTitle_SameDomainNotMerged(t *testing.T) {
	// Two articles from the same source with similar titles must NOT be merged.
	items := []models.RawItem{
		item("Federal Reserve raises interest rates 25 basis points decision", "reuters.com", ""),
		item("Federal Reserve raises interest rates 25 basis points meeting", "reuters.com", ""),
	}

	got, removed := ByTitle(items, []string{"reuters.com"})

	assert.Equal(t, 0, removed)
	assert.Len(t, got, 2)
}

func TestByTitle_SingleItem(t *testing.T) {
	items := []models.RawItem{item("Some headline about something important", "example.com", "")}
	got, removed := ByTitle(items, nil)
	assert.Equal(t, 0, removed)
	assert.Equal(t, items, got)
}

func TestByTitle_EmptyInput(t *testing.T) {
	got, removed := ByTitle(nil, nil)
	assert.Equal(t, 0, removed)
	assert.Empty(t, got)
}

func TestJaccard_IdenticalSets(t *testing.T) {
	a := map[string]struct{}{"fed": {}, "raises": {}, "rates": {}}
	assert.InDelta(t, 1.0, jaccard(a, a), 0.001)
}

func TestJaccard_DisjointSets(t *testing.T) {
	a := map[string]struct{}{"fed": {}, "raises": {}}
	b := map[string]struct{}{"tesla": {}, "model": {}}
	assert.InDelta(t, 0.0, jaccard(a, b), 0.001)
}

func TestJaccard_PartialOverlap(t *testing.T) {
	a := map[string]struct{}{"fed": {}, "raises": {}, "rates": {}, "quarter": {}}
	b := map[string]struct{}{"fed": {}, "raises": {}, "rates": {}, "point": {}}
	// intersection=3, union=5 → 0.6
	assert.InDelta(t, 0.6, jaccard(a, b), 0.001)
}

func TestTokenize_FiltersStopWords(t *testing.T) {
	tokens := tokenize("The Federal Reserve is raising interest rates")
	_, hasThe := tokens["the"]
	_, hasIs := tokens["is"]
	_, hasFederal := tokens["federal"]
	assert.False(t, hasThe)
	assert.False(t, hasIs)
	assert.True(t, hasFederal)
}

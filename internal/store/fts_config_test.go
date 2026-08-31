package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The list/count FTS predicates must use the zhparser-based `zh` config
// (migration 006). 'simple' treats Chinese as one long token and Chinese
// queries never match — this guards against an accidental revert (#55).
func TestFTSQueriesUseChineseConfig(t *testing.T) {
	assert.Contains(t, sqlListArticles, `search_tsv @@ plainto_tsquery('zh', $5)`)
	assert.Contains(t, sqlListArticles, `ts_rank(search_tsv, plainto_tsquery('zh', $5))`)
	assert.Contains(t, sqlCountArticles, `search_tsv @@ plainto_tsquery('zh', $5)`)
	assert.NotContains(t, sqlListArticles, "'simple'")
	assert.NotContains(t, sqlCountArticles, "'simple'")
	assert.True(t, strings.Contains(sqlListArticles, "plainto_tsquery") &&
		strings.Contains(sqlCountArticles, "plainto_tsquery"))
}

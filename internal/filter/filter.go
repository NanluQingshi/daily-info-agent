// Package filter implements keyword-based subscription filtering for fetched
// items. It runs after fetch/dedup and before AI processing so noise never
// reaches the LLM (and the token bill).
//
// Semantics:
//   - no keywords configured → pass-through (behaviour identical to before)
//   - whitelist configured → keep only items whose title/description match at
//     least one whitelist keyword
//   - blacklist configured → drop items matching at least one blacklist keyword
//   - both configured → whitelist applies first, blacklist prunes the survivors
//
// Matching is case-insensitive substring on title + description. CJK text
// needs no segmentation for substring matching, so Chinese keywords work
// naturally ("芯片" matches "国产芯片量产").
package filter

import (
	"strings"

	"github.com/user/daily-info-agent/pkg/models"
)

// KeywordFilter filters RawItems by whitelist/blacklist keywords.
// A zero-value filter (or one built from empty lists) is a pass-through.
type KeywordFilter struct {
	whitelist []string // lowercased keywords
	blacklist []string // lowercased keywords
}

// New builds a filter from raw keyword lists. Keywords are trimmed and
// lowercased; empty entries are dropped.
func New(whitelist, blacklist []string) *KeywordFilter {
	return &KeywordFilter{
		whitelist: normalize(whitelist),
		blacklist: normalize(blacklist),
	}
}

// Enabled reports whether any keyword is configured.
func (f *KeywordFilter) Enabled() bool {
	return len(f.whitelist) > 0 || len(f.blacklist) > 0
}

// Apply filters items and returns the kept slice plus the number removed.
// The input slice is never mutated; the returned slice aliases it only via
// retained elements.
func (f *KeywordFilter) Apply(items []models.RawItem) ([]models.RawItem, int) {
	if !f.Enabled() {
		return items, 0
	}

	kept := make([]models.RawItem, 0, len(items))
	for _, it := range items {
		if f.keep(it) {
			kept = append(kept, it)
		}
	}
	return kept, len(items) - len(kept)
}

// keep evaluates one item against the whitelist, then the blacklist.
func (f *KeywordFilter) keep(item models.RawItem) bool {
	if len(f.whitelist) > 0 && !matchAny(item, f.whitelist) {
		return false
	}
	if len(f.blacklist) > 0 && matchAny(item, f.blacklist) {
		return false
	}
	return true
}

// matchAny reports whether any keyword is a case-insensitive substring of the
// item's title or description.
func matchAny(item models.RawItem, keywords []string) bool {
	title := strings.ToLower(item.Title)
	desc := strings.ToLower(item.Description)
	for _, kw := range keywords {
		if strings.Contains(title, kw) || strings.Contains(desc, kw) {
			return true
		}
	}
	return false
}

// normalize trims, lowercases, and drops empty entries.
func normalize(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// SplitKeywords parses a comma-separated env value into keywords. Both ASCII
// and full-width (Chinese) commas are accepted; empty segments are dropped.
// Returns nil when nothing usable remains.
func SplitKeywords(raw string) []string {
	raw = strings.NewReplacer("，", ",", "、", ",").Replace(raw)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

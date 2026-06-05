// Package dedup removes near-duplicate RawItems produced by different news sources
// reporting on the same story. Similarity is measured with Jaccard distance on
// normalised title word tokens; Union-Find clusters items above the threshold,
// then the "best" representative is kept from each cluster.
package dedup

import (
	"strings"
	"unicode"

	"github.com/user/daily-info-agent/pkg/models"
)

const (
	similarityThreshold = 0.5 // Jaccard ≥ 0.5 → same story
	minTokens           = 3   // skip dedup for very short titles
)

// ByTitle returns a deduplicated slice and the number of items removed.
// Items whose titles are below minTokens are never merged (kept as-is).
// Among duplicates, the item from a trusted domain is preferred; otherwise
// the item with more textual content wins.
func ByTitle(items []models.RawItem, trustedDomains []string) ([]models.RawItem, int) {
	if len(items) <= 1 {
		return items, 0
	}

	trusted := make(map[string]bool, len(trustedDomains))
	for _, d := range trustedDomains {
		trusted[d] = true
	}

	sets := make([]map[string]struct{}, len(items))
	for i, item := range items {
		sets[i] = tokenize(item.Title)
	}

	// Union-Find
	parent := make([]int, len(items))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}

	for i := 0; i < len(items); i++ {
		if len(sets[i]) < minTokens {
			continue
		}
		for j := i + 1; j < len(items); j++ {
			if len(sets[j]) < minTokens {
				continue
			}
			// Never merge articles from the same source domain — a single outlet
			// won't publish two near-identical titles. Cross-source is the target.
			if items[i].SourceDomain == items[j].SourceDomain {
				continue
			}
			if jaccard(sets[i], sets[j]) >= similarityThreshold {
				parent[find(i)] = find(j)
			}
		}
	}

	// Cluster by root
	clusters := make(map[int][]int, len(items))
	for i := range items {
		root := find(i)
		clusters[root] = append(clusters[root], i)
	}

	// Pick best representative from each cluster; preserve original order via
	// the smallest member index as the cluster key.
	result := make([]models.RawItem, 0, len(clusters))
	removed := 0
	for _, members := range clusters {
		best := members[0]
		for _, m := range members[1:] {
			if isBetter(items[m], items[best], trusted) {
				best = m
			}
		}
		result = append(result, items[best])
		removed += len(members) - 1
	}

	return result, removed
}

// isBetter returns true if candidate should replace current as the cluster
// representative. Trusted-domain items beat untrusted; ties go to richer content.
func isBetter(candidate, current models.RawItem, trusted map[string]bool) bool {
	candTrusted := trusted[candidate.SourceDomain]
	currTrusted := trusted[current.SourceDomain]
	if candTrusted && !currTrusted {
		return true
	}
	if !candTrusted && currTrusted {
		return false
	}
	return len(candidate.Description)+len(candidate.Content) >
		len(current.Description)+len(current.Content)
}

// jaccard returns the Jaccard similarity coefficient of two token sets.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	intersection := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// tokenize lowercases s, splits on non-alphanumeric runes, filters stop words
// and single-character tokens, and returns the result as a set.
func tokenize(s string) map[string]struct{} {
	s = strings.ToLower(s)
	tokens := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	set := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		if len(t) >= 2 && !stopWords[t] {
			set[t] = struct{}{}
		}
	}
	return set
}

// stopWords contains common English function words that do not distinguish titles.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true,
	"and": true, "or": true, "but": true, "nor": true,
	"in": true, "on": true, "at": true, "to": true, "for": true,
	"of": true, "with": true, "by": true, "from": true, "as": true,
	"is": true, "are": true, "was": true, "were": true, "be": true,
	"been": true, "has": true, "have": true, "had": true,
	"it": true, "its": true, "this": true, "that": true,
	"will": true, "would": true, "could": true, "should": true,
	"into": true, "over": true, "after": true, "about": true,
	"says": true, "said": true, "up": true, "new": true,
}

// Package metrics holds process-wide application counters exposed via the
// /metrics endpoint. Counters are updated by pipeline stages (scheduler,
// processor, publisher) and read by metricsHandler. Atomic values make them
// safe for concurrent use.
package metrics

import "sync/atomic"

// Counters is the set of application-level counters exposed as Prometheus
// text metrics. Zero values are valid — every counter is an atomic.
type Counters struct {
	// Fetch stage
	ItemsFetched         atomic.Int64 // raw items fetched across all sources
	ItemsDeduped         atomic.Int64 // items removed by title-similarity dedup
	ItemsKeywordFiltered atomic.Int64 // items removed by keyword whitelist/blacklist

	// Full-text extraction stage
	ItemsExtracted atomic.Int64 // pages whose readable full text was extracted
	ExtractFailed  atomic.Int64 // page fetches/extractions that failed (fell back to summary)

	// Process stage
	ItemsProcessed atomic.Int64 // items sent through AI processing
	LLMCalls       atomic.Int64 // successful LLM API calls
	LLMErrors      atomic.Int64 // failed LLM API calls (non-2xx / network)

	// Verify stage
	ItemsPassed  atomic.Int64 // articles passing verification
	ItemsSkipped atomic.Int64 // articles skipped (low score / LLM unavailable)

	// Publish stage
	ItemsPublished atomic.Int64 // articles successfully published
	PublishFailed  atomic.Int64 // articles that failed to publish (permanent)
	PublishRetried atomic.Int64 // articles that succeeded after >= 1 retry

	// Data retention (#74): cumulative rows removed from the database
	RunLogsPruned  atomic.Int64
	ArticlesPruned atomic.Int64

	// Runs
	RunsCompleted atomic.Int64 // pipeline runs that finished (incl. aborted)
	RunsFailed    atomic.Int64 // pipeline runs aborted with fatal error
}

// App is the shared global counter set used across pipeline stages.
var App Counters

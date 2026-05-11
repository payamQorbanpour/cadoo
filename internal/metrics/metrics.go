// Package metrics holds the Prometheus instruments Cadoo exports. The
// /metrics endpoint is served by cadoo-api; cadoo-webhook and cadoo-worker
// also import the package so their counters register on the same default
// registry when scraped from the same process.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// DispatchTotal counts every tool dispatch by tool, provider, and outcome.
// Outcome is one of: success | failure | unknown.
var DispatchTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "cadoo",
	Name:      "dispatch_total",
	Help:      "Total tool dispatches.",
}, []string{"tool", "provider", "outcome"})

// DispatchDuration measures end-to-end dispatch latency, including LLM
// calls and VCS posts.
var DispatchDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "cadoo",
	Name:      "dispatch_duration_seconds",
	Help:      "Tool dispatch latency.",
	Buckets:   prometheus.ExponentialBuckets(0.5, 2, 8),
}, []string{"tool", "provider"})

// LLMCallTotal counts every LLM completion request by model and outcome.
var LLMCallTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "cadoo",
	Name:      "llm_call_total",
	Help:      "Total LLM calls.",
}, []string{"model", "outcome"})

// LLMTokensTotal sums prompt+completion tokens per model — useful for cost
// alerts independent of provider billing webhooks.
var LLMTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "cadoo",
	Name:      "llm_tokens_total",
	Help:      "Total LLM tokens consumed (prompt + completion).",
}, []string{"model"})

package llm

import (
	"fmt"
	"io"
	"os"
	"time"
)

// TotalInputTokens returns all input-side tokens billed by the provider,
// including prompt cache write and read volumes.
func (m Metrics) TotalInputTokens() int {
	return m.InputTokens + m.CacheCreationInputTokens + m.CacheReadInputTokens
}

// HasCache reports whether any prompt-cache tokens were recorded.
func (m Metrics) HasCache() bool {
	return m.CacheCreationInputTokens > 0 || m.CacheReadInputTokens > 0
}

// FormatTokens renders a human-readable token summary for stderr output.
func (m Metrics) FormatTokens() string {
	if !m.HasCache() {
		return fmt.Sprintf("%d in / %d out", m.InputTokens, m.OutputTokens)
	}
	return fmt.Sprintf("%d in / %d out (%d prompt + %d cache write + %d cache read)",
		m.TotalInputTokens(), m.OutputTokens,
		m.InputTokens, m.CacheCreationInputTokens, m.CacheReadInputTokens)
}

// CacheNote explains how cache read tokens relate to this run vs prior runs.
func (m Metrics) CacheNote() string {
	if m.CacheReadInputTokens == 0 {
		return ""
	}
	return "cache read = content billed this run but stored in a prior call (cost is still exact)"
}

// HasMetrics reports whether the driver provided any token or cost data.
// Backends that don't report metrics (e.g. pi print mode) return false.
func (m Metrics) HasMetrics() bool {
	return m.InputTokens > 0 || m.OutputTokens > 0 || m.CostUSD > 0
}

// LogStage prints per-call metrics for one pipeline stage (one LLM subprocess).
func LogStage(stage string, m Metrics) {
	LogStageTo(os.Stderr, stage, m)
}

// LogStageTo prints per-call metrics to w (useful in tests).
// For drivers that don't report token/cost data, prints duration only.
func LogStageTo(w io.Writer, stage string, m Metrics) {
	model := m.Model
	if model == "" {
		model = m.RequestedModel
	}
	if m.HasMetrics() {
		fmt.Fprintf(w, "fabric: [%s] %s (%s), $%.4f\n", stage, m.FormatTokens(), model, m.CostUSD)
	} else {
		dur := time.Duration(m.DurationMs) * time.Millisecond
		fmt.Fprintf(w, "fabric: [%s] complete (%v, %s)\n", stage, dur.Round(time.Millisecond), model)
	}
}

// Merge combines metrics from two LLM calls (e.g. pipeline stages).
func Merge(a, b Metrics) Metrics {
	requested := a.RequestedModel
	if requested == "" {
		requested = b.RequestedModel
	}
	model := b.Model
	if model == "" {
		model = a.Model
	}
	calls := a.Calls + b.Calls
	if calls == 0 {
		if a.DurationMs > 0 {
			calls++
		}
		if b.DurationMs > 0 {
			calls++
		}
	}
	return Metrics{
		DurationMs:               a.DurationMs + b.DurationMs,
		InputTokens:              a.InputTokens + b.InputTokens,
		CacheCreationInputTokens: a.CacheCreationInputTokens + b.CacheCreationInputTokens,
		CacheReadInputTokens:     a.CacheReadInputTokens + b.CacheReadInputTokens,
		OutputTokens:             a.OutputTokens + b.OutputTokens,
		CostUSD:                  a.CostUSD + b.CostUSD,
		Calls:                    calls,
		RequestedModel:           requested,
		Model:                    model,
	}
}

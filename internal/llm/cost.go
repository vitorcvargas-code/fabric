package llm

import (
	"fmt"
	"sort"
	"strings"
)

// Cost is the USD spent on LLM calls plus the models whose spend could not be
// priced. A model without a known rate is carried here by name, never summed
// in as $0: a zero that is really "no rate" reads as a measurement.
type Cost struct {
	USD      float64
	Unpriced []string // sorted, unique model ids
}

// KnownCost is a cost the provider or a rate card actually priced.
func KnownCost(usd float64) Cost { return Cost{USD: usd} }

// UnpricedCost marks a call whose model has no known rate.
func UnpricedCost(model string) Cost {
	if model == "" {
		model = "unknown model"
	}
	return Cost{Unpriced: []string{model}}
}

// IsKnown reports whether every call summed into c was priced.
func (c Cost) IsKnown() bool { return len(c.Unpriced) == 0 }

// Add sums two costs, keeping the union of unpriced models.
func (c Cost) Add(o Cost) Cost {
	return Cost{USD: c.USD + o.USD, Unpriced: unionSorted(c.Unpriced, o.Unpriced)}
}

// String renders the cost for stderr: "$0.1234", or with the unknown part
// spelled out ("$0.1234 + $? (no rate for deepseek-v4-pro)").
func (c Cost) String() string {
	if c.IsKnown() {
		return fmt.Sprintf("$%.4f", c.USD)
	}
	unknown := fmt.Sprintf("$? (no rate for %s)", strings.Join(c.Unpriced, ", "))
	if c.USD == 0 {
		return unknown
	}
	return fmt.Sprintf("$%.4f + %s", c.USD, unknown)
}

func unionSorted(a, b []string) []string {
	if len(a)+len(b) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(a)+len(b))
	for _, m := range append(append([]string{}, a...), b...) {
		seen[m] = true
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// CostEstimator prices one LLM call from its token counts. The bool is false
// when the estimator has no rate for the model: the caller must then fall back
// to another source or report the cost as unknown, never as zero.
type CostEstimator interface {
	EstimateCost(model string, inputTokens, cacheCreationInputTokens, cacheReadInputTokens, outputTokens int) (float64, bool)
}

// RateCard holds per-model pricing rates used by RateCardCostEstimator.
// All rates are USD per 1 million tokens.
type RateCard struct {
	InputPer1M      float64 `json:"input_per_1m" yaml:"input_per_1m"`
	OutputPer1M     float64 `json:"output_per_1m" yaml:"output_per_1m"`
	CacheReadPer1M  float64 `json:"cache_read_per_1m,omitempty" yaml:"cache_read_per_1m,omitempty"`
	CacheWritePer1M float64 `json:"cache_write_per_1m,omitempty" yaml:"cache_write_per_1m,omitempty"`
}

// inputRate returns the per-1M rate for input/prompt tokens. Falls back to InputPer1M.
func (r RateCard) inputRate() float64 { return r.InputPer1M }

// cacheReadRate returns the per-1M rate for cache read tokens.
// Defaults to InputPer1M when not explicitly set.
func (r RateCard) cacheReadRate() float64 {
	if r.CacheReadPer1M > 0 {
		return r.CacheReadPer1M
	}
	return r.InputPer1M
}

// cacheWriteRate returns the per-1M rate for cache write tokens.
// Defaults to InputPer1M when not explicitly set.
func (r RateCard) cacheWriteRate() float64 {
	if r.CacheWritePer1M > 0 {
		return r.CacheWritePer1M
	}
	return r.InputPer1M
}

// RateCardCostEstimator computes cost from token counts and per-model pricing
// (llm.model_pricing in fabric.yaml). It has no default rate on purpose: a
// model missing from the card is reported as unpriced, not billed at $0.
type RateCardCostEstimator struct {
	rates map[string]RateCard // keyed by model id without "provider/" prefix
}

// NewRateCardCostEstimator creates an estimator from per-model rates. Keys may
// carry a "provider/" prefix; lookups match on the bare model id.
func NewRateCardCostEstimator(rates map[string]RateCard) *RateCardCostEstimator {
	normalized := make(map[string]RateCard, len(rates))
	for model, rate := range rates {
		normalized[stripProviderPrefix(model)] = rate
	}
	return &RateCardCostEstimator{rates: normalized}
}

// EstimateCost computes USD cost from token counts using the model's rate card.
func (e *RateCardCostEstimator) EstimateCost(model string, inputTokens, cacheCreationInputTokens, cacheReadInputTokens, outputTokens int) (float64, bool) {
	rate, ok := e.rates[stripProviderPrefix(model)]
	if !ok {
		return 0, false
	}
	return tokenCost(inputTokens, rate.inputRate()) +
		tokenCost(cacheCreationInputTokens, rate.cacheWriteRate()) +
		tokenCost(cacheReadInputTokens, rate.cacheReadRate()) +
		tokenCost(outputTokens, rate.OutputPer1M), true
}

// tokenCost converts tokens to USD given a per-1M rate.
func tokenCost(tokens int, ratePer1M float64) float64 {
	if tokens <= 0 || ratePer1M <= 0 {
		return 0
	}
	return float64(tokens) / 1_000_000.0 * ratePer1M
}

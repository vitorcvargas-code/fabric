package llm

// CostEstimator computes per-call cost. Each driver provides its own
// implementation so cost calculation is driver-specific, not shared.
type CostEstimator interface {
	// EstimateCost returns cost in USD for one LLM call.
	EstimateCost(model string, inputTokens, cacheCreationInputTokens, cacheReadInputTokens, outputTokens int) float64
}

// DirectCostEstimator returns a fixed cost reported by the provider.
// Used by drivers whose upstream already reports accurate cost (e.g. claude-code).
type DirectCostEstimator struct {
	CostUSD float64
}

func (e DirectCostEstimator) EstimateCost(_ string, _, _, _, _ int) float64 {
	return e.CostUSD
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

// RateCardCostEstimator computes cost from token counts and per-model pricing.
// Used by drivers that have precise token counts but no native cost reporting (pi).
type RateCardCostEstimator struct {
	// DefaultRates used when the model is not found in Rates.
	DefaultRates RateCard
	// Rates maps model identifiers (e.g. "claude-opus-4-6-vertexai") to pricing.
	Rates map[string]RateCard
}

// NewRateCardCostEstimator creates an estimator with pricing rates.
func NewRateCardCostEstimator(defaultRates RateCard, rates map[string]RateCard) *RateCardCostEstimator {
	if rates == nil {
		rates = make(map[string]RateCard)
	}
	return &RateCardCostEstimator{DefaultRates: defaultRates, Rates: rates}
}

// EstimateCost computes USD cost from token counts using model-specific rates.
func (e *RateCardCostEstimator) EstimateCost(model string, inputTokens, cacheCreationInputTokens, cacheReadInputTokens, outputTokens int) float64 {
	rate := e.DefaultRates
	if r, ok := e.Rates[model]; ok {
		rate = r
	}
	return tokenCost(inputTokens, rate.inputRate()) +
		tokenCost(cacheCreationInputTokens, rate.cacheWriteRate()) +
		tokenCost(cacheReadInputTokens, rate.cacheReadRate()) +
		tokenCost(outputTokens, rate.OutputPer1M)
}

// tokenCost converts tokens to USD given a per-1M rate.
func tokenCost(tokens int, ratePer1M float64) float64 {
	if tokens <= 0 || ratePer1M <= 0 {
		return 0
	}
	return float64(tokens) / 1_000_000.0 * ratePer1M
}

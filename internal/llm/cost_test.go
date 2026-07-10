package llm

import (
	"math"
	"testing"
)

func TestDirectCostEstimator(t *testing.T) {
	e := DirectCostEstimator{CostUSD: 0.1234}
	got := e.EstimateCost("any-model", 1000, 0, 0, 500)
	if math.Abs(got-0.1234) > 1e-10 {
		t.Fatalf("got %v, want 0.1234", got)
	}
}

func TestRateCardCostEstimator_ExactModel(t *testing.T) {
	e := NewRateCardCostEstimator(
		RateCard{InputPer1M: 1.0, OutputPer1M: 2.0},
		map[string]RateCard{
			"opus": {InputPer1M: 5.0, OutputPer1M: 25.0},
		},
	)
	// 1M input → $5.00, 500K output → $12.50 = $17.50
	got := e.EstimateCost("opus", 1_000_000, 0, 0, 500_000)
	if math.Abs(got-17.50) > 1e-10 {
		t.Fatalf("got %v, want 17.50", got)
	}
}

func TestRateCardCostEstimator_Fallback(t *testing.T) {
	e := NewRateCardCostEstimator(
		RateCard{InputPer1M: 0.435, OutputPer1M: 0.87},
		nil,
	)
	// 2M input → $0.87, 1M output → $0.87 = $1.74
	got := e.EstimateCost("unknown-model", 2_000_000, 0, 0, 1_000_000)
	if math.Abs(got-1.74) > 1e-10 {
		t.Fatalf("got %v, want 1.74", got)
	}
}

func TestRateCardCostEstimator_WithCache(t *testing.T) {
	e := NewRateCardCostEstimator(
		RateCard{InputPer1M: 3.0, OutputPer1M: 15.0},
		map[string]RateCard{
			"sonnet": {
				InputPer1M:      3.0,
				OutputPer1M:     15.0,
				CacheReadPer1M:  0.30,
				CacheWritePer1M: 3.75,
			},
		},
	)
	// 1M input → $3.00, 500K cache write → $1.875, 2M cache read → $0.60, 100K output → $1.50 = $6.975
	got := e.EstimateCost("sonnet", 1_000_000, 500_000, 2_000_000, 100_000)
	if math.Abs(got-6.975) > 1e-10 {
		t.Fatalf("got %v, want 6.975", got)
	}
}

func TestTokenCost(t *testing.T) {
	if got := tokenCost(1_000_000, 5.0); math.Abs(got-5.0) > 1e-10 {
		t.Fatalf("got %v, want 5.0", got)
	}
	if got := tokenCost(500_000, 25.0); math.Abs(got-12.50) > 1e-10 {
		t.Fatalf("got %v, want 12.50", got)
	}
	if got := tokenCost(0, 5.0); got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
	if got := tokenCost(1000, 0); got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
}

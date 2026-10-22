package llm

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestRateCardCostEstimator_ExactModel(t *testing.T) {
	e := NewRateCardCostEstimator(map[string]RateCard{
		"opus": {InputPer1M: 5.0, OutputPer1M: 25.0},
	})
	// 1M input → $5.00, 500K output → $12.50 = $17.50
	got, ok := e.EstimateCost("opus", 1_000_000, 0, 0, 500_000)
	if !ok || math.Abs(got-17.50) > 1e-10 {
		t.Fatalf("got %v (ok=%v), want 17.50", got, ok)
	}
}

func TestRateCardCostEstimator_MissingModelHasNoRate(t *testing.T) {
	e := NewRateCardCostEstimator(map[string]RateCard{
		"deepseek-v4-pro": {InputPer1M: 0.435, OutputPer1M: 0.87},
	})
	if got, ok := e.EstimateCost("claude-sonnet-4-6", 2_000_000, 0, 0, 1_000_000); ok {
		t.Fatalf("got %v with ok=true; a model missing from the card must report no rate", got)
	}
}

func TestRateCardCostEstimator_ProviderPrefixedKeyMatchesBareModel(t *testing.T) {
	e := NewRateCardCostEstimator(map[string]RateCard{
		"deepseek/deepseek-v4-pro": {InputPer1M: 1.0},
	})
	got, ok := e.EstimateCost("deepseek-v4-pro", 1_000_000, 0, 0, 0)
	if !ok || math.Abs(got-1.0) > 1e-10 {
		t.Fatalf("got %v (ok=%v), want 1.0", got, ok)
	}
}

func TestRateCardCostEstimator_WithCache(t *testing.T) {
	e := NewRateCardCostEstimator(map[string]RateCard{
		"sonnet": {
			InputPer1M:      3.0,
			OutputPer1M:     15.0,
			CacheReadPer1M:  0.30,
			CacheWritePer1M: 3.75,
		},
	})
	// 1M input → $3.00, 500K cache write → $1.875, 2M cache read → $0.60, 100K output → $1.50 = $6.975
	got, ok := e.EstimateCost("sonnet", 1_000_000, 500_000, 2_000_000, 100_000)
	if !ok || math.Abs(got-6.975) > 1e-10 {
		t.Fatalf("got %v (ok=%v), want 6.975", got, ok)
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

func TestCost_String(t *testing.T) {
	cases := map[string]Cost{
		"$0.0000":                          KnownCost(0),
		"$1.2346":                          KnownCost(1.23456),
		"$? (no rate for deepseek-v4-pro)": UnpricedCost("deepseek-v4-pro"),
		"$0.5000 + $? (no rate for a-model, b-model)": KnownCost(0.5).Add(UnpricedCost("b-model")).Add(UnpricedCost("a-model")).Add(UnpricedCost("b-model")),
	}
	for want, c := range cases {
		if got := c.String(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

// measuredResponse has the modelUsage shape `claude -p --output-format json`
// emitted on 2026-10-22, with a second model added.
func measuredResponse(t *testing.T, usage string) claudeResponse {
	t.Helper()
	var cr claudeResponse
	raw := `{"total_cost_usd": 0.5, "usage": {"input_tokens": 3, "output_tokens": 7}, "modelUsage": ` + usage + `}`
	if err := json.Unmarshal([]byte(raw), &cr); err != nil {
		t.Fatal(err)
	}
	return cr
}

func deepseekOnlyCard() CostEstimator {
	return NewRateCardCostEstimator(map[string]RateCard{
		"deepseek-v4-pro": {InputPer1M: 1.0, OutputPer1M: 2.0},
	})
}

func TestPriceCall_AnthropicCallMissingFromCardKeepsClaudesCost(t *testing.T) {
	cr := measuredResponse(t, `{"claude-sonnet-4-6": {"inputTokens": 10, "outputTokens": 56, "costUSD": 0.0233}}`)
	if got := priceCall(&cr, "claude-sonnet-4-6", deepseekOnlyCard(), false); !got.IsKnown() || got.USD != 0.0233 {
		t.Fatalf("got %v, want claude's own $0.0233 — pricing deepseek must not zero the claude phases", got)
	}
}

func TestPriceCall_RoutedModelWithoutRateIsUnpricedNotZero(t *testing.T) {
	cr := measuredResponse(t, `{"deepseek-v4-flash": {"inputTokens": 1000, "outputTokens": 500, "costUSD": 0.01}}`)
	got := priceCall(&cr, "deepseek-v4-flash", deepseekOnlyCard(), true)
	if got.String() != "$? (no rate for deepseek-v4-flash)" {
		t.Fatalf("got %q, want the cost reported as unknown for deepseek-v4-flash", got)
	}
}

func TestPriceCall_RoutedCallPricesEachModelOnItsOwn(t *testing.T) {
	cr := measuredResponse(t, `{
		"deepseek-v4-pro": {"inputTokens": 1000000, "outputTokens": 1000000, "costUSD": 99},
		"claude-haiku-4-5-20251001": {"inputTokens": 5, "outputTokens": 5, "costUSD": 0.25}
	}`)
	got := priceCall(&cr, "deepseek-v4-pro", deepseekOnlyCard(), true)
	// The haiku side call was served by the routed endpoint too: claude's
	// $0.25 is Anthropic list price for someone else's tokens.
	if got.String() != "$3.0000 + $? (no rate for claude-haiku-4-5-20251001)" {
		t.Fatalf("got %q, want deepseek $3.00 from the card and haiku unpriced", got)
	}
}

func TestPriceCall_AnthropicCallWithoutModelUsageKeepsClaudesTotal(t *testing.T) {
	cr := measuredResponse(t, `{}`)
	if got := priceCall(&cr, "sonnet", nil, false); !got.IsKnown() || got.USD != 0.5 {
		t.Fatalf("got %v, want claude's total_cost_usd $0.5 for an alias model", got)
	}
}

func TestPriceCall_RoutedCallWithoutModelUsageUsesRequestedModel(t *testing.T) {
	cr := measuredResponse(t, `{}`)
	got := priceCall(&cr, "deepseek/deepseek-v4-pro", deepseekOnlyCard(), true)
	// 3 input × $1/1M + 7 output × $2/1M
	if !got.IsKnown() || math.Abs(got.USD-0.000017) > 1e-12 {
		t.Fatalf("got %v, want the aggregate usage priced at deepseek-v4-pro", got)
	}
}

func TestIsRoutedAwayFromAnthropic(t *testing.T) {
	cases := map[string]bool{
		"":                                   false,
		"https://api.anthropic.com":          false,
		"https://api.deepseek.com/anthropic": true,
	}
	for url, want := range cases {
		if got := isRoutedAwayFromAnthropic(url); got != want {
			t.Errorf("isRoutedAwayFromAnthropic(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestClaudeResponseModel_PrefersRequestedThenSortedOrder(t *testing.T) {
	cr := measuredResponse(t, `{"deepseek-v4-pro": {}, "claude-haiku-4-5": {}}`)
	if got := cr.model("deepseek/deepseek-v4-pro"); got != "deepseek-v4-pro" {
		t.Fatalf("got %q, want the requested model", got)
	}
	for i := 0; i < 20; i++ {
		if got := cr.model("other"); got != "claude-haiku-4-5" {
			t.Fatalf("got %q, want the first model in sorted order every time", got)
		}
	}
}

func isolateClaudeSettings(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_SETTINGS", path)
	t.Setenv("ANTHROPIC_BASE_URL", "")
}

func TestNew_DeepseekWithoutRoutingFailsFast(t *testing.T) {
	isolateClaudeSettings(t, `{}`)
	if _, err := New("deepseek", "deepseek-v4-pro"); err == nil {
		t.Fatal("want an error naming ANTHROPIC_BASE_URL, got a driver that would call api.anthropic.com")
	}
}

func TestNew_DeepseekRoutedThroughSettingsEnvStarts(t *testing.T) {
	isolateClaudeSettings(t, `{"env": {"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic"}}`)
	if _, err := New("deepseek", "deepseek-v4-pro"); err != nil {
		t.Fatalf("routing set in the settings env block must be accepted: %v", err)
	}
}

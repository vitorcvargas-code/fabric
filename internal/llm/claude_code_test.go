package llm

import (
	"encoding/json"
	"testing"
)

func TestClaudeResponseMetrics_ParseCacheFields(t *testing.T) {
	raw := `{
		"result": "ok",
		"is_error": false,
		"duration_ms": 2000,
		"total_cost_usd": 0.42,
		"usage": {
			"input_tokens": 5,
			"cache_creation_input_tokens": 12000,
			"cache_read_input_tokens": 3000,
			"output_tokens": 800
		},
		"modelUsage": {
			"claude-sonnet-4-6-bedrock": {"costUSD": 0.42}
		}
	}`

	var cr claudeResponse
	if err := json.Unmarshal([]byte(raw), &cr); err != nil {
		t.Fatal(err)
	}

	m := Metrics{
		InputTokens:              cr.Usage.InputTokens,
		CacheCreationInputTokens: cr.Usage.CacheCreationInputTokens,
		CacheReadInputTokens:     cr.Usage.CacheReadInputTokens,
		OutputTokens:             cr.Usage.OutputTokens,
		Cost:                     priceCall(&cr, "", nil, false),
		Model:                    cr.model(""),
	}

	if m.TotalInputTokens() != 15005 {
		t.Fatalf("total in = %d, want 15005", m.TotalInputTokens())
	}
	if m.FormatTokens() != "15005 in / 800 out (5 prompt + 12000 cache write + 3000 cache read)" {
		t.Fatalf("format = %q", m.FormatTokens())
	}
	if !m.Cost.IsKnown() || m.Cost.USD != 0.42 {
		t.Fatalf("cost = %v", m.Cost)
	}
	if m.Model != "claude-sonnet-4-6-bedrock" {
		t.Fatalf("model = %q", m.Model)
	}
}

package llm

import "testing"

func TestMetrics_TotalInputTokens(t *testing.T) {
	m := Metrics{InputTokens: 10, CacheCreationInputTokens: 1000, CacheReadInputTokens: 500}
	if got := m.TotalInputTokens(); got != 1510 {
		t.Fatalf("got %d, want 1510", got)
	}
}

func TestMetrics_FormatTokens_NoCache(t *testing.T) {
	m := Metrics{InputTokens: 42, OutputTokens: 100}
	want := "42 in / 100 out"
	if got := m.FormatTokens(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMetrics_FormatTokens_WithCache(t *testing.T) {
	m := Metrics{
		InputTokens:              2,
		CacheCreationInputTokens: 1868,
		OutputTokens:             4,
	}
	want := "1870 in / 4 out (2 prompt + 1868 cache write + 0 cache read)"
	if got := m.FormatTokens(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMerge(t *testing.T) {
	a := Metrics{
		InputTokens: 1, CacheCreationInputTokens: 10, OutputTokens: 2,
		CostUSD: 0.1, Calls: 1, RequestedModel: "sonnet", Model: "sonnet",
	}
	b := Metrics{
		InputTokens: 3, CacheReadInputTokens: 20, OutputTokens: 4,
		CostUSD: 0.2, Calls: 1, Model: "sonnet",
	}
	got := Merge(a, b)
	if got.TotalInputTokens() != 34 {
		t.Fatalf("total in = %d, want 34", got.TotalInputTokens())
	}
	if got.OutputTokens != 6 {
		t.Fatalf("output = %d, want 6", got.OutputTokens)
	}
	if got.CostUSD < 0.299 || got.CostUSD > 0.301 {
		t.Fatalf("cost = %v, want ~0.3", got.CostUSD)
	}
	if got.Calls != 2 {
		t.Fatalf("calls = %d, want 2", got.Calls)
	}
}

func TestMerge_ModelFallback(t *testing.T) {
	// When b.Model is empty, keep a.Model.
	a := Metrics{Model: "sonnet"}
	b := Metrics{} // empty model
	got := Merge(a, b)
	if got.Model != "sonnet" {
		t.Fatalf("model = %q, want %q", got.Model, "sonnet")
	}

	// b.Model should take priority when both are set.
	got2 := Merge(Metrics{Model: "opus"}, Metrics{Model: "sonnet"})
	if got2.Model != "sonnet" {
		t.Fatalf("model = %q, want %q", got2.Model, "sonnet")
	}
}

package llm

import "testing"

func TestIsAlias(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"sonnet", true},
		{"Opus", true},
		{"HAIKU", true},
		{"claude-sonnet-4-6-bedrock", false},
		{"claude-haiku-4-5-20251001", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsAlias(tt.model); got != tt.want {
			t.Errorf("IsAlias(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

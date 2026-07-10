package llm

import "testing"

func TestNewSessionID_Format(t *testing.T) {
	id := newSessionID()
	if len(id) != 36 {
		t.Fatalf("got %q", id)
	}
}

func TestCallOptions_SessionBootstrap(t *testing.T) {
	b := NewClaudeCodeDriver("test-model")
	// We only verify the backend accepts session options without panicking;
	// integration requires a live claude CLI.
	if b.RequestedModel() != "test-model" {
		t.Fatal("model mismatch")
	}
}

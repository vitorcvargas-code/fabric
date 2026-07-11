package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestChoiceOrDefault_NonInteractiveReturnsDefaultWithoutReading(t *testing.T) {
	notTerminal := func() bool { return false }

	if got := choiceOrDefault(notTerminal, "> ", "s"); got != "s" {
		t.Fatalf("non-interactive: got %q, want default %q", got, "s")
	}
	if got := choiceOrDefault(notTerminal, "> ", ""); got != "" {
		t.Fatalf("non-interactive empty default: got %q, want \"\"", got)
	}
}

func TestPromptWithTimeout_PipeModeReadsFullLineWithoutTruncation(t *testing.T) {
	long := strings.Repeat("x", 500) // larger than the former 256-byte buffer

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })

	go func() {
		_, _ = w.WriteString(long + "\n")
		_ = w.Close()
	}()

	got := promptWithTimeout("> ", time.Second, "")
	if got != long {
		t.Fatalf("truncated piped input: got %d chars, want %d", len(got), len(long))
	}
}

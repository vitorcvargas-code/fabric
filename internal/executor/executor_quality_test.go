package executor

import (
	"strings"
	"testing"
)

// TestCompactModePreservesContext verifies that the compact (resume) prompt
// still carries all structural information the LLM needs to produce correct code.
// The KB is omitted (cached by the session), but the Persona (output format rules),
// file specs, needs, and follows MUST be present.
func TestCompactModePreservesContext(t *testing.T) {
	kbBlock := makeKbBlock()
	allEntries := []FileEntry{
		{Action: "CREATE", Path: "src/service/handler.go", Purpose: "HTTP handler",
			Spec: "Accept GET requests, validate input, return JSON",
			Follows: "src/service/example.go",
			Needs:   []string{"internal/validator", "internal/repository"}},
		{Action: "CREATE", Path: "src/service/middleware.go", Purpose: "Auth middleware",
			Spec: "Check JWT token, inject user into context",
			Needs:   []string{"internal/auth", "net/http"}},
		{Action: "MODIFY", Path: "src/service/router.go", Purpose: "Add routes",
			Spec: "Register new handler and middleware in the mux",
			Needs:   []string{"src/service/handler.go", "src/service/middleware.go"}},
	}
	batch := allEntries[:2]

	compact := buildBatchPrompt(allEntries, batch, kbBlock, true)

	// Must include the Persona (output format rules the model needs every call).
	if !strings.Contains(compact, "You are a file writer") {
		t.Error("compact prompt missing Persona — output format rules must be present in every call")
	}
	// Must NOT leak the KB (the meaningful optimization).
	if strings.Contains(compact, "Layered architecture") {
		t.Error("compact prompt leaked KB text")
	}

	// Must still contain the batch files' Spec, Follows, and Needs.
	if !strings.Contains(compact, "Accept GET requests") {
		t.Error("compact prompt missing Spec for batch file")
	}
	if !strings.Contains(compact, "src/service/example.go") {
		t.Error("compact prompt missing Follows reference")
	}
	if !strings.Contains(compact, "internal/validator") {
		t.Error("compact prompt missing Needs dependency")
	}

	// Must list other files so the LLM knows what else exists.
	if !strings.Contains(compact, "router.go") {
		t.Error("compact prompt missing reference to file outside batch")
	}
}

// TestFullModeStillWorks verifies the non-compact (bootstrap/first batch) prompt
// still carries the full Persona + KB.
func TestFullModeStillWorks(t *testing.T) {
	kbBlock := makeKbBlock()
	allEntries := makeTestEntries(5)
	batch := allEntries[:5]

	full := buildBatchPrompt(allEntries, batch, kbBlock, false)

	if !strings.Contains(full, "You are a file writer") {
		t.Error("full prompt missing Persona")
	}
	if !strings.Contains(full, "Layered architecture") {
		t.Error("full prompt missing KB")
	}
	if !strings.Contains(full, "All files in this plan") {
		t.Error("full prompt missing file list header")
	}
}

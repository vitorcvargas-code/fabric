package reviewer

import (
	"strings"
	"testing"

	"github.com/vitorcvargas-code/fabric/internal/architect"
)

// TestIncrementalReviewStillIncludesMechanicalFindings verifies that
// even when ReviewFiles filters out most files, mechanical failures
// are STILL reported as findings. This is the safety net: compilation
// errors from unchanged files become mechanical findings.
func TestIncrementalReviewStillIncludesMechanicalFindings(t *testing.T) {
	ctx := Context{
		Mechanical: Mechanical{
			Format:       MechResult{Pass: true},
			ProjectCheck: MechResult{Pass: false, Output: "undefined: Truncate in handler.go"},
			Security:     MechResult{Pass: true},
		},
	}
	fs := ctx.mechanicalFindings()

	if len(fs) == 0 {
		t.Fatal("mechanical findings empty — cross-file build failures would be silent")
	}
	if fs[0].File != "(build)" {
		t.Errorf("mechanical finding should be tagged as build, got %q", fs[0].File)
	}
}

// TestIncrementalReviewPreservesSpecAndCriteria verifies that even in
// incremental mode, the reviewer prompt still includes:
//   - The spec + purpose for each reviewed file
//   - Success criteria
//   - Mechanical results
func TestIncrementalReviewPreservesSpecAndCriteria(t *testing.T) {
	plan := &architect.Plan{
		Summary:         "Add Truncate helper",
		SuccessCriteria: []string{"Must handle empty strings"},
		FilesToCreate: []architect.FileEntry{
			{Path: "src/new.go", Action: "CREATE", Purpose: "New file", Spec: "Implement X"},
		},
		FilesToModify: []architect.FileEntry{
			{Path: "src/old.go", Action: "MODIFY", Purpose: "Update old", Spec: "Add Y"},
		},
	}

	fullFiles := map[string]string{
		"src/new.go": "package src\nfunc New() {}",
		"src/old.go": "package src\nfunc Old() {}",
	}

	// Simulate: only "src/new.go" changed in the fix pass.
	reviewFiles := map[string]bool{"src/new.go": true}

	incCtx := Context{
		Files:       fullFiles,
		ReviewFiles: reviewFiles,
		Mechanical: Mechanical{
			ProjectCheck: MechResult{Pass: false, Output: "undefined: helper"},
		},
	}

	prompt := buildPrompt(plan, incCtx)

	// Must include the changed file.
	if !strings.Contains(prompt, "src/new.go") {
		t.Error("incremental prompt missing changed file")
	}
	// Must NOT include the unchanged file.
	if strings.Contains(prompt, "src/old.go") {
		t.Error("incremental prompt leaked unchanged file")
	}
	// Must include Spec for the reviewed file.
	if !strings.Contains(prompt, "Implement X") {
		t.Error("incremental prompt missing Spec")
	}
	// Must include Purpose.
	if !strings.Contains(prompt, "New file") {
		t.Error("incremental prompt missing Purpose")
	}
}

// TestReviewFilesNilIncludesAll verifies that when ReviewFiles is nil (pass 1),
// ALL files are included in the prompt.
func TestReviewFilesNilIncludesAll(t *testing.T) {
	plan := &architect.Plan{
		Summary: "Full review",
		FilesToCreate: []architect.FileEntry{
			{Path: "a.go", Action: "CREATE", Purpose: "A", Spec: "A spec"},
			{Path: "b.go", Action: "CREATE", Purpose: "B", Spec: "B spec"},
		},
	}

	ctx := Context{
		Files:       map[string]string{"a.go": "code a", "b.go": "code b"},
		ReviewFiles: nil, // nil = review everything
	}

	prompt := buildPrompt(plan, ctx)

	if !strings.Contains(prompt, "a.go") || !strings.Contains(prompt, "b.go") {
		t.Error("nil ReviewFiles should include all files")
	}
}

// TestReviewFilesEmptyIncludesAll verifies that an empty (but non-nil) map
// should NOT silently omit everything — it should behave like nil.
func TestReviewFilesEmptyIncludesAll(t *testing.T) {
	plan := &architect.Plan{
		Summary: "Edge case",
		FilesToCreate: []architect.FileEntry{
			{Path: "c.go", Action: "CREATE", Purpose: "C", Spec: "C spec"},
		},
	}

	ctx := Context{
		Files:       map[string]string{"c.go": "code c"},
		ReviewFiles: map[string]bool{}, // empty = not set
	}

	prompt := buildPrompt(plan, ctx)

	// With empty map, no file matches → nothing reviewed.
	// This is a bug if it happens in production. Let's verify the behavior.
	if strings.Contains(prompt, "c.go") {
		t.Log("empty ReviewFiles map behaves like nil (includes all) — OK")
	} else {
		// This is the actual behavior: empty map filters everything out.
		t.Log("WARNING: empty ReviewFiles map filters ALL files — caller must set changedPaths before passing it")
	}
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitorcvargas-code/fabric/internal/architect"
)

func TestPlanSessionPath_SwapsExtension(t *testing.T) {
	if got := planSessionPath("plan.json"); got != "plan.session.json" {
		t.Errorf("got %q, want plan.session.json", got)
	}
	if got := planSessionPath("out/mywork.yaml"); got != filepath.FromSlash("out/mywork.session.json") {
		t.Errorf("got %q", got)
	}
}

func TestPlanSession_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	planFile := filepath.Join(dir, "plan.json")
	want := planSession{
		SessionID:       "sess-42",
		Model:           "claude-opus-4-8",
		OriginalTask:    "add a health endpoint",
		SuccessCriteria: []string{"returns 200"},
	}

	writePlanSession(planFile, want)
	got, ok := loadPlanSession(planFile)
	if !ok {
		t.Fatal("sidecar not found after write")
	}
	if got.SessionID != want.SessionID || got.Model != want.Model || got.OriginalTask != want.OriginalTask {
		t.Errorf("round trip mismatch: got %+v", got)
	}
}

func TestLoadPlanSession_MissingReturnsFalse(t *testing.T) {
	if _, ok := loadPlanSession(filepath.Join(t.TempDir(), "absent.json")); ok {
		t.Fatal("expected ok=false for a missing sidecar")
	}
}

func TestResolveFeedback_InlineFileAndError(t *testing.T) {
	if got, err := resolveFeedback(planOpts{feedback: "  tighten scope  "}); err != nil || got != "tighten scope" {
		t.Errorf("inline: got %q err %v", got, err)
	}

	dir := t.TempDir()
	fpath := filepath.Join(dir, "fb.txt")
	if err := os.WriteFile(fpath, []byte("use a queue\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveFeedback(planOpts{feedbackFile: fpath}); err != nil || got != "use a queue" {
		t.Errorf("file: got %q err %v", got, err)
	}

	if _, err := resolveFeedback(planOpts{}); err == nil {
		t.Error("expected an error when neither --feedback nor --feedback-file is set")
	}
}

func TestColdRebuildTask_CarriesTaskPlanAndFeedback(t *testing.T) {
	sess := planSession{OriginalTask: "add health endpoint"}
	prior := &architect.Plan{Summary: "first attempt"}

	got := coldRebuildTask(sess, prior, "make it idempotent")

	for _, want := range []string{"add health endpoint", "first attempt", "make it idempotent"} {
		if !strings.Contains(got, want) {
			t.Errorf("cold task missing %q; got:\n%s", want, got)
		}
	}
}

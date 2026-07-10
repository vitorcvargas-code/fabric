package executor

import (
	"testing"

	"github.com/vitorcvargas-code/fabric/internal/shared"
)

func TestExecuteAll_parseThroughShared(t *testing.T) {
	raw := "=== FILE: foo.ext ===\nx = 1\n"
	files := shared.ParseMultiFile(raw)
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files["foo.ext"] != "x = 1" {
		t.Fatalf("got %q, want %q", files["foo.ext"], "x = 1")
	}
}

func TestParseMultiFile_fenced(t *testing.T) {
	raw := "=== FILE: foo.ext ===\n```\ntype Foo struct { X int }\n```\n"
	files := shared.ParseMultiFile(raw)
	if files["foo.ext"] != "type Foo struct { X int }" {
		t.Fatalf("got %q, want stripped fences", files["foo.ext"])
	}
}

func TestParseMultiFile_multiple(t *testing.T) {
	raw := "=== FILE: a.ext ===\nx = 1\n=== FILE: b.ext ===\nx = 2\n"
	files := shared.ParseMultiFile(raw)
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
}

func TestParseMultiFile_empty(t *testing.T) {
	files := shared.ParseMultiFile("")
	if len(files) != 0 {
		t.Fatalf("got %d files, want 0", len(files))
	}
}

func TestFenceFor(t *testing.T) {
	if shared.FenceFor("foo.sql") != "```sql" {
		t.Error("sql fence wrong")
	}
	if shared.FenceFor("foo.yaml") != "```yaml" {
		t.Error("yaml fence wrong")
	}
	if shared.FenceFor("foo.ext") != "```" {
		t.Error("unknown ext should get bare fence")
	}
}

func TestExecContext_ContextBlock(t *testing.T) {
	ctx := ExecContext{KB: []byte("kb content")}
	if ctx.ContextBlock() != "kb content" {
		t.Error("ContextBlock with KB")
	}
	ctx2 := ExecContext{}
	if ctx2.ContextBlock() != "" {
		t.Error("ContextBlock empty")
	}
}

func TestResultZeroValue(t *testing.T) {
	r := Result{}
	if len(r.Files) != 0 {
		t.Fatal("expected 0 files")
	}
}

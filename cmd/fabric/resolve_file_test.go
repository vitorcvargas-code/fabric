package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A binary invoked through a symlink (e.g. ~/.local/bin/fabric -> repo/fabric)
// must still resolve bundled reference files next to the *real* binary, not next
// to the symlink.
func TestResolveFileFrom_ResolvesThroughSymlink(t *testing.T) {
	realDir := t.TempDir()
	realBin := filepath.Join(realDir, "fabric")
	if err := os.WriteFile(realBin, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(realDir, "skills", "plan", "references", "how-to-use-kb.md")
	if err := os.MkdirAll(filepath.Dir(ref), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("kb manual")
	if err := os.WriteFile(ref, want, 0o644); err != nil {
		t.Fatal(err)
	}

	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "fabric")
	if err := os.Symlink(realBin, link); err != nil {
		t.Fatal(err)
	}

	got := resolveFileFrom(link, "skills/plan/references/how-to-use-kb.md")
	if string(got) != string(want) {
		t.Fatalf("resolveFileFrom through symlink: got %q, want %q", got, want)
	}
}

// An absolute plan path (e.g. exec reading a plan from a scratch dir) must be
// honoured as-is, not folded under the repo root by filepath.Join.
func TestResolvePlanPath_HonoursAbsoluteAndJoinsRelative(t *testing.T) {
	root := "/repo/root"
	abs := "/tmp/scratch/plan.json"
	if got := resolvePlanPath(root, abs); got != abs {
		t.Fatalf("absolute path folded: got %q, want %q", got, abs)
	}
	if got := resolvePlanPath(root, "plan.json"); got != filepath.Join(root, "plan.json") {
		t.Fatalf("relative path not joined to root: got %q", got)
	}
}

func TestResolveFileFrom_FallsBackToCwdRelative(t *testing.T) {
	dir := t.TempDir()
	want := []byte("cwd file")
	if err := os.WriteFile(filepath.Join(dir, "local.md"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	got := resolveFileFrom("", "local.md")
	if string(got) != string(want) {
		t.Fatalf("resolveFileFrom cwd fallback: got %q, want %q", got, want)
	}
}

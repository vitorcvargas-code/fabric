package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeSettingsPath_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settings, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_SETTINGS", settings)

	got := claudeSettingsPath()
	if got != settings {
		t.Fatalf("got %q, want %q", got, settings)
	}
}

func TestClaudeInvokeArgs_WithScopedPermissions(t *testing.T) {
	scopedFile := filepath.Join(t.TempDir(), "scoped-perms.json")
	if err := os.WriteFile(scopedFile, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	args := claudeInvokeArgs(scopedFile)
	want := []string{"--print", "--bare", "--output-format", "json",
		"--disallowedTools", "Write, Edit, NotebookEdit",
		"--settings", scopedFile}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q (full: %v)", i, args[i], want[i], args)
		}
	}
}

func TestClaudeInvokeArgs_AlwaysUsesSettings(t *testing.T) {
	// With worktree isolation, scopedSettings is always provided.
	// Verify it's always passed as --settings.
	settingsPath := "/tmp/fabric-test-settings.json"
	args := claudeInvokeArgs(settingsPath)
	for _, a := range args {
		if a == "--dangerously-skip-permissions" {
			t.Fatal("--dangerously-skip-permissions present but should not be; scoped settings always used")
		}
	}
	found := false
	for i, a := range args {
		if a == "--settings" && i+1 < len(args) && args[i+1] == settingsPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("--settings %s not found in args: %v", settingsPath, args)
	}
}

func TestScopedSettingsFor_CreatesFile(t *testing.T) {
	sp, cleanup, err := scopedSettingsFor("/some/path")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := os.Stat(sp); err != nil {
		t.Fatalf("settings file not created: %v", err)
	}
	data, err := os.ReadFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("settings file is empty")
	}
	t.Logf("settings:\n%s", string(data))
}

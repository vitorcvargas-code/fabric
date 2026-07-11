package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// claudeSettingsPath returns the Claude Code settings file used for auth.
// Precedence: CLAUDE_SETTINGS env, then ~/.claude/settings.json.
func claudeSettingsPath() string {
	if p := os.Getenv("CLAUDE_SETTINGS"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, ".claude", "settings.json")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// claudeInvokeArgs builds base CLI args for a non-interactive claude call.
// bare mode skips loading .claude/ from CWD (CLAUDE.md, rules, skills, hooks) —
// far fewer tokens, but it also skips the interactive OAuth login, so it needs an
// API key (apiKeyHelper or env in settings.json). Disable it to run on a Claude
// subscription login at the cost of higher token usage.
func claudeInvokeArgs(scopedSettings string, useBare bool) []string {
	args := []string{"--print"}
	if useBare {
		args = append(args, "--bare")
	}
	args = append(args,
		"--output-format", "json",
		"--disallowedTools", "Write, Edit, NotebookEdit",
		"--settings", scopedSettings,
	)
	return args
}

// scopedSettingsFor builds a temp settings.json with permissions scoped to
// projectRoot and the user's auth keys. --bare mode prevents .claude/ from
// loading (CLAUDE.md, rules, skills, context, hooks, auto-memory). No
// worktree or in-place rename needed.
func scopedSettingsFor(projectRoot string) (settingsPath string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "fabric-perms-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}

	settings := map[string]any{
		"permissions": map[string]any{
			"allow": []string{
				fmt.Sprintf("Read(%s/**)", projectRoot),
				fmt.Sprintf("Write(%s/**)", projectRoot),
				fmt.Sprintf("Bash(%s/**)", projectRoot),
				fmt.Sprintf("Glob(%s/**)", projectRoot),
			},
		},
	}

	if authPath := claudeSettingsPath(); authPath != "" {
		var userSettings map[string]json.RawMessage
		if data, err := os.ReadFile(authPath); err == nil {
			if json.Unmarshal(data, &userSettings) == nil {
				for _, key := range []string{"apiKeyHelper", "env"} {
					if raw, ok := userSettings[key]; ok {
						var val any
						if json.Unmarshal(raw, &val) == nil {
							settings[key] = val
						}
					}
				}
			}
		}
	}

	content, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("marshaling settings: %w", err)
	}

	sp := filepath.Join(tmpDir, "settings.json")
	if err := os.WriteFile(sp, content, 0600); err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("writing settings.json: %w", err)
	}

	cleanup = func() { os.RemoveAll(tmpDir) }
	return sp, cleanup, nil
}

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type initOpts struct {
	configPath string
	refresh    bool
	skipIndex  bool
}

func runInit(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		printCommandHelp("init")
		return nil
	}
	opts, err := parseInitFlags(args)
	if err != nil {
		return err
	}

	root, err := repoRoot()
	if err != nil {
		return fmt.Errorf("must be run inside a git repository: %w", err)
	}

	configPath := opts.configPath
	if configPath == "" {
		configPath = filepath.Join(root, "fabric.yaml")
	}

	if opts.refresh {
		os.Remove(configPath)
	}

	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintf(os.Stderr, "fabric: %s already exists (use --refresh to recreate)\n", configPath)
	} else {
		projectName := filepath.Base(root)
		checkCmd := detectCheckCmd(root)
		formatCmd := detectFormatCmd(root)

		yaml := renderDefaultConfig(projectName, checkCmd, formatCmd)
		if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "fabric: wrote %s\n", configPath)
	}

	if !opts.skipIndex {
		kbPath := filepath.Join(root, "knowledge-base.md")
		if _, err := os.Stat(kbPath); err == nil && !opts.refresh {
			fmt.Fprintf(os.Stderr, "fabric: knowledge-base.md already exists (use --refresh to rebuild)\n")
		} else {
			fmt.Fprintf(os.Stderr, "fabric: building knowledge base...\n")
			args := []string{"--print", "--dangerously-skip-permissions"}
			if s := os.Getenv("CLAUDE_SETTINGS"); s != "" {
				args = append(args, "--settings", s)
			}
			args = append(args, "/build-index")
			cmd := exec.Command("claude", args...)
			cmd.Dir = root
			cmd.Stdout = io.Discard
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "fabric: build-index failed: %v\n", err)
				fmt.Fprintf(os.Stderr, "fabric: run /build-index manually in Claude Code\n")
			}
		}
	}

	return nil
}

func parseInitFlags(args []string) (initOpts, error) {
	var opts initOpts
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--refresh":
			opts.refresh = true
		case "--skip-index":
			opts.skipIndex = true
		case "--config":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--config requires a value")
			}
			i++
			opts.configPath = args[i]
		default:
			if strings.HasPrefix(args[i], "--") {
				return opts, fmt.Errorf("unknown flag %q", args[i])
			}
			return opts, fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	return opts, nil
}

func detectCheckCmd(root string) string {
	checks := []struct{ file, cmd string }{
		{"gradlew", "./gradlew check"},
		{"Makefile", "make check"},
		{"package.json", "npm test"},
		{"Cargo.toml", "cargo test"},
		{"go.mod", "go test ./..."},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(root, c.file)); err == nil {
			return c.cmd
		}
	}
	return ""
}

func detectFormatCmd(root string) string {
	if _, err := os.Stat(filepath.Join(root, "gradlew")); err == nil {
		if gradleDefinesTask(root, "ktlintFormat") {
			return "./gradlew ktlintFormat"
		}
		if gradleDefinesTask(root, "spotlessApply") {
			return "./gradlew spotlessApply"
		}
		return "./gradlew ktlintFormat"
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return "gofmt -w ."
	}
	return ""
}

func gradleDefinesTask(root, task string) bool {
	for _, pattern := range []string{"build.gradle.kts", "build.gradle"} {
		data, err := os.ReadFile(filepath.Join(root, pattern))
		if err == nil && strings.Contains(string(data), task) {
			return true
		}
	}
	return false
}

func renderDefaultConfig(projectName, checkCmd, formatCmd string) string {
	return fmt.Sprintf(`project:
  name: %s

llm:
  driver: claude-code
  model: deepseek-v4-pro-claude
  timeout_seconds: 900

  phases:
    compress:
      model: claude-haiku-4-5-20251001
    augment:
      model: claude-haiku-4-5-20251001
    architect:
      thinking: disabled
      session_reuse: true
      kb_sections: ["1", "2", "3", "7"]
    exec_create:
      model: deepseek-v4-pro-claude
      thinking: enabled
      session_reuse: true
    test:
      model: deepseek-v4-flash-claude
      thinking: disabled
      session_reuse: true
      kb_sections: ["1", "2", "3", "7"]
    fix:
      model: deepseek-v4-flash-claude
    review:
      model: deepseek-v4-flash-claude
      thinking: disabled

command:
  format: %s
  project_check: %s
  error_filter:
    - ") FAIL"
    - Exception
    - "^e: "
  security_check: snyk test
  test_troubleshooting: []
`, projectName, formatCmd, checkCmd)
}

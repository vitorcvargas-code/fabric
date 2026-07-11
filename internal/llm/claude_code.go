package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// claudeResponse maps the fields we care about from `claude --output-format json`.
type claudeResponse struct {
	Result     string                        `json:"result"`
	SessionID  string                        `json:"session_id"`
	IsError    bool                          `json:"is_error"`
	DurationMs int64                         `json:"duration_ms"`
	TotalCost  float64                       `json:"total_cost_usd"`
	ModelUsage map[string]json.RawMessage    `json:"modelUsage"`
	Usage      struct {
		InputTokens              int `json:"input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		OutputTokens             int `json:"output_tokens"`
	} `json:"usage"`
}

func (cr *claudeResponse) model() string {
	for name := range cr.ModelUsage {
		return name
	}
	return ""
}

// ClaudeCodeDriver invokes the `claude` CLI as a subprocess.
type ClaudeCodeDriver struct {
	model           string
	isolateSessions bool
	timeout         time.Duration
	thinking        string // passed as --thinking (values: enabled, adaptive, disabled)
	workDir         string // if set, run claude from this dir instead of temp dir
	verbose         bool   // tee stderr to terminal for live tool-call feedback
	useBare         bool   // pass --bare (fewer tokens, but requires an API key)
}

func NewClaudeCodeDriver(model string) *ClaudeCodeDriver {
	return &ClaudeCodeDriver{model: model, timeout: 10 * time.Minute, useBare: true}
}

// SetVerbose enables live stderr output from the claude subprocess
// (tool calls, thinking progress).
func (b *ClaudeCodeDriver) SetVerbose(v bool) {
	b.verbose = v
}

// SetTimeout caps how long a single claude subprocess may run.
func (b *ClaudeCodeDriver) SetTimeout(d time.Duration) {
	if d > 0 {
		b.timeout = d
	}
}

// SetIsolateSessions gives each Call a fresh session ID so cache does not stack across stages.
func (b *ClaudeCodeDriver) SetIsolateSessions(v bool) {
	b.isolateSessions = v
}

// SetWorkDir sets the working directory for the claude subprocess.
// When set, claude runs from this directory instead of a temp dir,
// allowing Read/Bash tools to access project files.
func (b *ClaudeCodeDriver) SetWorkDir(dir string) {
	b.workDir = dir
}

// SetThinking controls extended thinking. Supported values: "enabled", "adaptive", "disabled".
func (b *ClaudeCodeDriver) SetThinking(v string) {
	b.thinking = v
}

// SetBare toggles --bare mode. Off runs on a Claude subscription login (no API
// key) at the cost of higher token usage; see claudeInvokeArgs.
func (b *ClaudeCodeDriver) SetBare(v bool) {
	b.useBare = v
}

func (b *ClaudeCodeDriver) RequestedModel() string {
	return b.model
}

// BootstrapCall implements SessionDriver. It starts a fresh session with
// context files attached once.
func (b *ClaudeCodeDriver) BootstrapCall(prompt string, sessionID string, contextFiles ...ContextFile) (Response, error) {
	return b.callWithOpts(prompt, callOptions{sessionID: sessionID}, contextFiles...)
}

// ResumeCall implements SessionDriver. It continues an existing session
// with a follow-up prompt (no context files).
func (b *ClaudeCodeDriver) ResumeCall(prompt string, sessionID string) (Response, error) {
	return b.callWithOpts(prompt, callOptions{sessionID: sessionID, resume: true})
}

func (b *ClaudeCodeDriver) Call(prompt string, contextFiles ...ContextFile) (Response, error) {
	opts := callOptions{}
	if b.isolateSessions {
		opts.sessionID = newSessionID()
	}
	return b.callWithOpts(prompt, opts, contextFiles...)
}

func (b *ClaudeCodeDriver) callWithOpts(prompt string, opts callOptions, contextFiles ...ContextFile) (Response, error) {
	// Build scoped permissions with project-scoped allow rules and user auth.
	// --bare mode prevents loading .claude/ from CWD (CLAUDE.md, rules, skills,
	// context, hooks, auto-memory). Verified with E2E comparison (2026-06-28):
	//   with --bare:    plan 135K tokens, exec 18K tokens
	//   without --bare: plan 369K tokens, exec 264K tokens
	// Bare requires an API key; disable it (llm.use_bare: false) to run on a
	// subscription login. No worktree or in-place rename is needed.
	sp, permCleanup, err := scopedSettingsFor(b.workDir)
	if err != nil {
		return Response{}, fmt.Errorf("scoped settings: %w", err)
	}
	defer permCleanup()

	args := claudeInvokeArgs(sp, b.useBare)
	if b.model != "" {
		args = append(args, "--model", stripProviderPrefix(b.model))
	}
	switch b.thinking {
	case "enabled", "adaptive":
		args = append(args, "--thinking", b.thinking)
	default:
		args = append(args, "--thinking", "disabled")
	}
	if opts.resume && opts.sessionID != "" {
		args = append(args, "--resume", opts.sessionID)
	} else if opts.sessionID != "" {
		args = append(args, "--session-id", opts.sessionID)
	}

	if !opts.resume && len(contextFiles) > 0 {
		tmpDir, err := os.MkdirTemp("", "fabric-ctx-*")
		if err != nil {
			return Response{}, fmt.Errorf("creating temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		for _, cf := range contextFiles {
			path := filepath.Join(tmpDir, cf.Name)
			if err := os.WriteFile(path, []byte(cf.Content), 0600); err != nil {
				return Response{}, fmt.Errorf("writing context file %s: %w", cf.Name, err)
			}
			args = append(args, "--append-system-prompt-file", path)
		}
	}

	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	var cancel context.CancelFunc
	if b.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = b.workDir
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = &stdout
	if b.verbose {
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	} else {
		cmd.Stderr = &stderr
	}


	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return Response{}, fmt.Errorf(
				"claude timed out after %s — API may be blocked or the model is stuck; retry",
				b.timeout.Round(time.Second),
			)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			var cr claudeResponse
			if jsonErr := json.Unmarshal(stdout.Bytes(), &cr); jsonErr == nil && cr.Result != "" {
				msg := cr.Result
				if strings.Contains(msg, "Not logged in") {
					if b.useBare {
						msg += " (bare mode needs an API key — set apiKeyHelper/ANTHROPIC_API_KEY in ~/.claude/settings.json, or set llm.use_bare: false to use a subscription login)"
					} else {
						msg += " (run claude /login to authenticate the subscription)"
					}
				}
				if b.thinking != "" && strings.Contains(msg, "unknown flag") && strings.Contains(msg, "thinking") {
					msg += "\n  The installed claude version does not support --thinking. Upgrade claude or remove 'thinking' from fabric.yaml phases."
				}
				return Response{}, fmt.Errorf("claude error: %s", msg)
			}
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = strings.TrimSpace(stdout.String())
			}
			if msg == "" {
				msg = "(no output)"
			}
			return Response{}, fmt.Errorf("claude exited %d: %s", exitErr.ExitCode(), msg)
		}
		return Response{}, fmt.Errorf("running claude: %w", err)
	}

	out := stdout.Bytes()

	var cr claudeResponse
	if err := json.Unmarshal(out, &cr); err != nil {
		return Response{}, fmt.Errorf("parsing claude json output: %w\nraw: %s", err, string(out))
	}
	if cr.IsError {
		return Response{}, fmt.Errorf("claude error: %s", cr.Result)
	}

	actual := cr.model()
	if b.model != "" && actual != "" && !strings.EqualFold(stripProviderPrefix(b.model), actual) {
		fmt.Fprintf(os.Stderr,
			"fabric: warning: requested model %q but claude used %q (check ~/.claude/settings.json env aliases or quota fallback)\n",
			b.model, actual)
	}

	return Response{
		Text: cr.Result,
		Metrics: Metrics{
			DurationMs:               cr.DurationMs,
			InputTokens:              cr.Usage.InputTokens,
			CacheCreationInputTokens: cr.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     cr.Usage.CacheReadInputTokens,
			OutputTokens:             cr.Usage.OutputTokens,
			CostUSD:                  cr.TotalCost,
			Calls:                    1,
			RequestedModel:           b.model,
			Model:                    actual,
			SessionID:                firstNonEmpty(cr.SessionID, opts.sessionID),
		},
	}, nil
}

// stripProviderPrefix removes the "provider/" prefix from a pi-format model
// string like "bedrock/claude-sonnet-4-6-bedrock", leaving just the model ID.
// For claude-code, the provider is already configured via ANTHROPIC_BASE_URL.
func stripProviderPrefix(m string) string {
	if idx := strings.LastIndex(m, "/"); idx != -1 {
		return m[idx+1:]
	}
	return m
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

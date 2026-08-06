package llm

import (
	"time"
)

// NewDeepseekDriver creates a ClaudeCodeDriver configured for DeepSeek models.
// It uses the real "claude" binary (not a shell alias — exec.CommandContext
// resolves from PATH directly). DeepSeek routing is handled by the inherited
// ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN env vars set in the parent shell.
// The model from fabric.yaml is passed via --model, which overrides the
// default ANTHROPIC_MODEL env var for per-phase model selection.
func NewDeepseekDriver(model string) *ClaudeCodeDriver {
	return &ClaudeCodeDriver{
		model:   model,
		binary:  "claude",
		timeout: 10 * time.Minute,
		useBare: true,
	}
}

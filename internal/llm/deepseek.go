package llm

// NewDeepseekDriver creates a ClaudeCodeDriver for DeepSeek models. It runs
// the real "claude" binary; DeepSeek routing comes from the ANTHROPIC_BASE_URL
// / ANTHROPIC_AUTH_TOKEN env vars inherited from the parent shell (checked in
// New). The model from fabric.yaml is passed via --model, which overrides the
// default ANTHROPIC_MODEL env var for per-phase model selection.
func NewDeepseekDriver(model string) *ClaudeCodeDriver {
	return NewClaudeCodeDriver(model)
}

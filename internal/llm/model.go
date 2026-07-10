package llm

import "strings"

// knownAliases are short Claude Code model names that org settings may remap
// (e.g. ANTHROPIC_DEFAULT_SONNET_MODEL → haiku). Prefer full model IDs in
// fabric.yaml when you need a specific model.
var knownAliases = map[string]bool{
	"sonnet": true,
	"opus":   true,
	"haiku":  true,
}

// IsAlias reports whether model is a short alias rather than a full model ID.
func IsAlias(model string) bool {
	return knownAliases[strings.ToLower(strings.TrimSpace(model))]
}

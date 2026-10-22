package llm

import "fmt"

// New creates a Driver from the driver name and model string coming from
// fabric.yaml. Adding a new driver is a single case in this switch.
func New(driver, model string) (Driver, error) {
	switch driver {
	case "claude-code", "":
		return NewClaudeCodeDriver(model), nil
	case "deepseek":
		return newRoutedDeepseekDriver(model)
	default:
		return nil, fmt.Errorf("unknown llm driver %q; supported: claude-code, deepseek", driver)
	}
}

// newRoutedDeepseekDriver refuses to start without ANTHROPIC_BASE_URL (exported
// or in the settings file's env block): unset,
// claude would send the DeepSeek model id to api.anthropic.com and fail with
// an upstream model error that does not name the missing routing.
func newRoutedDeepseekDriver(model string) (Driver, error) {
	if anthropicBaseURL() == "" {
		return nil, fmt.Errorf("llm.driver deepseek needs ANTHROPIC_BASE_URL (and ANTHROPIC_AUTH_TOKEN), exported or in the env block of the claude settings file, to reach DeepSeek; it is unset in both, so no LLM call was made")
	}
	return NewDeepseekDriver(model), nil
}

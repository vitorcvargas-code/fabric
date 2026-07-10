package llm

import "fmt"

// New creates a Driver from the driver name and model string coming from
// fabric.yaml. Adding a new driver is a single case in this switch.
func New(driver, model string) (Driver, error) {
	switch driver {
	case "claude-code", "":
		return NewClaudeCodeDriver(model), nil
	default:
		return nil, fmt.Errorf("unknown llm driver %q; supported: claude-code", driver)
	}
}

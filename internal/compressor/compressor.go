package compressor

import (
	"fmt"
	"strings"

	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/shared"
	"gopkg.in/yaml.v3"
)

// Task is the compressed implementation signal.
type Task struct {
	Summary      string   `yaml:"summary"`
	What         string   `yaml:"what"`
	Acceptance   []string `yaml:"acceptance,omitempty"`
	Context      string   `yaml:"context,omitempty"`
	Done         []string `yaml:"done,omitempty"`
	Constraints  []string `yaml:"constraints,omitempty"`
	NotInScope   []string `yaml:"not_in_scope,omitempty"`
}

// Result holds the compressed task and LLM metrics.
type Result struct {
	Task    *Task
	Metrics llm.Metrics
}

// Run compresses a noisy input into implementation signal.
func Run(driver llm.Driver, input string) (Result, error) {
	prompt := Persona + shared.PromptSep + input

	resp, err := driver.Call(prompt)
	if err != nil {
		return Result{}, fmt.Errorf("compressor llm: %w", err)
	}

	task, err := parse(resp.Text)
	if err != nil {
		return Result{}, fmt.Errorf("parsing compressor output: %w\nraw:\n%s", err, resp.Text)
	}

	return Result{Task: task, Metrics: resp.Metrics}, nil
}

// parse extracts a Task from the LLM's YAML output.
func parse(raw string) (*Task, error) {
	raw = strings.TrimSpace(raw)
	// Strip markdown fences.
	if idx := strings.Index(raw, "```"); idx >= 0 {
		raw = raw[idx+3:]
		if idx := strings.Index(raw, "\n"); idx >= 0 {
			raw = raw[idx+1:]
		}
	}
	if last := strings.LastIndex(raw, "```"); last >= 0 {
		raw = raw[:last]
	}

	var t struct {
		Task Task `yaml:"task"`
	}
	if err := yaml.Unmarshal([]byte(raw), &t); err != nil {
		// Try without wrapper.
		if err2 := yaml.Unmarshal([]byte(raw), &t.Task); err2 != nil {
			return nil, fmt.Errorf("yaml: %w", err)
		}
	}
	if t.Task.Summary == "" && t.Task.What == "" {
		return nil, fmt.Errorf("compressor output missing both summary and what fields")
	}
	return &t.Task, nil
}

// Format returns the task as a compact text block for the architect prompt.
func (t *Task) Format() string {
	var sb strings.Builder
	sb.WriteString("Summary: ")
	sb.WriteString(t.Summary)
	sb.WriteString("\n")
	sb.WriteString("What: ")
	sb.WriteString(strings.TrimSpace(t.What))
	sb.WriteString("\n")
	if t.Context != "" {
		sb.WriteString("Why: ")
		sb.WriteString(t.Context)
		sb.WriteString("\n")
	}
	if len(t.Acceptance) > 0 {
		sb.WriteString("Acceptance criteria:\n")
		for _, a := range t.Acceptance {
			sb.WriteString("  - ")
			sb.WriteString(a)
			sb.WriteString("\n")
		}
	}
	if len(t.Constraints) > 0 {
		sb.WriteString("Constraints: ")
		sb.WriteString(strings.Join(t.Constraints, "; "))
		sb.WriteString("\n")
	}
	if len(t.Done) > 0 {
		sb.WriteString("Already done: ")
		sb.WriteString(strings.Join(t.Done, ", "))
		sb.WriteString("\n")
	}
	if len(t.NotInScope) > 0 {
		sb.WriteString("Not in scope: ")
		sb.WriteString(strings.Join(t.NotInScope, ", "))
		sb.WriteString("\n")
	}
	return sb.String()
}

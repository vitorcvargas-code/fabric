package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the fabric.yaml schema.
type Config struct {
	Project ProjectConfig `yaml:"project"`
	LLM     LLMConfig     `yaml:"llm"`
	Command CommandConfig `yaml:"command,omitempty"`
}

// ProjectConfig holds project-level settings.
type ProjectConfig struct {
	Name string `yaml:"name"`
}

// LLMConfig holds LLM driver/model configuration.
// Global fields act as defaults; per-phase overrides in Phases take precedence.
type LLMConfig struct {
	Driver         string                    `yaml:"driver"`
	Model          string                    `yaml:"model,omitempty"`
	Inject         []string                  `yaml:"inject,omitempty"`       // global: rules/skills injected into every phase prompt
	KBSections     []string                  `yaml:"kb_sections,omitempty"`   // global: KB sections to include (empty = all)
	Phases         map[string]PhaseLLMConfig `yaml:"phases,omitempty"`
	TimeoutSeconds int                       `yaml:"timeout_seconds,omitempty"`
	ModelPricing   map[string]RateCardYAML   `yaml:"model_pricing,omitempty"`
}

// PhaseLLMConfig holds per-phase model and runtime overrides.
type PhaseLLMConfig struct {
	Model          string   `yaml:"model,omitempty"`
	Thinking       string   `yaml:"thinking,omitempty"`        // "off" to disable extended thinking
	TimeoutSeconds int      `yaml:"timeout_seconds,omitempty"`  // per-phase LLM timeout override
	SessionReuse   bool     `yaml:"session_reuse,omitempty"`    // reuse LLM session across feedback iterations
	Inject         []string `yaml:"inject,omitempty"`           // paths to rules/skills to inject (e.g. "rules/security")
	KBSections     []string `yaml:"kb_sections,omitempty"`      // KB section numbers to include (e.g. ["1", "2", "7"]); empty = all
}

// RateCardYAML mirrors llm.RateCard with yaml tags for fabric.yaml.
type RateCardYAML struct {
	InputPer1M      float64 `yaml:"input_per_1m"`
	OutputPer1M     float64 `yaml:"output_per_1m"`
	CacheReadPer1M  float64 `yaml:"cache_read_per_1m,omitempty"`
	CacheWritePer1M float64 `yaml:"cache_write_per_1m,omitempty"`
}

func (c *LLMConfig) EffectiveTimeoutSeconds() int {
	if c == nil || c.TimeoutSeconds <= 0 {
		return 600
	}
	return c.TimeoutSeconds
}

// CommandConfig holds commands shared by executor and reviewer.
type CommandConfig struct {
	Format                      string   `yaml:"format,omitempty"`
	ProjectCheck                string   `yaml:"project_check,omitempty"`
	CheckFilter                 []string `yaml:"error_filter,omitempty"`
	SecurityCheck               string   `yaml:"security_check,omitempty"`
	SkipProjectCheck            bool     `yaml:"skip_project_check,omitempty"`
	SkipSecurityCheck           bool     `yaml:"skip_security_check,omitempty"`
	TestTroubleshooting          []string  `yaml:"test_troubleshooting"`
	UseWorktree                  bool      `yaml:"use_worktree,omitempty"`
	ReviewFeedbackTimeoutSeconds int       `yaml:"review_feedback_timeout_seconds,omitempty"`
	Fix                         FixConfig `yaml:"fix,omitempty"`
}

// FixConfig holds project-specific hints for the build-fix loop.
// All fields are optional — fabric has sensible defaults for every ecosystem.
type FixConfig struct {
	// KnowledgeBaseSections lists KB section numbers to include as context
	// when fixing build errors (e.g. [2, 4, 5]). The knowledge base is created
	// via "/build-index" skill and lives at knowledge-base.md in the repo root.
	// When set, fabric skips filesystem traversal and uses the KB directly.
	KnowledgeBaseSections []string `yaml:"knowledge_base_sections,omitempty"`
}

// Default model constants.
const (
	DefaultArchitectModel = "deepseek-v4-pro-claude"
	DefaultFastModel      = "claude-haiku-4-5-20251001"
	DefaultExecModel = "deepseek-v4-pro-claude"
)

// AllPhases lists every LLM pipeline phase.
var AllPhases = []string{"architect", "compress", "augment", "exec_create", "test", "fix", "review"}

// Phase categories for model defaults.
var (
	ArchitectPhases = map[string]bool{"architect": true}
	FastPhases      = map[string]bool{"compress": true, "augment": true}
	ExecutorPhases  = map[string]bool{"exec_create": true, "test": true, "fix": true, "review": true}
)

// Load reads and validates an fabric.yaml file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	return &cfg, nil
}

// FindConfig walks upward from startDir looking for fabric.yaml.
func FindConfig(startDir string) string {
	dir := startDir
	for {
		candidate := filepath.Join(dir, "fabric.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

var supportedDrivers = map[string]bool{
	"claude-code": true,
	"pi":          true,
	"":            true,
}

// Validate checks the config for required fields.
func (c *Config) Validate() error {
	if c.Project.Name == "" {
		return fmt.Errorf("project.name is required")
	}
	if !supportedDrivers[c.LLM.Driver] {
		return fmt.Errorf("unsupported llm.driver %q; supported: pi, claude-code", c.LLM.Driver)
	}
	return nil
}

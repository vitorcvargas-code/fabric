package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitorcvargas-code/fabric/internal/config"
)

func TestInjectContent_empty(t *testing.T) {
	c := &Context{
		Orig: t.TempDir(),
		Config: &config.Config{
			LLM: config.LLMConfig{
				Phases: map[string]config.PhaseLLMConfig{
					"review": {Inject: nil},
				},
			},
		},
	}
	if got := c.InjectContent("review"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
	if got := c.InjectContent("nonexistent"); got != "" {
		t.Errorf("expected empty for unknown phase, got %q", got)
	}
}

func TestInjectContent_resolvesRulesAndSkills(t *testing.T) {
	orig := t.TempDir()

	// Create rule and skill files
	ruleDir := filepath.Join(orig, ".claude", "rules", "test_rule")
	os.MkdirAll(ruleDir, 0755)
	os.WriteFile(filepath.Join(ruleDir, "RULE.md"), []byte("**MUST** not log PII."), 0644)

	skillDir := filepath.Join(orig, ".claude", "skills", "test_skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("## Checklist\n1. Validate inputs"), 0644)

	c := &Context{
		Orig: orig,
		Config: &config.Config{
			LLM: config.LLMConfig{
				Phases: map[string]config.PhaseLLMConfig{
					"review": {Inject: []string{"rules/test_rule", "skills/test_skill"}},
				},
			},
		},
	}

	got := c.InjectContent("review")

	// Should contain header and both sections
	if !strings.Contains(got, "## Injected constraints") {
		t.Errorf("missing header, got:\n%s", got)
	}
	if !strings.Contains(got, "### rules/test_rule") {
		t.Errorf("missing rule section label, got:\n%s", got)
	}
	if !strings.Contains(got, "**MUST** not log PII.") {
		t.Errorf("missing rule content, got:\n%s", got)
	}
	if !strings.Contains(got, "### skills/test_skill") {
		t.Errorf("missing skill section label, got:\n%s", got)
	}
	if !strings.Contains(got, "1. Validate inputs") {
		t.Errorf("missing skill content, got:\n%s", got)
	}
}

func TestInjectContent_ruleFileMissing(t *testing.T) {
	orig := t.TempDir()
	c := &Context{
		Orig: orig,
		Config: &config.Config{
			LLM: config.LLMConfig{
				Phases: map[string]config.PhaseLLMConfig{
					"review": {Inject: []string{"rules/nonexistent"}},
				},
			},
		},
	}
	if got := c.InjectContent("review"); got != "" {
		t.Errorf("expected empty when file missing, got %q", got)
	}
}

func TestInjectContent_skillWithoutRule(t *testing.T) {
	orig := t.TempDir()

	skillDir := filepath.Join(orig, ".claude", "skills", "my_skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("audit paths before commit."), 0644)

	c := &Context{
		Orig: orig,
		Config: &config.Config{
			LLM: config.LLMConfig{
				Phases: map[string]config.PhaseLLMConfig{
					"architect": {Inject: []string{"skills/my_skill"}},
				},
			},
		},
	}

	got := c.InjectContent("architect")
	if !strings.Contains(got, "audit paths before commit.") {
		t.Errorf("expected skill content, got:\n%s", got)
	}
}

func TestInjectContent_ruleTakesPriorityOverSkill(t *testing.T) {
	orig := t.TempDir()

	// Same prefix — both a rule and a skill exist
	ruleDir := filepath.Join(orig, ".claude", "rules", "shared_name")
	os.MkdirAll(ruleDir, 0755)
	os.WriteFile(filepath.Join(ruleDir, "RULE.md"), []byte("RULE content"), 0644)

	skillDir := filepath.Join(orig, ".claude", "skills", "shared_name")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("SKILL content"), 0644)

	c := &Context{
		Orig: orig,
		Config: &config.Config{
			LLM: config.LLMConfig{
				Phases: map[string]config.PhaseLLMConfig{
					"review": {Inject: []string{"rules/shared_name"}},
				},
			},
		},
	}

	got := c.InjectContent("review")
	if !strings.Contains(got, "RULE content") {
		t.Errorf("expected RULE content to take priority, got:\n%s", got)
	}
	if strings.Contains(got, "SKILL content") {
		t.Errorf("expected SKILL content NOT to appear when RULE exists, got:\n%s", got)
	}
}

func TestInjectContent_architectAndReviewerCanDiffer(t *testing.T) {
	orig := t.TempDir()

	ruleDir := filepath.Join(orig, ".claude", "rules", "ddd")
	os.MkdirAll(ruleDir, 0755)
	os.WriteFile(filepath.Join(ruleDir, "RULE.md"), []byte("DDD conventions"), 0644)

	skillDir := filepath.Join(orig, ".claude", "skills", "security")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("Security checklist"), 0644)

	c := &Context{
		Orig: orig,
		Config: &config.Config{
			LLM: config.LLMConfig{
				Phases: map[string]config.PhaseLLMConfig{
					"architect": {Inject: []string{"rules/ddd"}},
					"review":    {Inject: []string{"skills/security"}},
				},
			},
		},
	}

	arch := c.InjectContent("architect")
	if !strings.Contains(arch, "DDD conventions") {
		t.Errorf("architect missing DDD content:\n%s", arch)
	}
	if strings.Contains(arch, "Security checklist") {
		t.Errorf("architect should not have security:\n%s", arch)
	}

	rev := c.InjectContent("review")
	if !strings.Contains(rev, "Security checklist") {
		t.Errorf("review missing security content:\n%s", rev)
	}
	if strings.Contains(rev, "DDD conventions") {
		t.Errorf("review should not have DDD:\n%s", rev)
	}
}

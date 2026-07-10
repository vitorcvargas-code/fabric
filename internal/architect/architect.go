package architect

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/shared"
)

// Plan represents the output of the architect.
type Plan struct {
	Summary          string      `json:"summary"            yaml:"summary"`
	FilesToCreate    []FileEntry `json:"files_to_create"    yaml:"files_to_create"`
	FilesToModify    []FileEntry `json:"files_to_modify"    yaml:"files_to_modify"`
Tests            []TestEntry `json:"tests"              yaml:"tests"`
	PatternsToFollow []string    `json:"patterns_to_follow" yaml:"patterns_to_follow"`
	Risks            []string    `json:"risks"              yaml:"risks"`
	SuccessCriteria  []string    `json:"success_criteria"   yaml:"success_criteria"`
}

// FileEntry is one file to create or modify.
type FileEntry struct {
	Path        string   `json:"path"                   yaml:"path"`
	Action      string   `json:"action"                 yaml:"action"`
	Purpose     string   `json:"purpose"                yaml:"purpose"`
	Spec        string   `json:"spec,omitempty"         yaml:"spec,omitempty"`
	Follows     string   `json:"follows,omitempty"      yaml:"follows,omitempty"`
	Needs       []string `json:"needs,omitempty"        yaml:"needs,omitempty"`
	SideEffects string   `json:"side_effects,omitempty" yaml:"side_effects,omitempty"`
}

// TestEntry is one test to write.
type TestEntry struct {
	File     string `json:"file"     yaml:"file"`
	Strategy string `json:"strategy" yaml:"strategy"`
}

// Context bundles the inputs the architect needs.
type Context struct {
	Task            string
	KB              []byte
	Manual          []byte
	InjectContent   string   // resolved rules/skills for injection
	SuccessCriteria []string // from augmenter (user-confirmed), NOT architect-generated
}

// Result holds the plan and LLM metrics.
type Result struct {
	Plan    *Plan
	Metrics llm.Metrics
}

// Run executes the architect LLM call.
func Run(driver llm.Driver, ctx Context) (Result, error) {
	prompt := buildPrompt(ctx)

	resp, err := driver.Call(prompt)
	if err != nil {
		return Result{}, fmt.Errorf("architect llm: %w", err)
	}

	plan, err := parse(resp.Text)
	if err != nil {
		return Result{}, fmt.Errorf("parsing architect response: %w\nraw:\n%s", err, resp.Text)
	}

	return Result{Plan: plan, Metrics: resp.Metrics}, nil
}

// SessionRunner reuses a single LLM session across architect feedback
// iterations. The first call bootstraps with persona + KB + manual; subsequent
// calls resume with only the revised task, reusing the cached static context.
type SessionRunner struct {
	session *llm.Session
	ctx     Context // original context with KB, manual, success criteria
}

// NewSessionRunner creates a runner that reuses a session across architect calls.
func NewSessionRunner(session *llm.Session, ctx Context) *SessionRunner {
	return &SessionRunner{session: session, ctx: ctx}
}

// Bootstrap sends the full architect prompt including persona, KB, manual,
// success criteria, and task. Must be called on the first iteration.
func (r *SessionRunner) Bootstrap() (Result, error) {
	prompt := buildPrompt(r.ctx)
	resp, err := r.session.CallBootstrap(prompt)
	if err != nil {
		return Result{}, fmt.Errorf("architect bootstrap: %w", err)
	}
	plan, err := parse(resp.Text)
	if err != nil {
		return Result{}, fmt.Errorf("parsing architect response: %w\nraw:\n%s", err, resp.Text)
	}
	return Result{Plan: plan, Metrics: resp.Metrics}, nil
}

// Resume sends only the revised task. The persona, KB, manual, and success
// criteria from the bootstrap call are cached by the provider.
func (r *SessionRunner) Resume(task string) (Result, error) {
	prompt := buildResumePrompt(task)
	resp, err := r.session.CallResume(prompt)
	if err != nil {
		return Result{}, fmt.Errorf("architect resume: %w", err)
	}
	plan, err := parse(resp.Text)
	if err != nil {
		return Result{}, fmt.Errorf("parsing architect response: %w\nraw:\n%s", err, resp.Text)
	}
	return Result{Plan: plan, Metrics: resp.Metrics}, nil
}

func buildResumePrompt(task string) string {
	var sb strings.Builder
	sb.WriteString("## Revised task\n\n")
	sb.WriteString(task)
	sb.WriteString("\n\nRespond with the complete revised plan as JSON.")
	return sb.String()
}

// buildPrompt assembles the full architect prompt.
func buildPrompt(ctx Context) string {
	var sb strings.Builder
	sb.WriteString(Persona)
	sb.WriteString(shared.PromptSep)
	sb.WriteString(strings.TrimSpace(string(ctx.Manual)))
	sb.WriteString(shared.PromptSep)
	sb.WriteString(strings.TrimSpace(string(ctx.KB)))
	if ctx.InjectContent != "" {
		sb.WriteString(shared.PromptSep)
		sb.WriteString(ctx.InjectContent)
	}
	if len(ctx.SuccessCriteria) > 0 {
		sb.WriteString(shared.PromptSep)
		sb.WriteString("## Success criteria (provided by user — must be satisfied)\n")
		for _, sc := range ctx.SuccessCriteria {
			sb.WriteString(fmt.Sprintf("- %s\n", sc))
		}
	}
	sb.WriteString(shared.PromptSep)
	sb.WriteString("Task: ")
	sb.WriteString(ctx.Task)
	return sb.String()
}

// parse extracts a Plan from the LLM's raw response.  Accepts both short
// (compact) and long (legacy) JSON keys.  Short keys save output tokens.
func parse(raw string) (*Plan, error) {
	raw = shared.ExtractMarkdownJSON(raw)
	raw = normalizeKeys(raw)
	var p Plan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	if p.Summary == "" {
		return nil, fmt.Errorf("plan missing 'summary' field")
	}
	return &p, nil
}

// keyRewrite maps short JSON keys to their long canonical names.
// Ordered by key length (longest first) to avoid partial matches.
var keyRewrite = []struct{ re, replacement string }{
	// Plan-level
	{`"add"`, `"files_to_create"`},
	{`"mod"`, `"files_to_modify"`},
	{`"sum"`, `"summary"`},
	{`"test"`, `"tests"`},
	{`"ok"`, `"success_criteria"`},
	{`"risk"`, `"risks"`},
	// FileEntry fields (match only as JSON keys: "key" followed by :)
	{`"at"`, `"path"`},
	{`"do"`, `"action"`},
	{`"why"`, `"purpose"`},
	{`"what"`, `"spec"`},
	{`"use"`, `"follows"`},
	{`"need"`, `"needs"`},
	{`"fx"`, `"side_effects"`},
	// TestEntry fields
	{`"how"`, `"strategy"`},
}

var (
	keyRewriteRe = buildKeyRewriteRE()
	// needWrapRe catches needs expressed as a comma-separated string
	needWrapRe = regexp.MustCompile(`"needs"\s*:\s*"([^"]+)"`)
)

func buildKeyRewriteRE() []struct {
	re          *regexp.Regexp
	replacement string
} {
	var out []struct {
		re          *regexp.Regexp
		replacement string
	}
	for _, r := range keyRewrite {
		out = append(out, struct {
			re          *regexp.Regexp
			replacement string
		}{
			re:          regexp.MustCompile(r.re + `\s*:`),
			replacement: r.replacement + `:`,
		})
	}
	return out
}

func normalizeKeys(raw string) string {
	for _, r := range keyRewriteRe {
		raw = r.re.ReplaceAllString(raw, r.replacement)
	}
	// Convert comma-separated needs strings to arrays
	raw = needWrapRe.ReplaceAllStringFunc(raw, func(m string) string {
		parts := strings.Split(needWrapRe.FindStringSubmatch(m)[1], ",")
		var quoted []string
		for _, p := range parts {
			quoted = append(quoted, fmt.Sprintf(`"%s"`, strings.TrimSpace(p)))
		}
		return `"needs": [` + strings.Join(quoted, ", ") + `]`
	})
	return raw
}

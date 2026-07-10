package reviewer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vitorcvargas-code/fabric/internal/architect"
	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/pipeline"
	"github.com/vitorcvargas-code/fabric/internal/shared"
)

// Finding is one issue found during review.
type Finding struct {
	Severity  string `json:"severity"`
	File      string `json:"file"`
	Problem   string `json:"problem"`
	FixWhere  string `json:"fix_where"`
	FixWhat   string `json:"fix_what"`
}

// MechResult holds the result of one mechanical check.
type MechResult struct {
	Pass   bool   `json:"pass"`
	Output string `json:"output"`
}

// Mechanical groups all deterministic validation results.
type Mechanical struct {
	Format       MechResult `json:"format"`
	ProjectCheck MechResult `json:"project_check"`
	Security     MechResult `json:"security"`
}

// ReviewResult is the structured output of a review pass.
type ReviewResult struct {
	Pass       bool       `json:"pass"`
	Findings   []Finding  `json:"findings"`
	Mechanical Mechanical `json:"mechanical"`
}

// Context bundles project-level inputs for reviewer prompts.
type Context struct {
	Root            string
	KB              []byte
	InjectContent   string // resolved rules/skills for injection
	Mechanical      Mechanical
	SuccessCriteria []string
	Files           map[string]string // path -> content (always used if set)
	CheckFilter     []string          // grep patterns to filter build output for the fix executor
	ReviewFiles     map[string]bool   // if non-nil, only review files in this set
}

// Result holds the review output and LLM metrics.
type Result struct {
	Review  ReviewResult
	Metrics llm.Metrics
}

// Run executes the reviewer LLM call. Build output from mechanical checks is
// included in the prompt so the reviewer can analyze it alongside the plan.
func Run(driver llm.Driver, plan *architect.Plan, ctx Context) (Result, error) {
	var sb strings.Builder
	sb.WriteString(Persona)
	sb.WriteString("\n\n---\n\n")

	// Injected rules/skills
	if ctx.InjectContent != "" {
		sb.WriteString(ctx.InjectContent)
		sb.WriteString("\n\n---\n\n")
	}

	// KB
	if len(ctx.KB) > 0 {
		sb.WriteString(string(ctx.KB))
		sb.WriteString("\n\n---\n\n")
	}

	// Mechanical results — tail-truncate so errors (at end) are visible.
	sb.WriteString("## Mechanical results\n")
	sb.WriteString(fmt.Sprintf("Format: pass=%v\n```\n%s\n```\n", ctx.Mechanical.Format.Pass,
		pipeline.Tail(ctx.Mechanical.Format.Output, 500)))
	sb.WriteString(fmt.Sprintf("Check: pass=%v\n```\n%s\n```\n", ctx.Mechanical.ProjectCheck.Pass,
		pipeline.Tail(ctx.Mechanical.ProjectCheck.Output, 3000)))
	sb.WriteString(fmt.Sprintf("Security: pass=%v\n```\n%s\n```\n\n", ctx.Mechanical.Security.Pass,
		pipeline.Tail(ctx.Mechanical.Security.Output, 500)))

	// Auto-generate findings from mechanical failures (guaranteed detection).
	mechFindings := ctx.mechanicalFindings()

	// Success criteria
	if len(ctx.SuccessCriteria) > 0 {
		sb.WriteString("## Success criteria\n")
		for _, sc := range ctx.SuccessCriteria {
			sb.WriteString(fmt.Sprintf("- %s\n", sc))
		}
		sb.WriteString("\n")
	}

	// Files with plan specs for cross-reference
	if ctx.ReviewFiles != nil {
		sb.WriteString("## Files (incremental — only changed files from last fix)\n\n")
		sb.WriteString("Review the fix pass changes against the original spec.\n\n")
	} else {
		sb.WriteString("## Files\n\n")
		sb.WriteString("Compare each file's implementation against its plan spec. ")
		sb.WriteString("A mismatch between spec and code is an error.\n\n")
	}
	for _, f := range plan.FilesToCreate {
		if ctx.ReviewFiles != nil && !ctx.ReviewFiles[f.Path] {
			continue
		}
		sb.WriteString(fmt.Sprintf("### %s (create)\nSpec: %s\nPurpose: %s\n```\n%s\n```\n\n",
			f.Path, f.Spec, f.Purpose, ctx.fileContent(f.Path)))
	}
	for _, f := range plan.FilesToModify {
		if ctx.ReviewFiles != nil && !ctx.ReviewFiles[f.Path] {
			continue
		}
		sb.WriteString(fmt.Sprintf("### %s (modify)\nSpec: %s\nPurpose: %s\n```\n%s\n```\n\n",
			f.Path, f.Spec, f.Purpose, ctx.fileContent(f.Path)))
	}
	for _, t := range plan.Tests {
		if ctx.ReviewFiles != nil && !ctx.ReviewFiles[t.File] {
			continue
		}
		sb.WriteString(fmt.Sprintf("### %s (test)\nStrategy: %s\n```\n%s\n```\n\n",
			t.File, t.Strategy, ctx.fileContent(t.File)))
	}

	resp, err := driver.Call(sb.String())
	if err != nil {
		return Result{}, err
	}

	rr, err := parseReview(resp.Text)
	if err != nil {
		return Result{}, fmt.Errorf("parsing reviewer response: %w\nraw:\n%s", err, resp.Text)
	}
	rr.Mechanical = ctx.Mechanical

	// Merge mechanical findings (guaranteed) with LLM findings.
	rr.Findings = append(mechFindings, rr.Findings...)

	return Result{Review: rr, Metrics: resp.Metrics}, nil
}

func (c Context) mechanicalFindings() []Finding {
	var fs []Finding
	if !c.Mechanical.Format.Pass {
		fs = append(fs, Finding{
			Severity: "error", File: "(format)",
			Problem: "Format check failed", FixWhere: "run format command",
			FixWhat: c.Mechanical.Format.Output,
		})
	}
	if !c.Mechanical.ProjectCheck.Pass {
		fixWhat := c.Mechanical.ProjectCheck.Output
		if len(c.CheckFilter) > 0 {
			fixWhat = shared.ExtractErrorLines(fixWhat, c.CheckFilter)
		}
		fs = append(fs, Finding{
			Severity: "error", File: "(build)",
			Problem: "Project check failed", FixWhere: "fix compilation/test errors",
			FixWhat: fixWhat,
		})
	}
	if !c.Mechanical.Security.Pass {
		fs = append(fs, Finding{
			Severity: "error", File: "(security)",
			Problem: "Security check failed", FixWhere: "fix security vulnerabilities",
			FixWhat: c.Mechanical.Security.Output,
		})
	}
	return fs
}

func (c Context) fileContent(path string) string {
	if c.Files != nil {
		if content, ok := c.Files[path]; ok {
			return shared.TrimLines(content, 200)
		}
	}
	return shared.TrimLines(shared.ReadFile(c.Root, path), 200)
}

// FindingsFixText formats findings as a fix prompt for the executor.
func (rr ReviewResult) FindingsFixText() string {
	if len(rr.Findings) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, f := range rr.Findings {
		sb.WriteString(fmt.Sprintf("## %s: %s\nFile: %s\n\n",
			strings.ToUpper(f.Severity), f.Problem, f.File))
		sb.WriteString(fmt.Sprintf("Fix where: %s\n", f.FixWhere))
		if len(f.FixWhat) > 0 {
			sb.WriteString(fmt.Sprintf("\n```\n%s\n```\n\n", pipeline.Tail(f.FixWhat, 3000)))
		}
	}
	return sb.String()
}

// HasErrors returns true if any finding has severity "error".
func (rr ReviewResult) HasErrors() bool {
	for _, f := range rr.Findings {
		if f.Severity == "error" {
			return true
		}
	}
	return false
}

func parseReview(raw string) (ReviewResult, error) {
	raw = shared.ExtractMarkdownJSON(raw)
	var rr ReviewResult
	if err := json.Unmarshal([]byte(raw), &rr); err != nil {
		return ReviewResult{}, fmt.Errorf("json: %w", err)
	}
	return rr, nil
}


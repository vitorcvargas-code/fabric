package augmenter

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/shared"
)

// Question is one clarifying question with checkbox options.
type Question struct {
	ID          string      `json:"id"`
	Question    string      `json:"question"`
	Text        string      `json:"text"` // alias for "question"
	MultiSelect bool        `json:"multi_select"`
	Options     flexOptions `json:"options"`
}

func (q Question) Title() string {
	if q.Question != "" {
		return q.Question
	}
	return q.Text
}

// flexOptions accepts both [{label, description}] objects and plain strings.
type flexOptions []Option

func (f *flexOptions) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for _, r := range raw {
		r = json.RawMessage(strings.TrimSpace(string(r)))
		if len(r) > 0 && r[0] == '{' {
			var o Option
			if err := json.Unmarshal(r, &o); err != nil {
				return err
			}
			*f = append(*f, o)
		} else {
			var s string
			if err := json.Unmarshal(r, &s); err != nil {
				return err
			}
			*f = append(*f, Option{Label: s, Description: s})
		}
	}
	return nil
}

// Option is one checkbox choice.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AnalyzeResult is the LLM's analysis output.
type AnalyzeResult struct {
	HasQuestions    bool       `json:"has_questions"`
	Questions       []Question `json:"questions"`
	SuccessCriteria []string   `json:"success_criteria"`
	SCNote          string     `json:"sc_note"`
}

// Context bundles the inputs the augmenter needs.
type Context struct {
	RawTask       string
	CompressedTask string
	KB            []byte
}

// Analyze runs the augmenter's first LLM call: produces questions + extracted SC.
func Analyze(driver llm.Driver, ctx Context) (AnalyzeResult, llm.Metrics, error) {
	var sb strings.Builder
	sb.WriteString(Persona)
	sb.WriteString(shared.PromptSep)

	if len(ctx.KB) > 0 {
		sb.WriteString("## Project Knowledge Base\n")
		sb.WriteString(string(ctx.KB))
		sb.WriteString(shared.PromptSep)
	}

	sb.WriteString("## User task\n")
	if ctx.CompressedTask != "" {
		sb.WriteString(ctx.CompressedTask)
		sb.WriteString(shared.PromptSep)
	}
	sb.WriteString("## Original task text\n")
	sb.WriteString(ctx.RawTask)

	resp, err := driver.Call(sb.String())
	if err != nil {
		return AnalyzeResult{}, llm.Metrics{}, fmt.Errorf("augmenter analyze: %w", err)
	}

	ar, err := parseAnalyze(resp.Text)
	if err != nil {
		return AnalyzeResult{}, llm.Metrics{}, fmt.Errorf("parsing augmenter response: %w\nraw:\n%s", err, resp.Text)
	}

	return ar, resp.Metrics, nil
}

// Resolve runs the augmenter's second LLM call: synthesizes answers into a crisp task.
func Resolve(driver llm.Driver, rawTask, compressedTask string, questions []Question, answers map[string]string, kb []byte) (string, llm.Metrics, error) {
	var sb strings.Builder
	sb.WriteString(`You are a technical analyst. Synthesize the clarifying answers into a precise,
unambiguous task description for an architect.

## Original task
`)
	if compressedTask != "" {
		sb.WriteString(compressedTask)
	}
	sb.WriteString("\n\n")

	if len(kb) > 0 {
		sb.WriteString("## Project KB (reference only)\n")
		sb.WriteString(string(kb))
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Answers to clarifying questions\n")
	for _, q := range questions {
		answer, ok := answers[q.ID]
		if !ok {
			answer = "(not answered)"
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", q.Title(), answer))
	}
	sb.WriteString("\n")

	if rawTask != "" && rawTask != compressedTask {
		sb.WriteString("## Original raw text (for context only)\n")
		sb.WriteString(rawTask)
		sb.WriteString("\n\n")
	}

	sb.WriteString(`Output ONLY the resolved task as plain text. 1-3 paragraphs. Be concrete:
include types, behaviors, constraints, scope boundaries. This task goes directly
to the architect — no ambiguity should remain.`)

	resp, err := driver.Call(sb.String())
	if err != nil {
		return "", llm.Metrics{}, fmt.Errorf("augmenter resolve: %w", err)
	}

	return strings.TrimSpace(resp.Text), resp.Metrics, nil
}

// PresentQuestions displays questions to the user and collects answers.
func PresentQuestions(questions []Question) map[string]string {
	answers := map[string]string{}

	for _, q := range questions {
		fmt.Fprintf(os.Stderr, "\n  %s\n", q.Title())
		for i, o := range q.Options {
			fmt.Fprintf(os.Stderr, "  [%d] %s — %s\n", i+1, o.Label, o.Description)
		}

		if q.MultiSelect {
			fmt.Fprint(os.Stderr, "  Select (comma-separated, 'skip', or 'other: your answer'): ")
		} else {
			fmt.Fprint(os.Stderr, "  Select (number, 'skip', or 'other: your answer'): ")
		}

		input := strings.TrimSpace(shared.ReadLine())

		if input == "" || strings.ToLower(input) == "skip" || strings.ToLower(input) == "s" {
			continue
		}

		answers[q.ID] = resolveAnswer(q.Options, input)
	}

	fmt.Fprintln(os.Stderr)
	return answers
}

func resolveAnswer(options []Option, input string) string {
	if strings.HasPrefix(input, "other:") || strings.HasPrefix(input, "Other:") {
		return strings.TrimSpace(input[6:])
	}

	// Try comma-separated numbers for multi-select
	parts := strings.Split(input, ",")
	var labels []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		idx := -1
		fmt.Sscanf(p, "%d", &idx)
		if idx >= 1 && idx <= len(options) {
			if options[idx-1].Label == "Other" {
				fmt.Fprint(os.Stderr, "  Specify: ")
				labels = append(labels, strings.TrimSpace(shared.ReadLine()))
			} else {
				labels = append(labels, options[idx-1].Label)
			}
		}
	}
	if len(labels) > 0 {
		return strings.Join(labels, ", ")
	}
	return input
}

// ConfirmSuccessCriteria presents SC to the user and returns the confirmed list.
func ConfirmSuccessCriteria(criteria []string, scNote string) []string {
	fmt.Fprintf(os.Stderr, "\n── success criteria ───────────────────────────\n")
	if len(criteria) == 0 {
		fmt.Fprintf(os.Stderr, "  No criteria could be extracted from your task.\n")
		fmt.Fprintf(os.Stderr, "  Provide at least one: what must be true for this to be done?\n")
		return inputCriteria()
	}

	if scNote != "" {
		label := scNote
		switch scNote {
		case "from acceptance criteria":
			label = "from acceptance criteria (copied verbatim)"
		case "extracted from description":
			label = "extracted from task description (please verify)"
		}
		fmt.Fprintf(os.Stderr, "  %s\n\n", label)
	}

	for i, sc := range criteria {
		fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, sc)
	}

	fmt.Fprintf(os.Stderr, "\n  [y] Confirm  [e] Edit  [n] I'll specify my own\n")
	fmt.Fprint(os.Stderr, "  Select: ")

	input := strings.TrimSpace(strings.ToLower(shared.ReadLine()))

	switch input {
	case "e", "edit":
		fmt.Fprintf(os.Stderr, "  Enter criteria one per line. Press Enter on an empty line to finish.\n")
		return inputCriteria()
	case "n", "no":
		fmt.Fprintf(os.Stderr, "  Enter criteria one per line. Press Enter on an empty line to finish.\n")
		return inputCriteria()
	default:
		return criteria
	}
}

func inputCriteria() []string {
	var criteria []string
	i := 1
	for {
		if len(criteria) == 0 {
			fmt.Fprintf(os.Stderr, "  [%d] ", i)
		} else {
			fmt.Fprintf(os.Stderr, "  [%d] (or empty to finish) ", i)
		}
		line := strings.TrimSpace(shared.ReadLine())
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		criteria = append(criteria, line)
		i++
	}
	if len(criteria) == 0 {
		fmt.Fprintf(os.Stderr, "  No criteria provided. Continuing without.\n")
	}
	return criteria
}

// parseAnalyze extracts AnalyzeResult from the LLM's raw JSON response.
func parseAnalyze(raw string) (AnalyzeResult, error) {
	raw = shared.ExtractMarkdownJSON(raw)
	var ar AnalyzeResult
	if err := json.Unmarshal([]byte(raw), &ar); err != nil {
		return AnalyzeResult{}, fmt.Errorf("json: %w", err)
	}
	return ar, nil
}

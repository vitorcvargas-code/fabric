package buildfix

import (
	"fmt"
	"strings"

	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/persona"
	"github.com/vitorcvargas-code/fabric/internal/shared"
)

type FixOptions struct {
	KB []byte
}

type FixResult struct {
	Files   map[string]string
	Metrics llm.Metrics
}

func ExecuteFix(driver llm.Driver, buildOutput string, opts FixOptions) (FixResult, error) {
	var body strings.Builder
	body.WriteString("## Build output with errors\n\n```\n")
	body.WriteString(buildOutput)
	if len(buildOutput) > 0 && buildOutput[len(buildOutput)-1] != '\n' {
		body.WriteString("\n")
	}
	body.WriteString("```\n\n")
	return runFix(driver, body.String(), opts)
}

func ExecuteFindingsFix(driver llm.Driver, root string, findingsByFile map[string][]string, opts FixOptions) (FixResult, error) {
	fence := "```"
	var body strings.Builder
	body.WriteString("## Findings to fix\n\n")
	for path, findings := range findingsByFile {
		body.WriteString(fmt.Sprintf("### %s\n%s\n", path, fence))
		for _, f := range findings {
			body.WriteString(f)
			body.WriteString("\n")
		}
		body.WriteString(fence)
		body.WriteString("\n\n")
		content := shared.ReadFile(root, path)
		if !strings.HasPrefix(content, "(unavailable") {
			body.WriteString(fmt.Sprintf("Current content:\n%s\n%s\n%s\n\n", fence, content, fence))
		}
	}
	return runFix(driver, body.String(), opts)
}

func runFix(driver llm.Driver, body string, opts FixOptions) (FixResult, error) {
	var sb strings.Builder
	sb.WriteString(persona.FixerPrompt)
	sb.WriteString("\n\n")

	if len(opts.KB) > 0 {
		sb.WriteString("## Project context (knowledge base)\n\n")
		sb.WriteString(string(opts.KB))
		sb.WriteString("\n\n")
	}

	sb.WriteString(body)
	sb.WriteString("Fix the reported issues. Output each changed file as:\n=== FILE: path/to/file ===\n```\ncontent\n```\n\n")

	resp, err := driver.Call(sb.String())
	if err != nil {
		return FixResult{}, err
	}
	files := shared.ParseMultiFile(resp.Text)
	if len(files) == 0 {
		return FixResult{}, fmt.Errorf("executor fix: LLM returned no files")
	}
	return FixResult{Files: files, Metrics: resp.Metrics}, nil
}

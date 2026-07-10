package tester

import (
	"fmt"
	"os"
	"strings"

	"github.com/vitorcvargas-code/fabric/internal/architect"
	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/shared"
)

// Result holds the output of one tester call.
type Result struct {
	Files   map[string]string
	Metrics llm.Metrics
}

// Context bundles project-level inputs for tester prompts.
type Context struct {
	Root string
	KB   []byte
}

func (c Context) kbBlock() string {
	if len(c.KB) > 0 {
		return string(c.KB)
	}
	return ""
}

// MaxTestsPerBatch limits how many tests one LLM call produces.
const MaxTestsPerBatch = 2

// RunBatch generates a single batch of test files.
func RunBatch(driver llm.Driver, allTests []architect.TestEntry, batch []architect.TestEntry, kbBlock string) (map[string]string, llm.Metrics, error) {
	var sb strings.Builder
	sb.WriteString(Persona)
	sb.WriteString("\n\n---\n\n")
	sb.WriteString(kbBlock)
	sb.WriteString("\n\n---\n\n")

	// List all tests, marking this batch's tests.
	sb.WriteString("## All tests in this plan\n\n")
	batchSet := map[string]bool{}
	for _, t := range batch {
		batchSet[t.File] = true
	}
	for _, t := range allTests {
		marker := ""
		if batchSet[t.File] {
			marker = " ← YOU MUST OUTPUT THIS FILE"
		}
		sb.WriteString(fmt.Sprintf("- %s: %s%s\n", t.File, t.Strategy, marker))
	}
	sb.WriteString("\n---\n\n")

	sb.WriteString(fmt.Sprintf("## Tests to output (produce exactly %d files)\n\n", len(batch)))
	for _, t := range batch {
		sb.WriteString(fmt.Sprintf("## TEST: %s\nStrategy: %s\n\n", t.File, t.Strategy))
	}

	resp, err := driver.Call(sb.String())
	if err != nil {
		return nil, llm.Metrics{}, fmt.Errorf("tester batch: %w", err)
	}

	batchFiles := shared.ParseMultiFile(resp.Text)
	if len(batchFiles) == 0 {
		maxLen := min(2000, len(resp.Text))
		fmt.Fprintf(os.Stderr, "tester batch: raw response (first %d chars):\n%s\n",
			maxLen, resp.Text[:maxLen])
		return nil, llm.Metrics{}, fmt.Errorf("LLM returned no files")
	}

	return batchFiles, resp.Metrics, nil
}

// RunBatchesSession runs multiple test batches reusing a single LLM session.
func RunBatchesSession(session *llm.Session, allTests []architect.TestEntry, batches [][]architect.TestEntry, kbBlock string) ([]map[string]string, llm.Metrics, error) {
	var allFiles []map[string]string
	var merged llm.Metrics

	for i, batch := range batches {
		prompt := buildTestBatchPrompt(allTests, batch, kbBlock)
		var resp llm.Response
		var err error

		if i == 0 {
			resp, err = session.CallBootstrap(prompt)
		} else {
			resp, err = session.CallResume(prompt)
		}
		if err != nil {
			return nil, llm.Metrics{}, fmt.Errorf("tester batch %d: %w", i+1, err)
		}

		batchFiles := shared.ParseMultiFile(resp.Text)
		if len(batchFiles) == 0 {
			return nil, llm.Metrics{}, fmt.Errorf("tester batch %d: LLM returned no files", i+1)
		}
		allFiles = append(allFiles, batchFiles)
		if merged.Calls == 0 {
			merged = resp.Metrics
		} else {
			merged = llm.Merge(merged, resp.Metrics)
		}
	}
	return allFiles, merged, nil
}

// RunSessionBatch runs a single test batch within an existing session.
func RunSessionBatch(session *llm.Session, first bool, allTests []architect.TestEntry, batch []architect.TestEntry, kbBlock string) (map[string]string, llm.Metrics, error) {
	prompt := buildTestBatchPrompt(allTests, batch, kbBlock)
	var resp llm.Response
	var err error
	if first {
		resp, err = session.CallBootstrap(prompt)
	} else {
		resp, err = session.CallResume(prompt)
	}
	if err != nil {
		return nil, llm.Metrics{}, err
	}
	batchFiles := shared.ParseMultiFile(resp.Text)
	if len(batchFiles) == 0 {
		return nil, llm.Metrics{}, fmt.Errorf("LLM returned no files")
	}
	return batchFiles, resp.Metrics, nil
}

func buildTestBatchPrompt(allTests []architect.TestEntry, batch []architect.TestEntry, kbBlock string) string {
	var sb strings.Builder
	sb.WriteString(Persona)
	sb.WriteString("\n\n---\n\n")
	sb.WriteString(kbBlock)
	sb.WriteString("\n\n---\n\n")

	sb.WriteString("## All tests in this plan\n\n")
	batchSet := map[string]bool{}
	for _, t := range batch {
		batchSet[t.File] = true
	}
	for _, t := range allTests {
		marker := ""
		if batchSet[t.File] {
			marker = " ← YOU MUST OUTPUT THIS FILE"
		}
		sb.WriteString(fmt.Sprintf("- %s: %s%s\n", t.File, t.Strategy, marker))
	}
	sb.WriteString("\n---\n\n")

	sb.WriteString(fmt.Sprintf("## Tests to output (produce exactly %d files)\n\n", len(batch)))
	for _, t := range batch {
		sb.WriteString(fmt.Sprintf("## TEST: %s\nStrategy: %s\n\n", t.File, t.Strategy))
	}
	return sb.String()
}

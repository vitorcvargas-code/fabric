package executor

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/vitorcvargas-code/fabric/internal/architect"
	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/shared"
)

// Result holds the output of one executor call.
type Result struct {
	Files   map[string]string
	Metrics llm.Metrics
}

// ExecContext bundles project-level inputs for executor prompts.
type ExecContext struct {
	Root string
	KB   []byte
}

func (c ExecContext) ContextBlock() string {
	if len(c.KB) > 0 {
		return string(c.KB)
	}
	return ""
}

const MaxFilesPerBatch = 5

type FileEntry struct {
	Action  string
	Path    string
	Follows string
	Needs   []string
	Purpose string
	Spec    string
}

func BuildFileList(plan *architect.Plan) []FileEntry {
	var all []FileEntry
	for _, f := range plan.FilesToCreate {
		all = append(all, FileEntry{
			Action: "CREATE", Path: f.Path, Follows: f.Follows,
			Needs: f.Needs, Purpose: f.Purpose, Spec: f.Spec,
		})
	}
	for _, f := range plan.FilesToModify {
		all = append(all, FileEntry{
			Action: "MODIFY", Path: f.Path,
			Needs: f.Needs, Purpose: f.Purpose, Spec: f.Spec,
		})
	}
	// Sort test files last so production code exists before tests are written.
	isTestPath := func(p string) bool {
		return strings.Contains(p, "src/test/") || strings.Contains(p, "/test/") || strings.HasSuffix(p, "IT.kt") || strings.HasSuffix(p, "Test.kt")
	}
	slices.SortStableFunc(all, func(a, b FileEntry) int {
		aTest, bTest := isTestPath(a.Path), isTestPath(b.Path)
		if aTest != bTest {
			if aTest {
				return 1
			}
			return -1
		}
		return 0
	})
	return all
}

func ExecuteBatch(driver llm.Driver, allEntries []FileEntry, batch []FileEntry, kbBlock string) (map[string]string, llm.Metrics, error) {
	var sb strings.Builder
	sb.WriteString(Persona)
	sb.WriteString("\n\n---\n\n")
	sb.WriteString(kbBlock)
	sb.WriteString("\n\n---\n\n")

	batchSet := map[string]bool{}
	for _, f := range batch {
		batchSet[f.Path] = true
	}
	sb.WriteString("## All files in this plan\n\n")
	for _, f := range allEntries {
		marker := ""
		if batchSet[f.Path] {
			marker = " ← YOU MUST OUTPUT THIS FILE"
		}
		sb.WriteString(fmt.Sprintf("- %s: %s%s\n", f.Action, f.Path, marker))
	}
	sb.WriteString("\n---\n\n")

	sb.WriteString(fmt.Sprintf("## Files to output (produce exactly %d files)\n\n", len(batch)))
	for _, f := range batch {
		sb.WriteString(fmt.Sprintf("### %s: %s\n", f.Action, f.Path))
		if f.Follows != "" {
			sb.WriteString(fmt.Sprintf("Follows: %s\n", f.Follows))
		}
		if len(f.Needs) > 0 {
			sb.WriteString(fmt.Sprintf("Needs: %s\n", strings.Join(f.Needs, ", ")))
		}
		sb.WriteString(fmt.Sprintf("Purpose: %s\n", f.Purpose))
		sb.WriteString(fmt.Sprintf("Spec: %s\n\n", f.Spec))
	}

	resp, err := driver.Call(sb.String())
	if err != nil {
		return nil, llm.Metrics{}, fmt.Errorf("executor batch: %w", err)
	}

	batchFiles := shared.ParseMultiFile(resp.Text)
	if len(batchFiles) == 0 {
		maxLen := min(2000, len(resp.Text))
		fmt.Fprintf(os.Stderr, "executor batch: raw response (first %d chars):\n%s\n",
			maxLen, resp.Text[:maxLen])
		return nil, llm.Metrics{}, fmt.Errorf("LLM returned no files")
	}
	return batchFiles, resp.Metrics, nil
}

func ExecuteSessionBatch(session *llm.Session, first bool, allEntries []FileEntry, batch []FileEntry, kbBlock string) (map[string]string, llm.Metrics, error) {
	prompt := buildBatchPrompt(allEntries, batch, kbBlock, !first)
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

func ExecuteBatchesSession(session *llm.Session, allEntries []FileEntry, batches [][]FileEntry, kbBlock string) ([]map[string]string, llm.Metrics, error) {
	var allFiles []map[string]string
	var merged llm.Metrics

	for i, batch := range batches {
		prompt := buildBatchPrompt(allEntries, batch, kbBlock, i > 0)
		var resp llm.Response
		var err error

		if i == 0 {
			resp, err = session.CallBootstrap(prompt)
		} else {
			resp, err = session.CallResume(prompt)
		}
		if err != nil {
			return nil, llm.Metrics{}, fmt.Errorf("executor batch %d: %w", i+1, err)
		}

		batchFiles := shared.ParseMultiFile(resp.Text)
		if len(batchFiles) == 0 {
			return nil, llm.Metrics{}, fmt.Errorf("executor batch %d: LLM returned no files", i+1)
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

func buildBatchPrompt(allEntries []FileEntry, batch []FileEntry, kbBlock string, compact bool) string {
	var sb strings.Builder

	sb.WriteString(Persona)
	sb.WriteString("\n\n---\n\n")
	if !compact {
		// Session resume: KB already cached from bootstrap.
		// Keep Persona — it contains output format rules the model needs every call.
		sb.WriteString(kbBlock)
		sb.WriteString("\n\n---\n\n")
	}

	batchSet := map[string]bool{}
	for _, f := range batch {
		batchSet[f.Path] = true
	}

	if compact {
		sb.WriteString("## Other files in this plan (for import context only — do NOT output)\n\n")
		for _, f := range allEntries {
			if batchSet[f.Path] {
				continue
			}
			sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", f.Path, f.Action, f.Purpose))
		}
		if len(allEntries) == len(batch) {
			sb.WriteString("(none — this batch covers all remaining files)\n")
		}
	} else {
		sb.WriteString("## All files in this plan\n\n")
		for _, f := range allEntries {
			marker := ""
			if batchSet[f.Path] {
				marker = " ← YOU MUST OUTPUT THIS FILE"
			}
			sb.WriteString(fmt.Sprintf("- %s: %s%s\n", f.Action, f.Path, marker))
		}
	}
	sb.WriteString("\n---\n\n")

	sb.WriteString(fmt.Sprintf("## Files to output (produce exactly %d files)\n\n", len(batch)))
	for _, f := range batch {
		sb.WriteString(fmt.Sprintf("### %s: %s\n", f.Action, f.Path))
		if f.Follows != "" {
			sb.WriteString(fmt.Sprintf("Follows: %s\n", f.Follows))
		}
		if len(f.Needs) > 0 {
			sb.WriteString(fmt.Sprintf("Needs: %s\n", strings.Join(f.Needs, ", ")))
		}
		sb.WriteString(fmt.Sprintf("Purpose: %s\n", f.Purpose))
		sb.WriteString(fmt.Sprintf("Spec: %s\n\n", f.Spec))
	}
	return sb.String()
}

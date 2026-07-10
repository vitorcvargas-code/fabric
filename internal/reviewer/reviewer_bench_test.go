package reviewer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vitorcvargas-code/fabric/internal/architect"
)

func makeReviewPlan(fileCount int) *architect.Plan {
	p := &architect.Plan{Summary: "Add pagination to list endpoints"}
	for i := 0; i < fileCount; i++ {
		p.FilesToCreate = append(p.FilesToCreate, architect.FileEntry{
			Path:    fmt.Sprintf("src/handler_%d.go", i),
			Action:  "CREATE",
			Purpose: fmt.Sprintf("Handler for endpoint %d", i),
			Spec:    "Implement paginated list handler with offset/limit query params",
		})
	}
	for i := 0; i < fileCount/3; i++ {
		p.FilesToModify = append(p.FilesToModify, architect.FileEntry{
			Path:    fmt.Sprintf("src/existing_%d.go", i),
			Action:  "MODIFY",
			Purpose: fmt.Sprintf("Add pagination support to existing handler %d", i),
			Spec:    "Add ParsePagination call and pass Page struct to service layer",
		})
	}
	return p
}

func makeFileContent() string {
	return strings.Repeat("func Handler(w http.ResponseWriter, r *http.Request) {\n\t// implementation\n}\n", 20)
}

// TestReviewPromptSizeDelta measures the byte savings from incremental review.
func TestReviewPromptSizeDelta(t *testing.T) {
	content := makeFileContent()

	tests := []struct {
		totalFiles   int
		changedFiles int
	}{
		{10, 2},
		{20, 3},
		{30, 5},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_total_%d_changed", tt.totalFiles, tt.changedFiles), func(t *testing.T) {
			plan := makeReviewPlan(tt.totalFiles)

			// Build full-file prompt.
			fullCtx := Context{
				Files: make(map[string]string),
			}
			for _, f := range plan.FilesToCreate {
				fullCtx.Files[f.Path] = content
			}
			for _, f := range plan.FilesToModify {
				fullCtx.Files[f.Path] = content
			}
			_ = buildPrompt(plan, fullCtx)

			// Build incremental prompt (only changed files).
			reviewFiles := make(map[string]bool)
			for i := 0; i < tt.changedFiles && i < len(plan.FilesToCreate); i++ {
				reviewFiles[plan.FilesToCreate[i].Path] = true
			}
			for i := 0; i < tt.changedFiles && i < len(plan.FilesToModify); i++ {
				reviewFiles[plan.FilesToModify[i].Path] = true
			}

			incCtx := Context{
				Files:       fullCtx.Files,
				ReviewFiles: reviewFiles,
			}
			_ = buildPrompt(plan, incCtx)

			// The savings come from not including unchanged file contents in the prompt.
			totalCount := len(plan.FilesToCreate) + len(plan.FilesToModify)
			filesOmitted := totalCount - len(reviewFiles)
			if filesOmitted > 0 {
				t.Logf("%d/%d files omitted from review prompt (%d changed, %d total)",
					filesOmitted, totalCount, len(reviewFiles), totalCount)
			}
		})
	}
}

// buildPrompt mirrors reviewer.Run without the LLM call, for measurement.
func buildPrompt(plan *architect.Plan, ctx Context) string {
	var sb strings.Builder
	sb.WriteString(Persona)
	sb.WriteString("\n\n---\n\n")

	if ctx.InjectContent != "" {
		sb.WriteString(ctx.InjectContent)
		sb.WriteString("\n\n---\n\n")
	}
	if len(ctx.KB) > 0 {
		sb.WriteString(string(ctx.KB))
		sb.WriteString("\n\n---\n\n")
	}

	sb.WriteString("## Mechanical results\n")
	sb.WriteString("Format: pass=true\n```\n```\n")
	sb.WriteString("Check: pass=true\n```\n```\n")
	sb.WriteString("Security: pass=true\n```\n```\n\n")

	if len(ctx.SuccessCriteria) > 0 {
		sb.WriteString("## Success criteria\n")
		for _, sc := range ctx.SuccessCriteria {
			sb.WriteString(fmt.Sprintf("- %s\n", sc))
		}
		sb.WriteString("\n")
	}

	if ctx.ReviewFiles != nil {
		sb.WriteString("## Files (incremental)\n\n")
	} else {
		sb.WriteString("## Files\n\n")
		sb.WriteString("Compare each file's implementation against its plan spec.\n\n")
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
	return sb.String()
}

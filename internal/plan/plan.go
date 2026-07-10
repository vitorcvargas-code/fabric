package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vitorcvargas-code/fabric/internal/architect"
)

// ValidationIssue is one problem found during plan validation.
type ValidationIssue struct {
	Severity string // "error" or "warning"
	File     string
	Message  string
}

// ValidateFields checks that every file entry has the mandatory fields
// (action, follows, needs). Returns issues for missing or empty fields.
func ValidateFields(p *architect.Plan) []ValidationIssue {
	var issues []ValidationIssue
	for _, f := range p.FilesToCreate {
		if f.Action == "" {
			issues = append(issues, ValidationIssue{
				Severity: "error",
				File:     f.Path,
				Message:  "missing required field 'action' (must be \"create\" or \"modify\")",
			})
		}
		if f.Follows == "" {
			issues = append(issues, ValidationIssue{
				Severity: "error",
				File:     f.Path,
				Message:  "missing required field 'follows' (exemplar path from KB §3 or §6)",
			})
		}
		if len(f.Needs) == 0 {
			issues = append(issues, ValidationIssue{
				Severity: "error",
				File:     f.Path,
				Message:  "missing required field 'needs' (type names this file imports, or [\"none\"])",
			})
		}
	}
	for _, f := range p.FilesToModify {
		if f.Action == "" {
			issues = append(issues, ValidationIssue{
				Severity: "error",
				File:     f.Path,
				Message:  "missing required field 'action' (must be \"create\" or \"modify\")",
			})
		}
		if f.Follows == "" {
			issues = append(issues, ValidationIssue{
				Severity: "error",
				File:     f.Path,
				Message:  "missing required field 'follows' (exemplar path from KB §3 or §6)",
			})
		}
		if len(f.Needs) == 0 {
			issues = append(issues, ValidationIssue{
				Severity: "error",
				File:     f.Path,
				Message:  "missing required field 'needs' (type names this file imports, or [\"none\"])",
			})
		}
	}
	return issues
}

// ValidatePaths checks that all file paths in the plan resolve against the
// project root. Create parent dirs must exist. Modify targets must exist.
// Follows paths must exist. Test paths must be under a test directory.
func ValidatePaths(p *architect.Plan, root string) []ValidationIssue {
	var issues []ValidationIssue

	for _, f := range p.FilesToCreate {
		dir := filepath.Dir(filepath.Join(root, f.Path))
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			parent := filepath.Dir(dir)
			if _, err := os.Stat(parent); os.IsNotExist(err) {
				issues = append(issues, ValidationIssue{
					Severity: "error",
					File:     f.Path,
					Message:  fmt.Sprintf("parent directory %s does not exist", filepath.Dir(f.Path)),
				})
			}
		}
		if f.Follows != "" {
			if _, err := os.Stat(filepath.Join(root, f.Follows)); os.IsNotExist(err) {
				issues = append(issues, ValidationIssue{
					Severity: "error",
					File:     f.Path,
					Message:  fmt.Sprintf("follows exemplar %s does not exist", f.Follows),
				})
			}
		}
	}

	for _, f := range p.FilesToModify {
		fullPath := filepath.Join(root, f.Path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			issues = append(issues, ValidationIssue{
				Severity: "error",
				File:     f.Path,
				Message:  "file does not exist — cannot modify a non-existent file",
			})
		}
	}

	for _, t := range p.Tests {
		if !isTestPath(t.File) {
			issues = append(issues, ValidationIssue{
				Severity: "error",
				File:     t.File,
				Message:  "test path must be under a test directory",
			})
		}
	}

	if len(p.FilesToCreate) > 0 && len(p.Tests) == 0 {
		issues = append(issues, ValidationIssue{
			Severity: "warning",
			Message:  "plan creates files but has no tests",
		})
	}

	return issues
}

func isTestPath(p string) bool {
	if strings.Contains(p, "/test/") {
		return true
	}
	if strings.HasSuffix(p, "_test.go") {
		return true
	}
	if strings.HasSuffix(p, "Test.kt") || strings.HasSuffix(p, "IT.kt") || strings.HasSuffix(p, "Tests.kt") {
		return true
	}
	if strings.HasSuffix(p, "Test.java") || strings.HasSuffix(p, "IT.java") || strings.HasSuffix(p, "Tests.java") {
		return true
	}
	return false
}

// HasErrors returns true if any issue has severity "error".
func HasErrors(issues []ValidationIssue) bool {
	for _, i := range issues {
		if i.Severity == "error" {
			return true
		}
	}
	return false
}

// ErrorCount returns the number of error-severity issues.
func ErrorCount(issues []ValidationIssue) int {
	n := 0
	for _, i := range issues {
		if i.Severity == "error" {
			n++
		}
	}
	return n
}

// WarningCount returns the number of warning-severity issues.
func WarningCount(issues []ValidationIssue) int {
	n := 0
	for _, i := range issues {
		if i.Severity == "warning" {
			n++
		}
	}
	return n
}

// Format formats validation issues for display.
func Format(issues []ValidationIssue) string {
	if len(issues) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n── plan validation ────────────────────────\n")
	for _, issue := range issues {
		prefix := "⚠"
		if issue.Severity == "error" {
			prefix = "✗"
		}
		if issue.File != "" {
			sb.WriteString(fmt.Sprintf("  %s %s: %s\n", prefix, issue.File, issue.Message))
		} else {
			sb.WriteString(fmt.Sprintf("  %s %s\n", prefix, issue.Message))
		}
	}
	sb.WriteString(fmt.Sprintf("  %d error(s), %d warning(s)\n", ErrorCount(issues), WarningCount(issues)))
	sb.WriteString("───────────────────────────────────────────\n")
	return sb.String()
}

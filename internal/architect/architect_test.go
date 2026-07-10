package architect

import (
	"strings"
	"testing"

	"github.com/vitorcvargas-code/fabric/internal/shared"
)

func TestExtractJSON_fenced(t *testing.T) {
	raw := "```json\n{\"summary\": \"test\"}\n```"
	got := shared.ExtractMarkdownJSON(raw)
	if got != `{"summary": "test"}` {
		t.Errorf("got %q, want %q", got, `{"summary": "test"}`)
	}
}

func TestExtractJSON_unfenced(t *testing.T) {
	raw := `{"summary": "test"}`
	got := shared.ExtractMarkdownJSON(raw)
	if got != raw {
		t.Errorf("got %q, want %q", got, raw)
	}
}

func TestExtractJSON_textBeforeJSON(t *testing.T) {
	raw := "some text before\n{\"summary\": \"test\"}\nsome text after"
	got := shared.ExtractMarkdownJSON(raw)
	if got != `{"summary": "test"}` {
		t.Errorf("got %q", got)
	}
}

func TestExtractJSON_genericFence(t *testing.T) {
	raw := "```\n{\"summary\": \"test\"}\n```"
	got := shared.ExtractMarkdownJSON(raw)
	if got != `{"summary": "test"}` {
		t.Errorf("got %q", got)
	}
}

func TestExtractJSON_empty(t *testing.T) {
	got := shared.ExtractMarkdownJSON("")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractJSON_nestedObject(t *testing.T) {
	raw := "```json\n{\"summary\": \"x\", \"files_to_create\": [{\"path\": \"a.kt\", \"action\": \"create\"}]}\n```"
	got := shared.ExtractMarkdownJSON(raw)
	if !strings.HasPrefix(got, "{") || !strings.HasSuffix(got, "}") {
		t.Errorf("not a valid JSON object: %q", got)
	}
}

func TestParse_valid(t *testing.T) {
	raw := `{"summary": "add health endpoint"}`
	p, err := parse(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if p.Summary != "add health endpoint" {
		t.Errorf("summary = %q", p.Summary)
	}
}

func TestParse_missingSummary(t *testing.T) {
	_, err := parse(`{"files_to_create": []}`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParse_dollarEscape(t *testing.T) {
	raw := `{"summary": "fix $PATH variable"}`
	p, err := parse(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if p.Summary != "fix $PATH variable" {
		t.Errorf("summary = %q", p.Summary)
	}
}

func TestParse_backslashDollar(t *testing.T) {
	// \\$ is valid JSON (escaped backslash \\ + literal $).
	// The fix must not corrupt it into \$ (invalid JSON).
	raw := `{"summary": "test", "spec": "value=\"\\${holder-platform-client.name}\""}`
	p, err := parse(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if p.Summary != "test" {
		t.Errorf("summary = %q", p.Summary)
	}
}

func TestParse_fenced(t *testing.T) {
	raw := "```json\n{\"summary\": \"test\"}\n```"
	p, err := parse(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if p.Summary != "test" {
		t.Errorf("summary = %q", p.Summary)
	}
}

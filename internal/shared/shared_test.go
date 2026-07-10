package shared

import (
	"testing"
)

func TestParseMultiFile_single(t *testing.T) {
	raw := "=== FILE: foo.ext ===\nx = 1\n"
	files := ParseMultiFile(raw)
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files["foo.ext"] != "x = 1" {
		t.Fatalf("got %q, want %q", files["foo.ext"], "x = 1")
	}
}

func TestParseMultiFile_fenced(t *testing.T) {
	raw := "=== FILE: foo.ext ===\n```\ntype Foo struct { X int }\n```\n"
	files := ParseMultiFile(raw)
	if files["foo.ext"] != "type Foo struct { X int }" {
		t.Fatalf("got %q", files["foo.ext"])
	}
}

func TestParseMultiFile_fencedWithLangTag(t *testing.T) {
	raw := "=== FILE: Foo.kt ===\n```kotlin\npackage com.example\n\ndata class Foo(val x: Int)\n```\n"
	files := ParseMultiFile(raw)
	want := "package com.example\n\ndata class Foo(val x: Int)"
	if files["Foo.kt"] != want {
		t.Fatalf("got %q, want %q", files["Foo.kt"], want)
	}
}

func TestParseMultiFile_fencedWithLangTagJava(t *testing.T) {
	raw := "=== FILE: Bar.java ===\n```java\npublic class Bar {}\n```\n"
	files := ParseMultiFile(raw)
	if files["Bar.java"] != "public class Bar {}" {
		t.Fatalf("got %q", files["Bar.java"])
	}
}

func TestParseMultiFile_fencedWithTrailingCommentary(t *testing.T) {
	raw := "=== FILE: foo.ext ===\n```\ntype Foo struct { X int }\n```\n\n## Summary\n\nSome text.\n"
	files := ParseMultiFile(raw)
	if files["foo.ext"] != "type Foo struct { X int }" {
		t.Fatalf("got %q", files["foo.ext"])
	}
}

func TestParseMultiFile_noHeader(t *testing.T) {
	files := ParseMultiFile("just some text")
	if len(files) != 0 {
		t.Fatalf("got %d files, want 0", len(files))
	}
}

func TestParseMultiFile_emptyContent(t *testing.T) {
	raw := "=== FILE: empty.ext ===\n=== FILE: next.ext ===\nnext = 1\n"
	files := ParseMultiFile(raw)
	if _, ok := files["empty.ext"]; ok {
		t.Error("empty.ext should not appear")
	}
	if _, ok := files["next.ext"]; !ok {
		t.Error("next.ext should appear")
	}
}

func TestFenceFor(t *testing.T) {
	if FenceFor("x.sql") != "```sql" {
		t.Error("sql")
	}
	if FenceFor("x.yaml") != "```yaml" {
		t.Error("yaml")
	}
	if FenceFor("x.json") != "```json" {
		t.Error("json")
	}
	if FenceFor("x.properties") != "```" {
		t.Error("properties")
	}
	if FenceFor("x.ext") != "```" {
		t.Error("default")
	}
}

func TestTrimLines(t *testing.T) {
	got := TrimLines("a\nb\nc\nd\ne\n", 3)
	expected := "a\nb\nc\n// ... (truncated)"
	if got != expected {
		t.Errorf("%q != %q", got, expected)
	}
}

func TestTrimLines_under(t *testing.T) {
	got := TrimLines("a\nb\n", 5)
	if got != "a\nb\n" {
		t.Errorf("got %q", got)
	}
}

func TestFilterKBSections_matching(t *testing.T) {
	kb := []byte("## 1. Quick Reference\nstuff\n## 2. Architecture\npaths\n## 3. Feature Anatomy\nchecklist\n## 5. Pattern Catalog\ncode\n## 6. Conventions\nrules\n")
	got := string(FilterKBSections(kb, "2", "5"))
	if !contains(got, "Quick Reference") {
		t.Error("missing section 1 (always included)")
	}
	if !contains(got, "Architecture") {
		t.Error("missing section 2")
	}
	if contains(got, "Feature Anatomy") {
		t.Error("section 3 should be excluded")
	}
	if !contains(got, "Pattern Catalog") {
		t.Error("missing section 5")
	}
	if contains(got, "Conventions") {
		t.Error("section 6 should be excluded (not requested)")
	}
}

func TestFilterKBSections_noMatch(t *testing.T) {
	kb := []byte("## 1. Quick\n## 2. Arch\n")
	got := FilterKBSections(kb, "9")
	if !contains(string(got), "Quick") {
		t.Error("should fall back to full KB when nothing matches")
	}
}

func contains(s, sub string) bool {
	return stringsIndex(s, sub) >= 0
}

func stringsIndex(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

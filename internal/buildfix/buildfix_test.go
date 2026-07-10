package buildfix

import (
	"testing"
)

func TestExtractErrorLines(t *testing.T) {
	output := `> Task :test FAILED

SomeTest > should_do_thing() FAILED
    org.springframework.dao.DataIntegrityViolationException at Foo.kt:42

OtherTest > should_work() FAILED
    java.lang.AssertionError at Bar.kt:53

357 tests completed, 50 failed

BUILD FAILED in 2m
`

	t.Run("single filter", func(t *testing.T) {
		result := ExtractErrorLines(output, []string{`) FAIL`})
		if !containsAll(result, "SomeTest", "OtherTest", "DataIntegrityViolationException") {
			t.Errorf("expected test failure + exception lines in:\n%s", result)
		}
	})

	t.Run("multiple filters", func(t *testing.T) {
		result := ExtractErrorLines(output, []string{`Exception`, `FAILED`})
		if !containsAll(result, "SomeTest", "OtherTest", "DataIntegrityViolationException", "AssertionError") {
			t.Errorf("expected all error lines in:\n%s", result)
		}
	})

	t.Run("empty filters returns full output", func(t *testing.T) {
		result := ExtractErrorLines(output, nil)
		if !contains(result, "BUILD FAILED") {
			t.Errorf("empty filters should return full output:\n%s", result)
		}
	})

	t.Run("no match returns truncated output", func(t *testing.T) {
		result := ExtractErrorLines(output, []string{`NONEXISTENT_PATTERN`})
		if !contains(result, "BUILD FAILED") {
			t.Errorf("no match should return full output:\n%s", result)
		}
	})
}

func TestIsEmptyOrFormatOnly(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want bool
	}{
		{"empty", "", true},
		{"whitespace only", "  \n\t\n  ", true},
		{"has content", "SomeTest FAILED", false},
	}
	for _, tt := range tests {
		got := isEmptyOrFormatOnly(tt.out)
		if got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

package linear

import (
	"strings"
	"testing"
)

func TestParseIdentifier_SplitsTeamAndNumber(t *testing.T) {
	team, number, err := ParseIdentifier("FAB-12")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if team != "FAB" || number != 12 {
		t.Fatalf("got team=%q number=%d, want FAB/12", team, number)
	}
}

func TestParseIdentifier_RejectsMalformed(t *testing.T) {
	for _, id := range []string{"FAB", "FAB-", "-12", "FAB-abc", ""} {
		if _, _, err := ParseIdentifier(id); err == nil {
			t.Errorf("expected error for %q, got none", id)
		}
	}
}

func TestIsReady_TrueWhenOpenAndUnblocked(t *testing.T) {
	issue := Issue{StateType: "unstarted"}
	if !issue.IsReady() {
		t.Fatal("expected an open, unblocked issue to be ready")
	}
}

func TestIsReady_FalseWhenBlockedOrTerminal(t *testing.T) {
	blocked := Issue{StateType: "unstarted", Blockers: []string{"FAB-1"}}
	done := Issue{StateType: "completed"}
	if blocked.IsReady() {
		t.Error("blocked issue should not be ready")
	}
	if done.IsReady() {
		t.Error("completed issue should not be ready")
	}
}

func TestTaskText_IncludesIdentifierTitleAndDescription(t *testing.T) {
	issue := Issue{Identifier: "FAB-12", Title: "Add endpoint", Description: "## How to implement\n- do the thing"}
	text := issue.TaskText()
	for _, want := range []string{"FAB-12", "Add endpoint", "How to implement"} {
		if !strings.Contains(text, want) {
			t.Errorf("TaskText missing %q:\n%s", want, text)
		}
	}
}

package linear

import (
	"strings"
	"testing"
)

func TestIncompleteBlockers_KeepsOnlyOpenBlocksRelations(t *testing.T) {
	var n issueNode
	n.InverseRelations.Nodes = []relationNode{
		newRelation("blocks", "FAB-1", "unstarted"),  // open blocker → counts
		newRelation("blocks", "FAB-2", "completed"),  // done blocker → ignored
		newRelation("related", "FAB-3", "unstarted"), // not a blocker → ignored
	}

	got := incompleteBlockers(n.InverseRelations.Nodes)
	if len(got) != 1 || got[0] != "FAB-1" {
		t.Fatalf("got %v, want [FAB-1]", got)
	}
}

func TestToIssue_ParentBlockedMakesSubtaskNotReady(t *testing.T) {
	var n issueNode
	n.Identifier = "VIT-77"
	n.State.Type = "backlog" // no direct blockers of its own
	n.Parent = &parentNode{Identifier: "VIT-40"}
	n.Parent.InverseRelations.Nodes = []relationNode{
		newRelation("blocks", "VIT-10", "unstarted"), // parent story is blocked
	}

	issue := n.toIssue()
	if issue.IsReady() {
		t.Fatalf("subtask under a blocked parent should not be ready; blockers=%v", issue.Blockers)
	}
	if len(issue.Blockers) != 1 || issue.Blockers[0] != "VIT-40" {
		t.Fatalf("got blockers %v, want [VIT-40]", issue.Blockers)
	}
}

func TestToIssue_ParentNotBlockedKeepsSubtaskReady(t *testing.T) {
	var n issueNode
	n.Identifier = "VIT-77"
	n.State.Type = "backlog"
	n.Parent = &parentNode{Identifier: "VIT-40"}
	n.Parent.InverseRelations.Nodes = []relationNode{
		newRelation("blocks", "VIT-10", "completed"), // parent's blocker is done
	}

	if !n.toIssue().IsReady() {
		t.Fatalf("subtask under an unblocked parent should be ready")
	}
}

func TestDecodeGraphQL_SurfacesAPIErrors(t *testing.T) {
	body := `{"errors":[{"message":"authentication required"}]}`
	var out struct{}
	err := decodeGraphQL(strings.NewReader(body), &out)
	if err == nil || !strings.Contains(err.Error(), "authentication required") {
		t.Fatalf("expected API error surfaced, got %v", err)
	}
}

func TestDecodeGraphQL_DecodesData(t *testing.T) {
	body := `{"data":{"value":42}}`
	var out struct {
		Value int `json:"value"`
	}
	if err := decodeGraphQL(strings.NewReader(body), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Value != 42 {
		t.Fatalf("got %d, want 42", out.Value)
	}
}

func newRelation(relType, id, stateType string) relationNode {
	var r relationNode
	r.Type = relType
	r.Issue.Identifier = id
	r.Issue.State.Type = stateType
	return r
}

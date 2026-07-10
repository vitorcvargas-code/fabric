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

	got := incompleteBlockers(n)
	if len(got) != 1 || got[0] != "FAB-1" {
		t.Fatalf("got %v, want [FAB-1]", got)
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

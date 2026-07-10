package linear

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListReady_PagesThroughAllResults guards the pagination bug: a single
// page would silently drop the oldest issues (the orchestrator's foundation
// roots). The fake server returns two pages and we expect both issues back.
func TestListReady_PagesThroughAllResults(t *testing.T) {
	page1 := `{"data":{"issues":{"pageInfo":{"hasNextPage":true,"endCursor":"CUR"},"nodes":[{"identifier":"VIT-185","state":{"type":"backlog"}}]}}}`
	page2 := `{"data":{"issues":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[{"identifier":"VIT-76","state":{"type":"backlog"}}]}}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"after":"CUR"`) {
			io.WriteString(w, page2)
			return
		}
		io.WriteString(w, page1)
	}))
	defer srv.Close()

	c := &Client{apiKey: "test", endpoint: srv.URL, http: srv.Client()}
	issues, err := c.ListReady("VIT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2 (pagination dropped a page)", len(issues))
	}
	if issues[0].Identifier != "VIT-185" || issues[1].Identifier != "VIT-76" {
		t.Fatalf("got %s,%s — want VIT-185,VIT-76", issues[0].Identifier, issues[1].Identifier)
	}
}

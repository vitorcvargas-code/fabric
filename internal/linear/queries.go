package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// decodeGraphQL unwraps the { data, errors } envelope and decodes data into out.
func decodeGraphQL(r io.Reader, out any) error {
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(r).Decode(&env); err != nil {
		return fmt.Errorf("decoding Linear response: %w", err)
	}
	if len(env.Errors) > 0 {
		return fmt.Errorf("Linear API error: %s", env.Errors[0].Message)
	}
	return json.Unmarshal(env.Data, out)
}

// issueNode mirrors the GraphQL shape we request for an issue.
type issueNode struct {
	Identifier    string `json:"identifier"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	PriorityLabel string `json:"priorityLabel"`
	State         struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"state"`
	InverseRelations struct {
		Nodes []relationNode `json:"nodes"`
	} `json:"inverseRelations"`
}

// relationNode is one "A blocks B"-style relation on an issue.
type relationNode struct {
	Type  string `json:"type"`
	Issue struct {
		Identifier string `json:"identifier"`
		State      struct {
			Type string `json:"type"`
		} `json:"state"`
	} `json:"issue"`
}

func (n issueNode) toIssue() Issue {
	return Issue{
		Identifier: n.Identifier,
		Title:      n.Title,
		Description: n.Description,
		Priority:   n.PriorityLabel,
		StateName:  n.State.Name,
		StateType:  n.State.Type,
		Blockers:   incompleteBlockers(n),
	}
}

// incompleteBlockers returns identifiers of issues that block this one and are
// not yet done. In Linear an "A blocks B" relation appears on B's
// inverseRelations, with the blocker as the relation's `issue`.
func incompleteBlockers(n issueNode) []string {
	var out []string
	for _, r := range n.InverseRelations.Nodes {
		if r.Type != "blocks" {
			continue
		}
		if r.Issue.State.Type == "completed" || r.Issue.State.Type == "canceled" {
			continue
		}
		out = append(out, r.Issue.Identifier)
	}
	return out
}

const issueFields = `
  identifier title description priorityLabel
  state { name type }
  inverseRelations { nodes { type issue { identifier state { type } } } }
`

// FetchIssue loads a single issue by its human identifier (e.g. "FAB-12").
func (c *Client) FetchIssue(identifier string) (Issue, error) {
	team, number, err := ParseIdentifier(identifier)
	if err != nil {
		return Issue{}, err
	}
	gql := `query($team: String!, $number: Float!) {
	  issues(filter: { team: { key: { eq: $team } }, number: { eq: $number } }, first: 1) {
	    nodes {` + issueFields + `}
	  }
	}`
	var data struct {
		Issues struct {
			Nodes []issueNode `json:"nodes"`
		} `json:"issues"`
	}
	if err := c.query(context.Background(), gql, map[string]any{"team": team, "number": number}, &data); err != nil {
		return Issue{}, err
	}
	if len(data.Issues.Nodes) == 0 {
		return Issue{}, fmt.Errorf("issue %s not found", identifier)
	}
	return data.Issues.Nodes[0].toIssue(), nil
}

// ListReady returns the team's non-terminal issues, each tagged with whether
// it is ready to pick up. Callers filter with Issue.IsReady. Pages through the
// whole result — the foundation issues the orchestrator needs are the oldest,
// so a single page would silently drop them.
func (c *Client) ListReady(teamKey string) ([]Issue, error) {
	gql := `query($team: String!, $after: String) {
	  issues(filter: { team: { key: { eq: $team } }, state: { type: { nin: ["completed", "canceled"] } } }, first: 100, after: $after) {
	    pageInfo { hasNextPage endCursor }
	    nodes {` + issueFields + `}
	  }
	}`
	var issues []Issue
	cursor := ""
	for {
		var data struct {
			Issues struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []issueNode `json:"nodes"`
			} `json:"issues"`
		}
		vars := map[string]any{"team": teamKey, "after": nullableCursor(cursor)}
		if err := c.query(context.Background(), gql, vars, &data); err != nil {
			return nil, err
		}
		for _, n := range data.Issues.Nodes {
			issues = append(issues, n.toIssue())
		}
		if !data.Issues.PageInfo.HasNextPage {
			return issues, nil
		}
		cursor = data.Issues.PageInfo.EndCursor
	}
}

// nullableCursor maps the empty first-page cursor to GraphQL null.
func nullableCursor(cursor string) any {
	if cursor == "" {
		return nil
	}
	return cursor
}

// Package linear is fabric's boundary to the Linear issue tracker. It fetches
// issues as plan input and answers "what is ready to work on right now?" — the
// two things the plan pipeline and the orchestrator need from Linear.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const endpoint = "https://api.linear.app/graphql"

// Client talks to Linear's GraphQL API using a personal API key.
type Client struct {
	apiKey   string
	endpoint string
	http     *http.Client
}

// NewClient builds a client from the LINEAR_API_KEY environment variable.
func NewClient() (*Client, error) {
	key := strings.TrimSpace(os.Getenv("LINEAR_API_KEY"))
	if key == "" {
		return nil, fmt.Errorf("LINEAR_API_KEY is not set — create a personal API key at https://linear.app/settings/account/security and export it")
	}
	return &Client{apiKey: key, endpoint: endpoint, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

// Issue is the subset of a Linear issue fabric acts on.
type Issue struct {
	Identifier string
	Title      string
	Description string
	Priority   string
	StateName  string
	StateType  string
	Blockers   []string // identifiers of incomplete issues blocking this one
}

// IsReady reports whether the issue can be picked up now: not terminal and
// not held back by an unfinished blocker.
func (i Issue) IsReady() bool {
	return i.StateType != "completed" && i.StateType != "canceled" && len(i.Blockers) == 0
}

// TaskText renders the issue as the natural-language task the plan pipeline
// consumes. The spec-to-linear description already carries "How to implement"
// and "How to test", so the description is the brief.
func (i Issue) TaskText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: %s\n\n", i.Identifier, i.Title)
	if i.Description != "" {
		b.WriteString(i.Description)
	}
	return b.String()
}

// ParseIdentifier splits a human identifier like "FAB-12" into its team key
// and issue number.
func ParseIdentifier(id string) (team string, number int, err error) {
	cut := strings.LastIndex(id, "-")
	if cut <= 0 || cut == len(id)-1 {
		return "", 0, fmt.Errorf("invalid issue identifier %q — expected TEAM-NUMBER (e.g. FAB-12)", id)
	}
	number, err = strconv.Atoi(id[cut+1:])
	if err != nil {
		return "", 0, fmt.Errorf("invalid issue identifier %q — %q is not a number", id, id[cut+1:])
	}
	return id[:cut], number, nil
}

func (c *Client) query(ctx context.Context, gql string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": gql, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling Linear: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Linear returned HTTP %d", resp.StatusCode)
	}
	return decodeGraphQL(resp.Body, out)
}

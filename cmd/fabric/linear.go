package main

import (
	"fmt"
	"os"

	"github.com/vitorcvargas-code/fabric/internal/linear"
)

func runLinear(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printCommandHelp("linear")
		return nil
	}
	switch args[0] {
	case "list":
		return runLinearList(args[1:])
	case "show":
		return runLinearShow(args[1:])
	default:
		return fmt.Errorf("unknown linear subcommand %q\n\nRun 'fabric help linear' for usage", args[0])
	}
}

func runLinearList(args []string) error {
	team, err := requireTeamFlag(args)
	if err != nil {
		return err
	}
	client, err := linear.NewClient()
	if err != nil {
		return err
	}
	issues, err := client.ListReady(team)
	if err != nil {
		return err
	}

	banner("📋", fmt.Sprintf("linear: %s — %d open", team, len(issues)))
	for _, issue := range issues {
		printIssueLine(issue)
	}
	return nil
}

func runLinearShow(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("linear show requires an issue identifier (e.g. FAB-12)")
	}
	client, err := linear.NewClient()
	if err != nil {
		return err
	}
	issue, err := client.FetchIssue(args[0])
	if err != nil {
		return err
	}
	fmt.Println(issue.TaskText())
	return nil
}

func printIssueLine(issue linear.Issue) {
	mark := cGreen + "✓ ready " + cReset
	if !issue.IsReady() {
		mark = cDim + "✗ blocked" + cReset
	}
	fmt.Fprintf(os.Stderr, "  %s  %s%-10s%s %s\n", mark, cBold, issue.Identifier, cReset, issue.Title)
	if len(issue.Blockers) > 0 {
		fmt.Fprintf(os.Stderr, "            %sblocked by %v%s\n", cDim, issue.Blockers, cReset)
	}
}

func requireTeamFlag(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--team" {
			if i+1 >= len(args) {
				return "", fmt.Errorf("--team requires a team key (e.g. FAB)")
			}
			return args[i+1], nil
		}
	}
	return "", fmt.Errorf("linear list requires --team <KEY> (e.g. --team FAB)")
}

func linearHelp() string {
	return `╭─ linear ─────────────────────────────────────────────────
│
│  Read work from Linear. Requires LINEAR_API_KEY in the
│  environment (personal key from Settings → Security & access).
│
│  fabric linear list --team FAB     Show open issues, ready vs blocked
│  fabric linear show FAB-12         Print an issue as task text
│
│  Feed an issue straight into planning:
│    fabric plan --linear FAB-12 --out plan.json
│
│  An issue is "ready" when it is not done/canceled and has no
│  unfinished blocker (blockedBy relation). This is the query the
│  orchestrator uses to pick parallel-safe work.
│
╰───────────────────────────────────────────────────────────`
}

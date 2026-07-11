package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vitorcvargas-code/fabric/internal/buildfix"
	"github.com/vitorcvargas-code/fabric/internal/persona"
	"github.com/vitorcvargas-code/fabric/internal/pipeline"
	"github.com/vitorcvargas-code/fabric/internal/reviewer"
	"github.com/vitorcvargas-code/fabric/internal/shared"
)

const maxFixIterations = 3

type reviewState struct {
	Pass     int                `json:"pass"`
	Findings []reviewer.Finding `json:"findings"`
	Files    map[string]string  `json:"files"`
}

func runReview(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		printCommandHelp("review")
		return nil
	}
	opts, err := parseReviewFlags(args)
	if err != nil {
		return err
	}

	cfg, err := loadConfig(opts.configPath)
	if err != nil {
		return err
	}

	ctx, err := pipeline.NewContext(cfg.cfg, opts.dryRun, cfg.cfg.Command.UseWorktree)
	if err != nil {
		return err
	}
	defer ctx.Close()

	// Load plan from the original repo (plan files are user-provided, not
	// synced into worktree by default).
	p, kb, err := loadPlan(filepath.Join(ctx.Orig, opts.planFile))
	if err != nil {
		return err
	}

	banner("🔍", "fabric review")
	if err := logLLMSetup(cfg, opts.modelOverride); err != nil {
		return err
	}

	// ── Resume detection ─────────────────────────────────────────────
	statePath := reviewStatePath(opts.planFile)
	var state *reviewState
	if s, resumed := tryResume(statePath); resumed {
		state = s
	}

	totalStart := time.Now()
	var totalIn, totalOut int
	var totalCost float64
	fileContents := map[string]string{}
	if state != nil {
		mergeMaps(fileContents, state.Files)
	}

	reviewDrv, err := ctx.NewDriver(*persona.ByName(persona.PhaseReview))
	if err != nil {
		return fmt.Errorf("reviewer driver: %w", err)
	}
	execDrv, err := ctx.NewDriver(*persona.ByName(persona.PhaseFix))
	if err != nil {
		return fmt.Errorf("executor driver: %w", err)
	}

	reviewModel := pipeline.ModelName(reviewDrv)
	execModel := pipeline.ModelName(execDrv)

	var rvPasses, rvCalls, rvFiles int
	var rvIn, rvOut int
	var rvCost float64
	skipProject := opts.skipProjectCheck || cfg.cfg.Command.SkipProjectCheck
	skipSecurity := opts.skipSecurityCheck || cfg.cfg.Command.SkipSecurityCheck

	startPass := 1
	if state != nil {
		startPass = state.Pass
	}

	var changedPaths map[string]bool // nil on first pass = review all; set on subsequent passes

	for i := startPass; i <= maxFixIterations; i++ {
		banner("🔍", fmt.Sprintf("review pass %d/%d", i, maxFixIterations))

		mech := runMechanical(ctx.Root, cfg, opts.dryRun, skipProject, skipSecurity)

		status("📋", "checks", fmt.Sprintf("%s format  %s project  %s security",
			passLabel(mech.Format.Pass), passLabel(mech.ProjectCheck.Pass), passLabel(mech.Security.Pass)))

		passStart := time.Now()
		stop, done := spinner(reviewModel, "review", fmt.Sprintf("pass %d/%d", i, maxFixIterations))
		rv, err := reviewer.Run(reviewDrv, p, reviewer.Context{
			Root:            ctx.Root,
			KB:              kb,
			InjectContent:   ctx.InjectContent(persona.PhaseReview),
			Mechanical:      mech,
			SuccessCriteria: p.SuccessCriteria,
			Files:           fileContents,
			CheckFilter:     cfg.cfg.Command.CheckFilter,
			ReviewFiles:     changedPaths,
		})
		close(stop)
		<-done
		if err != nil {
			return fmt.Errorf("reviewer: %w", err)
		}
		rvCalls++
		totalIn += rv.Metrics.TotalInputTokens()
		totalOut += rv.Metrics.OutputTokens
		totalCost += rv.Metrics.CostUSD
		rvIn += rv.Metrics.TotalInputTokens()
		rvOut += rv.Metrics.OutputTokens
		rvCost += rv.Metrics.CostUSD
		status("✓", "review done", fmt.Sprintf("%s in, %s out (%.0fs)",
			pipeline.FormatTokens(rv.Metrics.TotalInputTokens()),
			pipeline.FormatTokens(rv.Metrics.OutputTokens),
			time.Since(passStart).Seconds()))

		n := len(rv.Review.Findings)
		if !rv.Review.HasErrors() {
			if n > 0 {
				status("✓", "result", fmt.Sprintf("all clear — %d warning(s)", n))
			} else {
				status("✓", "result", "all clear")
			}
			rvPasses = i
			os.Remove(statePath)
			break
		}

		status("✗", "finding(s)", fmt.Sprintf("%d issue(s):", n))
		for _, f := range rv.Review.Findings {
			fmt.Fprintf(os.Stderr, "       %s  %s%s\n",
				severityIcon(f.Severity), cRed+f.Problem+cReset, cDim+" ("+f.File+")"+cReset)
		}

		// ── Feedback gate (first pass only) ──────────────────────────
		if i == 1 && state == nil {
			if isStdinTerminal() {
				fmt.Fprintf(os.Stderr, "\n  [a]pprove — auto-fix  [f]eedback — add yours  [s]kip — accept\n")
			}
			choice := strings.TrimSpace(strings.ToLower(choiceOrDefault(isStdinTerminal, "  > ", "")))
			switch {
			case choice == "f" || choice == "feedback":
				collectUserFeedback(&rv.Review.Findings)
			case choice == "s" || choice == "skip":
				rvPasses = i
				fmt.Fprintf(os.Stderr, "  %s accepted with %d finding(s)\n", "⚠️", n)
				os.Remove(statePath)
				goto summary
			}
		}

		// ── Circuit breaker (max iterations) ─────────────────────────
		if i == maxFixIterations {
			rvPasses = i
			fmt.Fprintf(os.Stderr, "\n  %s %d findings after %d passes — auto-fix exhausted\n", "⚠️", n, maxFixIterations)
			fmt.Fprintf(os.Stderr, "  [r]eplan — stop  [f]eedback — 1 more fix  [a]ccept — exit\n")
			choice := strings.TrimSpace(strings.ToLower(choiceOrDefault(isStdinTerminal, "  > ", "")))

			switch {
			case choice == "f" || choice == "feedback":
				collectUserFeedback(&rv.Review.Findings)
			case choice == "r" || choice == "replan":
				fmt.Fprintf(os.Stderr, "  %s structural issues — re-plan with: fabric plan \"<task> + fix: <findings>\"%s\n", cDim, cReset)
				os.Remove(statePath)
				goto summary
			default:
				os.Remove(statePath)
				goto summary
			}
		}

		// ── Auto-fix ─────────────────────────────────────────────────
		findingsByFile := groupFindingsByFile(ctx.Root, rv.Review.Findings)
		stop, done = spinner(execModel, "fix", fmt.Sprintf("pass %d/%d", i, maxFixIterations))
		fixResult, err := buildfix.ExecuteFindingsFix(execDrv, ctx.Root, findingsByFile, buildfix.FixOptions{})
		close(stop)
		<-done
		if err != nil {
			status("✗", "fix", err.Error())
			fmt.Fprintf(os.Stderr, "\n  The fix executor returned empty. The LLM cannot determine the fix.\n")
			fmt.Fprintf(os.Stderr, "  [g]ive guidance  [s]kip  [r]eplan\n")
			choice := strings.TrimSpace(strings.ToLower(choiceOrDefault(isStdinTerminal, "  > ", "s")))
			switch {
			case choice == "s" || choice == "skip":
				rvPasses = i
				os.Remove(statePath)
				goto summary
			case choice == "r" || choice == "replan":
				fmt.Fprintf(os.Stderr, "  %s Use: fabric plan <task>%s\n", cDim, cReset)
				os.Remove(statePath)
				goto summary
			default:
				collectUserFeedbackNoTimeout(&rv.Review.Findings)
			}
			saveState(statePath, i+1, rv.Review.Findings, fileContents)
			if i < maxFixIterations {
				continue
			}
			rvPasses = i + 1
			break
		}
		rvCalls++
		totalIn += fixResult.Metrics.TotalInputTokens()
		totalOut += fixResult.Metrics.OutputTokens
		totalCost += fixResult.Metrics.CostUSD
		rvIn += fixResult.Metrics.TotalInputTokens()
		rvOut += fixResult.Metrics.OutputTokens
		rvCost += fixResult.Metrics.CostUSD

		fixed := writeFiles(ctx.Root, fixResult.Files, opts.dryRun)
		rvFiles += fixed
		mergeMaps(fileContents, fixResult.Files)
		ctx.SyncBack(fixResult.Files)

		if !opts.dryRun && cfg.cfg.Command.Format != "" {
			captureCmdStreaming(ctx.Root, cfg.cfg.Command.Format, "  │ ")
		}

		if len(fixResult.Files) > 0 {
			changedPaths = make(map[string]bool)
			for p := range fixResult.Files {
				changedPaths[p] = true
			}
		}

		saveState(statePath, i+1, rv.Review.Findings, fileContents)
	}

	if rvPasses == 0 {
		rvPasses = maxFixIterations
	}
	os.Remove(statePath)

summary:
	totalBanner("", "review complete")
	writeTotalLine(" passes", fmt.Sprintf("%d", rvPasses))
	writeTotalLine(" files fixed", fmt.Sprintf("%d", rvFiles))
	writeTotalLine(" time", time.Since(totalStart).Round(time.Millisecond).String())
	writeTotalLine(" tokens", fmt.Sprintf("%s in  %s out", pipeline.FormatTokens(rvIn), pipeline.FormatTokens(rvOut)))
	writeTotalLine(" cost", fmt.Sprintf("$%.4f", rvCost))
	writeTotalLine(" llm calls", fmt.Sprintf("%d", rvCalls))
	separator("━")
	return nil
}

// ── Resume helpers ──────────────────────────────────────────────────────

func reviewStatePath(planFile string) string {
	ext := filepath.Ext(planFile)
	base := strings.TrimSuffix(planFile, ext)
	return base + ".review.json"
}

func tryResume(statePath string) (*reviewState, bool) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, false
	}
	var s reviewState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, false
	}
	if s.Pass < 1 || s.Pass > maxFixIterations {
		return nil, false
	}

	fmt.Fprintf(os.Stderr, "\n  %s Found paused review (pass %d/%d, %d findings)\n",
		"⏸", s.Pass, maxFixIterations, len(s.Findings))
	fmt.Fprintf(os.Stderr, "  [r]esume  [n]ew review  (30s)\n")
	choice := promptWithTimeout("  > ", 30*time.Second, "r")
	choice = strings.TrimSpace(strings.ToLower(choice))

	if choice == "n" || choice == "new" {
		os.Remove(statePath)
		return nil, false
	}
	return &s, true
}

func saveState(statePath string, pass int, findings []reviewer.Finding, files map[string]string) {
	s := reviewState{Pass: pass, Findings: findings, Files: files}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(statePath, data, 0644)
}

func collectUserFeedback(findings *[]reviewer.Finding) {
	fmt.Fprint(os.Stderr, "  feedback > ")
	fb := shared.ReadLine()
	if fb != "" {
		*findings = append(*findings, reviewer.Finding{
			Severity: "error", File: "(user feedback)",
			Problem: fb, FixWhere: "N/A", FixWhat: fb,
		})
	}
}

func collectUserFeedbackNoTimeout(findings *[]reviewer.Finding) {
	collectUserFeedback(findings)
}

// groupFindingsByFile builds a map of file path to error lines from review findings.
// Files that don't exist on disk are included — the fix executor will create them.
func groupFindingsByFile(root string, findings []reviewer.Finding) map[string][]string {
	byFile := map[string][]string{}
	for _, f := range findings {
		path := strings.TrimSpace(f.File)
		if path == "" || path == "(user feedback)" {
			continue
		}
		text := fmt.Sprintf("## %s: %s\nFix where: %s\n%s",
			strings.ToUpper(f.Severity), f.Problem, f.FixWhere, f.FixWhat)
		byFile[path] = append(byFile[path], text)
	}
	return byFile
}

func passLabel(ok bool) string {
	if ok {
		return cGreen + "✓" + cReset
	}
	return cRed + "✗" + cReset
}

func severityIcon(s string) string {
	if s == "error" {
		return "✗"
	}
	return "⚠"
}

type reviewOpts struct {
	configPath        string
	modelOverride     string
	dryRun            bool
	skipProjectCheck  bool
	skipSecurityCheck bool
	planFile          string
}

func parseReviewFlags(args []string) (reviewOpts, error) {
	opts := reviewOpts{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i < len(args) {
				opts.configPath = args[i]
			}
		case "--model":
			i++
			if i < len(args) {
				opts.modelOverride = args[i]
			}
		case "--dry-run":
			opts.dryRun = true
		case "--skip-project-check":
			opts.skipProjectCheck = true
		case "--skip-security-check":
			opts.skipSecurityCheck = true
		default:
			if !strings.HasPrefix(args[i], "-") && opts.planFile == "" {
				opts.planFile = args[i]
				continue
			}
			return opts, fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return opts, nil
}

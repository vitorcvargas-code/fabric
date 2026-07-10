package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vitorcvargas-code/fabric/internal/architect"
	"github.com/vitorcvargas-code/fabric/internal/augmenter"
	"github.com/vitorcvargas-code/fabric/internal/compressor"
	"github.com/vitorcvargas-code/fabric/internal/linear"
	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/persona"
	"github.com/vitorcvargas-code/fabric/internal/pipeline"
	"github.com/vitorcvargas-code/fabric/internal/plan"
	"github.com/vitorcvargas-code/fabric/internal/shared"
)

func runPlan(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		printCommandHelp("plan")
		return nil
	}
	opts, err := parsePlanFlags(args)
	if err != nil {
		return err
	}

	cfg, err := loadConfig(opts.configPath)
	if err != nil {
		return err
	}

	ctx, err := pipeline.NewContext(cfg.cfg, false, cfg.cfg.Command.UseWorktree)
	if err != nil {
		return err
	}
	defer ctx.Close()
	root := ctx.Root

	kbContent, err := os.ReadFile(filepath.Join(root, "knowledge-base.md"))
	if err != nil {
		return fmt.Errorf("knowledge-base.md not found — run 'fabric init' first")
	}

	manualContent := resolveFile("skills/plan/references/how-to-use-kb.md")
	if manualContent == nil {
		return fmt.Errorf("how-to-use-kb.md not found")
	}

	if err := logLLMSetup(cfg, opts.modelOverride); err != nil {
		return err
	}

	totalStart := time.Now()
	var phaseMetrics []phaseMetric
	rawTask := opts.rawInput
	var compressedTask string

	if opts.issueKey != "" {
		fmt.Fprintf(os.Stderr, "fabric: fetching Jira issue %s...\n", opts.issueKey)
		issueJSON, err := fetchJiraIssue(opts.issueKey)
		if err != nil {
			return fmt.Errorf("fetching Jira issue: %w", err)
		}
		rawTask = issueJSON
		if opts.rawInput == "" {
			opts.rawInput = opts.issueKey
		}
	}

	if opts.linearKey != "" {
		fmt.Fprintf(os.Stderr, "fabric: fetching Linear issue %s...\n", opts.linearKey)
		taskText, err := fetchLinearIssue(opts.linearKey)
		if err != nil {
			return fmt.Errorf("fetching Linear issue: %w", err)
		}
		rawTask = taskText
		if opts.rawInput == "" {
			opts.rawInput = opts.linearKey
		}
	}

	stdinContext, _ := readStdin()
	compressorInput := ""
	if len(stdinContext) > 500 {
		compressorInput = stdinContext
	} else if len(rawTask) > 2000 || looksLikeJSON(rawTask) {
		compressorInput = rawTask
	}

	if len(compressorInput) > 500 {
		compDrv, err := ctx.NewDriver(*persona.ByName(persona.PhaseCompress))
		if err != nil {
			return fmt.Errorf("compressor driver: %w", err)
		}
		fmt.Fprintf(os.Stderr, "fabric: [compress] %.1fK input...\n", float64(len(compressorInput))/1024)

		compStart := time.Now()
		compResult, err := compressor.Run(compDrv, compressorInput)
		compLatency := time.Since(compStart)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fabric: compressor warning (non-fatal): %v\n", err)
		} else {
			compressedTask = compResult.Task.Format()
fmt.Fprintf(os.Stderr, "fabric: [compress] %.1fK → %.1fK, %d in/%d out, $%.4f, %v\n", float64(len(compressorInput))/1024, float64(len(compressedTask))/1024, compResult.Metrics.TotalInputTokens(), compResult.Metrics.OutputTokens, compResult.Metrics.CostUSD, compLatency.Round(time.Millisecond))
			phaseMetrics = append(phaseMetrics, phaseMetric{
				name: "compress", model: actualModel(compResult.Metrics),
				tokensIn: compResult.Metrics.TotalInputTokens(), tokensOut: compResult.Metrics.OutputTokens,
				cost: compResult.Metrics.CostUSD, latency: compLatency,
			})
			if compResult.Task.Summary != "" {
				rawTask = compResult.Task.Summary
			}
		}
	}

	taskStr := rawTask
	if compressedTask != "" {
		taskStr = compressedTask + "\n\nAuthor summary: " + rawTask
	}

	archKB := kbContent
	if sections := ctx.PhaseKBSections(persona.PhaseArchitect); len(sections) > 0 {
		archKB = shared.FilterKBSections(kbContent, sections...)
	}
	archCtx := architect.Context{Task: taskStr, KB: archKB, Manual: manualContent, InjectContent: ctx.InjectContent(persona.PhaseArchitect)}
	augKB := shared.FilterKBSections(kbContent, "2", "3", "7")

	var confirmedSC []string
	var answers map[string]string
	augDrv, augErr := ctx.NewDriver(*persona.ByName(persona.PhaseAugment))
	if augErr == nil {
		augCtx := augmenter.Context{RawTask: rawTask, CompressedTask: compressedTask, KB: augKB}
		augStart := time.Now()
		ar, augMetrics, aErr := augmenter.Analyze(augDrv, augCtx)
		augLatency := time.Since(augStart)
		if aErr != nil {
			fmt.Fprintf(os.Stderr, "fabric: [augment] error: %v\n", aErr)
			if !opts.autoApprove {
				confirmedSC = augmenter.ConfirmSuccessCriteria(nil,
					"Augmenter unavailable — define success criteria manually")
			}
		} else {
			phaseMetrics = append(phaseMetrics, phaseMetric{
				name: "augment-analyze", model: actualModel(augMetrics),
				tokensIn: augMetrics.TotalInputTokens(), tokensOut: augMetrics.OutputTokens,
				cost: augMetrics.CostUSD, latency: augLatency,
			})
				fmt.Fprintf(os.Stderr, "fabric: [augment] %d in/%d out, $%.4f, %v\n", augMetrics.TotalInputTokens(), augMetrics.OutputTokens, augMetrics.CostUSD, augLatency.Round(time.Millisecond))
			if !opts.autoApprove && ar.HasQuestions && len(ar.Questions) > 0 {
				answers = augmenter.PresentQuestions(ar.Questions)
				resolveStart := time.Now()
				resolved, resolveMetrics, rErr := augmenter.Resolve(augDrv,
					rawTask, compressedTask, ar.Questions, answers, augKB)
				if rErr != nil {
					fmt.Fprintf(os.Stderr, "fabric: [augment resolve] error: %v\n", rErr)
				} else {
					archCtx.Task = resolved
					phaseMetrics = append(phaseMetrics, phaseMetric{
						name: "augment-resolve", model: actualModel(resolveMetrics),
						tokensIn: resolveMetrics.TotalInputTokens(), tokensOut: resolveMetrics.OutputTokens,
						cost: resolveMetrics.CostUSD, latency: time.Since(resolveStart),
					})
						fmt.Fprintf(os.Stderr, "fabric: [augment resolve] %d in/%d out, $%.4f, %v\n", resolveMetrics.TotalInputTokens(), resolveMetrics.OutputTokens, resolveMetrics.CostUSD, time.Since(resolveStart).Round(time.Millisecond))
				}
			}
			if len(ar.SuccessCriteria) == 0 && len(ar.Questions) > 0 {
				// Use Q&A answers as criteria candidates when LLM finds none.
				for _, q := range ar.Questions {
					if a, ok := answers[q.ID]; ok && a != "" {
						ar.SuccessCriteria = append(ar.SuccessCriteria, a)
					}
				}
			}
			if opts.autoApprove {
				confirmedSC = ar.SuccessCriteria
			} else {
				confirmedSC = augmenter.ConfirmSuccessCriteria(ar.SuccessCriteria, ar.SCNote)
			}
		}
		archCtx.SuccessCriteria = confirmedSC
	} else {
		fmt.Fprintf(os.Stderr, "fabric: [augment] driver error: %v, skipping\n", augErr)
	}

	const maxArchIter = 5
	var archPlan *architect.Plan
	var planApproved bool
	archFeedback := ""

	// Session reuse: create driver + session once, reuse across feedback iterations.
	archPhaseCfg := cfg.cfg.LLM.Phases["architect"]
	var sessionRunner *architect.SessionRunner
	var sessionModel string
	if archPhaseCfg.SessionReuse {
		archDrv, err := ctx.NewDriver(*persona.ByName(persona.PhaseArchitect))
		if err != nil {
			return fmt.Errorf("architect driver: %w", err)
		}
		sessionModel = pipeline.ModelName(archDrv)
		if isc, ok := archDrv.(llm.IsolateSessionConfigurable); ok {
			isc.SetIsolateSessions(false)
		}
		session := llm.NewSession(archDrv, "")
		if session == nil {
			return fmt.Errorf("architect driver does not support sessions")
		}
		sessionRunner = architect.NewSessionRunner(session, archCtx)
	}

	for !planApproved && len(phaseMetrics) < 50 {
		archRunIndex := countByPrefix(phaseMetrics, "architect") + 1
		if archRunIndex > maxArchIter {
			return fmt.Errorf("plan aborted after %d iterations", maxArchIter)
		}

		runCtx := archCtx
		if archFeedback != "" {
			runCtx.Task = archCtx.Task + "\n\nUser feedback on previous plan:\n" + archFeedback
		}

		var result architect.Result
		var archLatency time.Duration
		archStart := time.Now()

		if sessionRunner != nil {
			if archFeedback == "" {
				stop, done := spinner(sessionModel, "architect", "")
				result, err = sessionRunner.Bootstrap()
				close(stop)
				<-done
			} else {
				stop, done := spinner(sessionModel, "architect", "resume")
				result, err = sessionRunner.Resume(runCtx.Task)
				close(stop)
				<-done
			}
		} else {
			archDrv, err := ctx.NewDriver(*persona.ByName(persona.PhaseArchitect))
			if err != nil {
				return fmt.Errorf("architect driver: %w", err)
			}
			stop, done := spinner(pipeline.ModelName(archDrv), "architect", "")
			result, err = architect.Run(archDrv, runCtx)
			close(stop)
			<-done
		}
		archLatency = time.Since(archStart)
		if err != nil {
			return fmt.Errorf("architect failed: %w", err)
		}

		phaseMetrics = append(phaseMetrics, phaseMetric{
			name: "architect", model: actualModel(result.Metrics),
			tokensIn: result.Metrics.TotalInputTokens(), tokensOut: result.Metrics.OutputTokens,
			cost: result.Metrics.CostUSD, latency: archLatency,
		})

			fmt.Fprintf(os.Stderr, "fabric: [architect] %d in/%d out, $%.4f, %v\n", result.Metrics.TotalInputTokens(), result.Metrics.OutputTokens, result.Metrics.CostUSD, archLatency.Round(time.Millisecond))
		archPlan = result.Plan
		if len(confirmedSC) > 0 {
			archPlan.SuccessCriteria = confirmedSC
		}

		fieldIssues := plan.ValidateFields(archPlan)
			pathIssues := plan.ValidatePaths(archPlan, root)
			issues := append(fieldIssues, pathIssues...)
		if msg := plan.Format(issues); msg != "" {
			fmt.Fprint(os.Stderr, msg)
		}
		if plan.HasErrors(issues) {
			fmt.Fprintf(os.Stderr, "fabric: plan has %d validation error(s)\n", plan.ErrorCount(issues))
			if opts.execReady {
				return fmt.Errorf("plan failed exec-ready validation (%d error(s))", plan.ErrorCount(issues))
			}
		} else if len(issues) == 0 {
			fmt.Fprintf(os.Stderr, "fabric: plan certified exec-ready\n")
		}

		htmlPath := filepath.Join(root, "plan.html")
		if err := os.WriteFile(htmlPath, []byte(planToHTML(archPlan)), 0644); err == nil {
			fmt.Fprintf(os.Stderr, "\n  plan written to %splan.html%s\n", cBold, cReset)
		}

		if opts.autoApprove {
			planApproved = true
			break
		}

		fmt.Fprintf(os.Stderr, "\n  [a]pprove  [f]eedback  [q]uit\n")
		fmt.Fprint(os.Stderr, "  > ")
		choice := strings.TrimSpace(strings.ToLower(shared.ReadLine()))

		switch {
		case choice == "a" || choice == "approve" || choice == "":
			planApproved = true
		case choice == "q" || choice == "quit":
			return fmt.Errorf("plan aborted by user")
		case choice == "f" || choice == "feedback":
			fmt.Fprint(os.Stderr, "  feedback > ")
			archFeedback = strings.TrimSpace(shared.ReadLine())
			if archFeedback == "" {
				planApproved = true
			}
		default:
			planApproved = true
		}
	}

	if !planApproved {
		return fmt.Errorf("plan aborted")
	}

	out, err := formatPlan(archPlan, opts.format)
	if err != nil {
		return fmt.Errorf("formatting output: %w", err)
	}
	fmt.Println(out)

	if opts.outPath != "" {
		jsonOut, _ := formatPlan(archPlan, "json")
		if jsonOut != "" {
			os.WriteFile(opts.outPath, []byte(jsonOut+"\n"), 0644)
		}
	}

	totalLatency := time.Since(totalStart)
	var totalIn, totalOut int
	var totalCost float64
	for _, pm := range phaseMetrics {
		totalIn += pm.tokensIn
		totalOut += pm.tokensOut
		totalCost += pm.cost
	}
	fmt.Fprintf(os.Stderr, "\n── phase metrics ─────────────────────────────\n")
	for _, pm := range phaseMetrics {
		fmt.Fprintf(os.Stderr, "  %-12s %s  %d in / %d out  $%.4f  %v\n",
			pm.name+":", pm.model, pm.tokensIn, pm.tokensOut, pm.cost, pm.latency.Round(time.Millisecond))
	}
	fmt.Fprintf(os.Stderr, "  ─────────────────────────────────────────────\n")
	fmt.Fprintf(os.Stderr, "  %-12s %d calls  %d in / %d out  $%.4f  %v\n",
		"total:", len(phaseMetrics), totalIn, totalOut, totalCost, totalLatency.Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "──────────────────────────────────────────────\n")

	return nil
}

func countByPrefix(metrics []phaseMetric, prefix string) int {
	n := 0
	for _, m := range metrics {
		if strings.HasPrefix(m.name, prefix) {
			n++
		}
	}
	return n
}

type phaseMetric struct {
	name      string
	model     string
	tokensIn  int
	tokensOut int
	cost      float64
	latency   time.Duration
}

func actualModel(m llm.Metrics) string {
	if m.Model != "" {
		return m.Model
	}
	return m.RequestedModel
}

func looksLikeJSON(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) > 0 && (s[0] == '{' || s[0] == '[')
}

func fetchLinearIssue(identifier string) (string, error) {
	client, err := linear.NewClient()
	if err != nil {
		return "", err
	}
	issue, err := client.FetchIssue(identifier)
	if err != nil {
		return "", err
	}
	return issue.TaskText(), nil
}

func fetchJiraIssue(key string) (string, error) {
	cmd := exec.Command("acli", "jira", "workitem", "view", key,
		"--json", "--fields", "summary,description,subtasks,parent,issuelinks,customfield_10008")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("acli: %w (is 'acli' installed and authenticated?)", err)
	}
	return string(out), nil
}

func formatPlan(p *architect.Plan, format string) (string, error) {
	switch format {
	case "json":
		b, err := json.MarshalIndent(p, "", "  ")
		return string(b), err
	case "yaml", "":
		return planToYAML(p), nil
	default:
		return "", fmt.Errorf("unknown format %q", format)
	}
}

func planToYAML(p *architect.Plan) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("summary: %s\n", p.Summary))
	if len(p.FilesToCreate) > 0 {
		sb.WriteString("files_to_create:\n")
		for _, f := range p.FilesToCreate {
			sb.WriteString(fmt.Sprintf("  - path: %s\n", f.Path))
			sb.WriteString(fmt.Sprintf("    action: %s\n", f.Action))
			sb.WriteString(fmt.Sprintf("    purpose: %s\n", f.Purpose))
			if f.Spec != "" {
				sb.WriteString(fmt.Sprintf("    spec: |\n      %s\n", strings.ReplaceAll(f.Spec, "\n", "\n      ")))
			}
			if f.Follows != "" {
				sb.WriteString(fmt.Sprintf("    follows: %s\n", f.Follows))
			}
			if len(f.Needs) > 0 {
				sb.WriteString(fmt.Sprintf("    needs: [%s]\n", strings.Join(f.Needs, ", ")))
			}
			if f.SideEffects != "" {
				sb.WriteString(fmt.Sprintf("    side_effects: %s\n", f.SideEffects))
			}
		}
	}
	if len(p.FilesToModify) > 0 {
		sb.WriteString("files_to_modify:\n")
		for _, f := range p.FilesToModify {
			sb.WriteString(fmt.Sprintf("  - path: %s\n", f.Path))
			sb.WriteString(fmt.Sprintf("    action: %s\n", f.Action))
			sb.WriteString(fmt.Sprintf("    purpose: %s\n", f.Purpose))
			if f.Spec != "" {
				sb.WriteString(fmt.Sprintf("    spec: |\n      %s\n", strings.ReplaceAll(f.Spec, "\n", "\n      ")))
			}
		}
	}
	if len(p.Tests) > 0 {
		sb.WriteString("tests:\n")
		for _, t := range p.Tests {
			sb.WriteString(fmt.Sprintf("  - file: %s\n", t.File))
			sb.WriteString(fmt.Sprintf("    strategy: %s\n", t.Strategy))
		}
	}
	if len(p.PatternsToFollow) > 0 {
		sb.WriteString("patterns_to_follow:\n")
		for _, pt := range p.PatternsToFollow {
			sb.WriteString(fmt.Sprintf("  - %s\n", pt))
		}
	}
	if len(p.Risks) > 0 {
		sb.WriteString("risks:\n")
		for _, r := range p.Risks {
			sb.WriteString(fmt.Sprintf("  - %s\n", r))
		}
	}
	if len(p.SuccessCriteria) > 0 {
		sb.WriteString("success_criteria:\n")
		for _, sc := range p.SuccessCriteria {
			sb.WriteString(fmt.Sprintf("  - %s\n", sc))
		}
	}
	return sb.String()
}

type planOpts struct {
	rawInput      string
	configPath    string
	modelOverride string
	format        string
	autoApprove   bool
	execReady     bool
	issueKey      string
	linearKey     string
	outPath       string
}

func parsePlanFlags(args []string) (planOpts, error) {
	opts := planOpts{format: "yaml"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--config requires a value")
			}
			i++
			opts.configPath = args[i]
		case "--model":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--model requires a value")
			}
			i++
			opts.modelOverride = args[i]
		case "--issue":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--issue requires an issue key (e.g. SWACCPP-722)")
			}
			i++
			opts.issueKey = args[i]
		case "--linear":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--linear requires an issue identifier (e.g. FAB-12)")
			}
			i++
			opts.linearKey = args[i]
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			i++
			opts.format = args[i]
		case "--yes", "-y":
			opts.autoApprove = true
		case "--out":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--out requires a path")
			}
			i++
			opts.outPath = args[i]
		case "--exec-ready":
			opts.execReady = true
		default:
			if strings.HasPrefix(args[i], "--") {
				return opts, fmt.Errorf("unknown flag %q", args[i])
			}
			if opts.rawInput != "" {
				return opts, fmt.Errorf("unexpected argument %q (task must be a single quoted string)", args[i])
			}
			opts.rawInput = args[i]
		}
	}
	if opts.rawInput == "" && opts.issueKey != "" {
		opts.rawInput = opts.issueKey
	}
	if opts.rawInput == "" && opts.linearKey != "" {
		opts.rawInput = opts.linearKey
	}
	if opts.rawInput == "" && opts.issueKey == "" && opts.linearKey == "" {
		return opts, fmt.Errorf("plan requires a task description, --issue, or --linear flag\n\nUsage: fabric plan \"<task>\"\n       fabric plan --issue SWACCPP-722\n       fabric plan --linear FAB-12")
	}
	return opts, nil
}

func resolveFile(paths ...string) []byte {
	for _, p := range paths {
		if exe, err := os.Executable(); err == nil {
			if content, err := os.ReadFile(filepath.Join(filepath.Dir(exe), p)); err == nil {
				return content
			}
		}
		if content, err := os.ReadFile(p); err == nil {
			return content
		}
	}
	return nil
}

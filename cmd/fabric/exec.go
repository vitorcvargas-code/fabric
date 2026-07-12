package main

import (
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vitorcvargas-code/fabric/internal/architect"
	"github.com/vitorcvargas-code/fabric/internal/buildfix"
	"github.com/vitorcvargas-code/fabric/internal/executor"
	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/persona"
	"github.com/vitorcvargas-code/fabric/internal/pipeline"
	"github.com/vitorcvargas-code/fabric/internal/shared"
	"github.com/vitorcvargas-code/fabric/internal/tester"
)

func runExec(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		printCommandHelp("exec")
		return nil
	}
	opts, err := parseExecFlags(args)
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

	root := ctx.Root

	banner("⚡", "fabric exec")
	if err := logLLMSetup(cfg, opts.modelOverride); err != nil {
		return err
	}

	p, kb, err := loadPlan(resolvePlanPath(ctx.Orig, opts.planFile))
	if err != nil {
		return err
	}

	execKB := resolveKB(kb, ctx, persona.PhaseExec, "2", "4", "5", "6", "7")
	testKB := resolveKB(kb, ctx, persona.PhaseTest, "2", "5", "6", "7")

	var totalStart = time.Now()
	var calls int
	var totalIn, totalOut int
	var totalCost float64
	var written int

	n := len(p.FilesToCreate) + len(p.FilesToModify)
	fmt.Fprintf(os.Stderr, "\n  %s%d files%s (%d new, %d modify)\n", cBold, n, cReset, len(p.FilesToCreate), len(p.FilesToModify))
	separator("─")

	// ── Idempotency: exclude already-written files ─────────────────────
	touched := map[string]bool{}
	for _, f := range p.FilesToCreate {
		if fileWritten(root, f.Path) {
			touched[f.Path] = true
		}
	}
	for _, f := range p.FilesToModify {
		if fileWritten(root, f.Path) {
			touched[f.Path] = true
		}
	}
	if len(touched) > 0 {
		fmt.Fprintf(os.Stderr, "  %s%d files%s already written — skipping\n", cDim, len(touched), cReset)
	}

	// ── Codegen ────────────────────────────────────────────────────────
	execDrv, err := ctx.NewDriver(*persona.ByName(persona.PhaseExec))
	if err != nil {
		return fmt.Errorf("executor driver: %w", err)
	}
	execModel := pipeline.ModelName(execDrv)

	allEntries := executor.BuildFileList(p)
	preFilterBatches := (len(allEntries) + executor.MaxFilesPerBatch - 1) / executor.MaxFilesPerBatch
	_ = preFilterBatches // kept for reference; actual batch count may differ after idempotency filter
	var execMetrics llm.Metrics

	// Pre-compute batches: group entries and filter already-written files.
	type batchSpec struct {
		num     int
		entries []executor.FileEntry
	}
	var batches []batchSpec
	for start := 0; start < len(allEntries); start += executor.MaxFilesPerBatch {
		end := start + executor.MaxFilesPerBatch
		if end > len(allEntries) {
			end = len(allEntries)
		}
		var pending []executor.FileEntry
		for _, e := range allEntries[start:end] {
			if !touched[e.Path] {
				pending = append(pending, e)
			}
		}
		if len(pending) == 0 {
			continue
		}
		batches = append(batches, batchSpec{
			num:     len(batches) + 1,
			entries: pending,
		})
	}

	if len(batches) > 0 {
		if len(batches) == 1 {
			// Single batch: run sequentially (no goroutine overhead).
			stop, done := spinner(execModel, "codegen", fmt.Sprintf("batch %d/%d", batches[0].num, len(batches)))
			batchFiles, m, err := executor.ExecuteBatch(execDrv, allEntries, batches[0].entries, string(execKB))
			close(stop)
			<-done
			if err != nil {
				return fmt.Errorf("executor: %w", err)
			}
			filterBatch(batchFiles, batches[0].entries)
			written += writeFiles(root, batchFiles, opts.dryRun)
			for p := range batchFiles {
				touched[p] = true
			}
			ctx.SyncBack(batchFiles)
			execMetrics = m
		} else if sessionReuseEnabled(cfg, "exec_create") {
			// Multiple batches with session reuse: per-batch spinners, one session.
			if isc, ok := execDrv.(llm.IsolateSessionConfigurable); ok {
				isc.SetIsolateSessions(false)
			}
			session := llm.NewSession(execDrv, "")
			if session == nil {
				return fmt.Errorf("executor driver does not support sessions")
			}
			for i, b := range batches {
				first := i == 0
				stop, done := spinner(execModel, "codegen", fmt.Sprintf("batch %d/%d", b.num, len(batches)))
				batchFiles, m, err := executor.ExecuteSessionBatch(session, first, allEntries, b.entries, string(execKB))
				close(stop)
				<-done
				if err != nil {
					return fmt.Errorf("executor batch %d: %w", b.num, err)
				}
				filterBatch(batchFiles, b.entries)
				written += writeFiles(root, batchFiles, opts.dryRun)
				for p := range batchFiles {
					touched[p] = true
				}
				ctx.SyncBack(batchFiles)
				if execMetrics.Calls == 0 {
					execMetrics = m
				} else {
					execMetrics = llm.Merge(execMetrics, m)
				}
			}
		} else {
			// Multiple batches without session reuse: loop sequentially.
			for _, b := range batches {
				stop, done := spinner(execModel, "codegen", fmt.Sprintf("batch %d/%d", b.num, len(batches)))
				batchFiles, m, err := executor.ExecuteBatch(execDrv, allEntries, b.entries, string(execKB))
				close(stop)
				<-done
				if err != nil {
					return fmt.Errorf("executor: %w", err)
				}
				filterBatch(batchFiles, b.entries)
				written += writeFiles(root, batchFiles, opts.dryRun)
				for p := range batchFiles {
					touched[p] = true
				}
				ctx.SyncBack(batchFiles)
				if execMetrics.Calls == 0 {
					execMetrics = m
				} else {
					execMetrics = llm.Merge(execMetrics, m)
				}
			}
		}
	}
	calls += execMetrics.Calls
	totalIn += execMetrics.TotalInputTokens()
	totalOut += execMetrics.OutputTokens
	totalCost += execMetrics.CostUSD
	status("✓", "codegen done", fmt.Sprintf("%s in, %s out (%.0fs, %d calls, %d batches)",
		pipeline.FormatTokens(execMetrics.TotalInputTokens()),
		pipeline.FormatTokens(execMetrics.OutputTokens),
		time.Since(totalStart).Seconds(), execMetrics.Calls, len(batches)))

	// ── Tester ────────────────────────────────────────────────────────
	if len(p.Tests) > 0 {
		testDrv, err2 := ctx.NewDriver(*persona.ByName(persona.PhaseTest))
		if err2 != nil {
			return fmt.Errorf("tester driver: %w", err2)
		}
		testModel := pipeline.ModelName(testDrv)
		fmt.Fprintf(os.Stderr, "\n  %s%d tests%s\n", cBold, len(p.Tests), cReset)
		separator("─")

		totalTestBatches := (len(p.Tests) + tester.MaxTestsPerBatch - 1) / tester.MaxTestsPerBatch
		var testMetrics llm.Metrics

		// Pre-compute test batches.
		var testBatches [][]architect.TestEntry
		for start := 0; start < len(p.Tests); start += tester.MaxTestsPerBatch {
			end := start + tester.MaxTestsPerBatch
			if end > len(p.Tests) {
				end = len(p.Tests)
			}
			var pending []architect.TestEntry
			for _, t := range p.Tests[start:end] {
				if !touched[t.File] {
					pending = append(pending, t)
				}
			}
			if len(pending) > 0 {
				testBatches = append(testBatches, pending)
			}
		}

		if len(testBatches) > 0 {
			if len(testBatches) == 1 {
				stop, done := spinner(testModel, "tests", "batch 1/1")
				batchFiles, m, err2 := tester.RunBatch(testDrv, p.Tests, testBatches[0], string(testKB))
				close(stop)
				<-done
				if err2 != nil {
					return fmt.Errorf("tester: %w", err2)
				}
				written += writeFiles(root, batchFiles, opts.dryRun)
				for p := range batchFiles {
					touched[p] = true
				}
				ctx.SyncBack(batchFiles)
				testMetrics = m
			} else if sessionReuseEnabled(cfg, "test") {
				// Multi-batch with session reuse: per-batch spinners, one session.
				if isc, ok := testDrv.(llm.IsolateSessionConfigurable); ok {
					isc.SetIsolateSessions(false)
				}
				session := llm.NewSession(testDrv, "")
				if session == nil {
					return fmt.Errorf("tester driver does not support sessions")
				}
				for i, batch := range testBatches {
					first := i == 0
					batchNum := i + 1
					stop, done := spinner(testModel, "tests", fmt.Sprintf("batch %d/%d", batchNum, len(testBatches)))
					batchFiles, m, err2 := tester.RunSessionBatch(session, first, p.Tests, batch, string(testKB))
					close(stop)
					<-done
					if err2 != nil {
						return fmt.Errorf("tester batch %d: %w", batchNum, err2)
					}
					written += writeFiles(root, batchFiles, opts.dryRun)
					for p := range batchFiles {
						touched[p] = true
					}
					ctx.SyncBack(batchFiles)
					if testMetrics.Calls == 0 {
						testMetrics = m
					} else {
						testMetrics = llm.Merge(testMetrics, m)
					}
				}
			} else {
				// Multi-batch without session reuse: loop sequentially.
				for i, batch := range testBatches {
					batchNum := i + 1
					stop, done := spinner(testModel, "tests", fmt.Sprintf("batch %d/%d", batchNum, len(testBatches)))
					batchFiles, m, err2 := tester.RunBatch(testDrv, p.Tests, batch, string(testKB))
					close(stop)
					<-done
					if err2 != nil {
						return fmt.Errorf("tester: %w", err2)
					}
					written += writeFiles(root, batchFiles, opts.dryRun)
					for p := range batchFiles {
						touched[p] = true
					}
					ctx.SyncBack(batchFiles)
					if testMetrics.Calls == 0 {
						testMetrics = m
					} else {
						testMetrics = llm.Merge(testMetrics, m)
					}
				}
			}
		}
		calls += testMetrics.Calls
		totalIn += testMetrics.TotalInputTokens()
		totalOut += testMetrics.OutputTokens
		totalCost += testMetrics.CostUSD
		status("✓", "tests done", fmt.Sprintf("%s in, %s out (%.0fs, %d calls, %d batches)",
			pipeline.FormatTokens(testMetrics.TotalInputTokens()),
			pipeline.FormatTokens(testMetrics.OutputTokens),
			time.Since(totalStart).Seconds(), testMetrics.Calls, totalTestBatches))
	}

	// ── Format ────────────────────────────────────────────────────────
	if !opts.dryRun && cfg.cfg.Command.Format != "" {
		status("✨", "formatting", "...")
		captureCmdStreaming(root, cfg.cfg.Command.Format, "  │ ")
	}

	// ── Self-correcting build ─────────────────────────────────────────
	if !opts.dryRun && cfg.cfg.Command.ProjectCheck != "" {
		fixDrv, err4 := ctx.NewDriver(*persona.ByName(persona.PhaseFix))
		fixModel := pipeline.ModelName(fixDrv)
		if err4 != nil {
			return fmt.Errorf("fix driver: %w", err4)
		}

		loopOpts := buildfix.LoopOptions{
			ProjectCheck:   cfg.cfg.Command.ProjectCheck,
			Format:         cfg.cfg.Command.Format,
			CheckFilter:    cfg.cfg.Command.CheckFilter,
			Troubleshooting: cfg.cfg.Command.TestTroubleshooting,
			DryRun:         opts.dryRun,
			OnFixWorking: func(pass int) func() {
				stop, done := spinner(fixModel, "fix", fmt.Sprintf("pass %d", pass))
				return func() { close(stop); <-done }
			},
		}
		if len(cfg.cfg.Command.Fix.KnowledgeBaseSections) > 0 {
			baseKB := loadFile(root, "knowledge-base.md")
			loopOpts.KB = shared.FilterKBSections(baseKB, cfg.cfg.Command.Fix.KnowledgeBaseSections...)
		}
		writeFn := func(files map[string]string) int {
			return writeFiles(root, files, opts.dryRun)
		}
		ls, err := buildfix.RunLoop(fixDrv, root, loopOpts, writeFn, ctx.SyncBack)
		if err != nil {
			return err
		}
		written += ls.Written
		calls += ls.Calls
		totalIn += ls.TotalIn
		totalOut += ls.TotalOut
		totalCost += ls.TotalCost
	}

	b, _ := json.Marshal(p)
	fmt.Println(string(b))

	totalBanner("", "done")
	writeTotalLine(" files", fmt.Sprintf("%d", written))
	writeTotalLine(" time", fmt.Sprintf("%.0fs", time.Since(totalStart).Seconds()))
	writeTotalLine(" tokens", fmt.Sprintf("%s in / %s out", pipeline.FormatTokens(totalIn), pipeline.FormatTokens(totalOut)))
	writeTotalLine(" cost", fmt.Sprintf("$%.4f", totalCost))
	writeTotalLine(" calls", fmt.Sprintf("%d", calls))
	separator("━")
	return nil
}

// fileWritten returns true when path exists on disk with non-zero size
// and differs from the git index version. Used for idempotency: skip
// files already generated or modified by a previous run.
func fileWritten(root, path string) bool {
	full := filepath.Join(root, path)
	info, err := os.Stat(full)
	if err != nil {
		return false
	}
	if info.Size() == 0 {
		return false
	}
	// Check git: if the file is modified in the working tree vs index,
	// it was written by a previous run.
	diff, err := runGitCmd(root, "diff", "--name-only", "--", path)
	if err == nil && strings.TrimSpace(diff) != "" {
		return true
	}
	// Untracked files were also created by a previous run.
	untracked, err := runGitCmd(root, "ls-files", "--others", "--exclude-standard", "--", path)
	if err == nil && strings.TrimSpace(untracked) != "" {
		return true
	}
	return false
}

func runGitCmd(dir string, args ...string) (string, error) {
	cmd := execCmd(dir, "git", args...)
	out, err := cmd.Output()
	return string(out), err
}

func execCmd(dir, name string, args ...string) *osexec.Cmd {
	cmd := osexec.Command(name, args...)
	cmd.Dir = dir
	return cmd
}

func writeFiles(root string, files map[string]string, dryRun bool) int {
	total := len(files)
	var w int
	for path, content := range files {
		// Normalize absolute paths to relative.
		if filepath.IsAbs(path) && strings.HasPrefix(path, root+"/") {
			if rel, err := filepath.Rel(root, path); err == nil {
				path = rel
			}
		}
		// Skip build artifacts and non-source paths.
		if shared.IsNonSourcePath(path) {
			fmt.Fprintf(os.Stderr, "  ⚠️  %s: skipping (build artifact)\n", path)
			continue
		}
		w++
		if dryRun {
			fmt.Printf("=== %s ===\n%s\n\n", path, content)
			continue
		}
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", path, err)
			continue
		}
		if info, err := os.Stat(fullPath); err == nil {
			oldSize := info.Size()
			newSize := int64(len(content))
			if oldSize > 0 && newSize < oldSize/2 {
				fmt.Fprintf(os.Stderr, "  ⚠️  %s: new content is %d bytes vs %d — skipping (likely truncated)\n", path, newSize, oldSize)
				continue
			}
		}
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", path, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "  ✓ %swrote%s %s (%d/%d)\n", cGreen, cReset, cDim+path+cReset, w, total)
	}
	return w
}


type execOpts struct {
	configPath    string
	modelOverride string
	dryRun        bool
	planFile      string
}

func parseExecFlags(args []string) (execOpts, error) {
	opts := execOpts{}
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
		case "--dry-run":
			opts.dryRun = true
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

func sessionReuseEnabled(cfg *loadedConfig, phase string) bool {
	if cfg == nil || cfg.cfg == nil {
		return false
	}
	pc, ok := cfg.cfg.LLM.Phases[phase]
	return ok && pc.SessionReuse
}

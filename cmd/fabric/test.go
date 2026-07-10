package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vitorcvargas-code/fabric/internal/architect"
	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/persona"
	"github.com/vitorcvargas-code/fabric/internal/pipeline"
	"github.com/vitorcvargas-code/fabric/internal/tester"
)

func runTest(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		printCommandHelp("test")
		return nil
	}
	opts, err := parseTestFlags(args)
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

	banner("⚡", "fabric test")
	if err := logLLMSetup(cfg, opts.modelOverride); err != nil {
		return err
	}

	p, kb, err := loadPlan(opts.planFile)
	if err != nil {
		return err
	}

	testKB := resolveKB(kb, ctx, persona.PhaseTest, "2", "5", "6", "7")

	if len(p.Tests) == 0 {
		fmt.Fprintf(os.Stderr, "  %sno tests in plan%s\n", cDim, cReset)
		return nil
	}

	totalStart := time.Now()

	// Idempotency: skip already-written test files.
	touched := map[string]bool{}
	for _, t := range p.Tests {
		if fileWritten(root, t.File) {
			touched[t.File] = true
		}
	}
	if len(touched) > 0 {
		fmt.Fprintf(os.Stderr, "  %s%d tests%s already written — skipping\n", cDim, len(touched), cReset)
	}

	fmt.Fprintf(os.Stderr, "\n  %s%d tests%s\n", cBold, len(p.Tests), cReset)
	separator("─")

	testDrv, err := ctx.NewDriver(*persona.ByName(persona.PhaseTest))
	if err != nil {
		return fmt.Errorf("tester driver: %w", err)
	}
	testModel := pipeline.ModelName(testDrv)

	totalTestBatches := (len(p.Tests) + tester.MaxTestsPerBatch - 1) / tester.MaxTestsPerBatch
	var testMetrics llm.Metrics
	var written int

	for start := 0; start < len(p.Tests); start += tester.MaxTestsPerBatch {
		end := start + tester.MaxTestsPerBatch
		if end > len(p.Tests) {
			end = len(p.Tests)
		}

		// Filter already-written tests.
		pending := p.Tests[start:end]
		var batch []architect.TestEntry
		for _, t := range pending {
			if !touched[t.File] {
				batch = append(batch, t)
			}
		}

		if len(batch) == 0 {
			continue
		}

		batchNum := start/tester.MaxTestsPerBatch + 1
		stop, done := spinner(testModel, "tests", fmt.Sprintf("batch %d/%d", batchNum, totalTestBatches))
		batchFiles, m, err := tester.RunBatch(testDrv, p.Tests, batch, string(testKB))
		close(stop)
		<-done
		if err != nil {
			return fmt.Errorf("tester: %w", err)
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

	status("✓", "tests done", fmt.Sprintf("%s in, %s out (%.0fs, %d calls, %d batches)",
		pipeline.FormatTokens(testMetrics.TotalInputTokens()),
		pipeline.FormatTokens(testMetrics.OutputTokens),
		time.Since(totalStart).Seconds(), testMetrics.Calls, totalTestBatches))

	totalBanner("", "done")
	writeTotalLine(" files", fmt.Sprintf("%d", written))
	writeTotalLine(" time", fmt.Sprintf("%.0fs", time.Since(totalStart).Seconds()))
	writeTotalLine(" tokens", fmt.Sprintf("%s in / %s out",
		pipeline.FormatTokens(testMetrics.TotalInputTokens()),
		pipeline.FormatTokens(testMetrics.OutputTokens)))
	writeTotalLine(" cost", fmt.Sprintf("$%.4f", testMetrics.CostUSD))
	separator("━")
	return nil
}

type testOpts struct {
	configPath    string
	modelOverride string
	dryRun        bool
	planFile      string
}

func parseTestFlags(args []string) (testOpts, error) {
	opts := testOpts{}
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

package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vitorcvargas-code/fabric/internal/buildfix"
	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/persona"
	"github.com/vitorcvargas-code/fabric/internal/pipeline"
	"github.com/vitorcvargas-code/fabric/internal/shared"
)

func runFix(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		printCommandHelp("fix")
		return nil
	}
	opts, err := parseFixFlags(args)
	if err != nil {
		return err
	}

	cfg, err := loadConfig(opts.configPath)
	if err != nil {
		return err
	}

	root, err := pipeline.RepoRoot()
	if err != nil {
		return err
	}

	banner("\U0001f527", "fabric fix")
	if err := logLLMSetup(cfg, opts.modelOverride); err != nil {
		return err
	}

	if cfg.cfg.Command.ProjectCheck == "" {
		return fmt.Errorf("no command.project_check configured in fabric.yaml")
	}

	fixDrv, err := llm.New(cfg.cfg.LLM.Driver, cfg.cfg.LLM.Model)
	if err != nil {
		return fmt.Errorf("fix driver: %w", err)
	}
	if vc, ok := fixDrv.(llm.VerboseConfigurable); ok {
		vc.SetVerbose(true)
	}

	_ = persona.ByName(persona.PhaseFix) // ensure persona exists

	loopOpts := buildfix.LoopOptions{
		ProjectCheck:   cfg.cfg.Command.ProjectCheck,
		Format:         cfg.cfg.Command.Format,
		CheckFilter:    cfg.cfg.Command.CheckFilter,
		Troubleshooting: cfg.cfg.Command.TestTroubleshooting,
		DryRun:         opts.dryRun,
		OnFixStart: func(pass int) {
			fmt.Fprintf(os.Stderr, "  \U0001F9E0 fix      %s%s pass %d%s\n",
				cDim+pipeline.ModelName(fixDrv)+cReset, cDim, pass, cReset)
		},
		OnFixDone: func(ok bool) {},
		OnFixWorking: func(pass int) func() {
			stop, done := spinner(pipeline.ModelName(fixDrv), "fix", fmt.Sprintf("pass %d", pass))
			return func() { close(stop); <-done }
		},
		OnBeforeFix: func(pass int) string {
			return promptForGuidance()
		},
	}
	if len(cfg.cfg.Command.Fix.KnowledgeBaseSections) > 0 {
		kb := loadFile(root, "knowledge-base.md")
		loopOpts.KB = shared.FilterKBSections(kb, cfg.cfg.Command.Fix.KnowledgeBaseSections...)
	}

	writeFn := func(files map[string]string) int {
		return writeFiles(root, files, opts.dryRun)
	}

	start := time.Now()
	ls, err := buildfix.RunLoop(fixDrv, root, loopOpts, writeFn, func(map[string]string) {})
	if err != nil {
		return err
	}

	totalBanner("", "done")
	writeTotalLine(" files", fmt.Sprintf("%d", ls.Written))
	writeTotalLine(" time", fmt.Sprintf("%.0fs", time.Since(start).Seconds()))
	writeTotalLine(" tokens", fmt.Sprintf("%s in / %s out", pipeline.FormatTokens(ls.TotalIn), pipeline.FormatTokens(ls.TotalOut)))
	writeTotalLine(" cost", fmt.Sprintf("$%.4f", ls.TotalCost))
	writeTotalLine(" calls", fmt.Sprintf("%d", ls.Calls))
	separator("━")
	return nil
}

type fixOpts struct {
	configPath    string
	modelOverride string
	dryRun        bool
}

func parseFixFlags(args []string) (fixOpts, error) {
	var opts fixOpts
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
			if strings.HasPrefix(args[i], "-") {
				return opts, fmt.Errorf("unknown flag %q", args[i])
			}
			return opts, fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	return opts, nil
}

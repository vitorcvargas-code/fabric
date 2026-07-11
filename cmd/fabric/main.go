package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const version = "0.9.0"

func main() {
	// Catch SIGINT/SIGTERM so deferred cleanups (e.g. worktree removal
	// and sync-back) always run, even on Ctrl+C.
	// The signal kills the claude subprocess; cmd.Run() then returns an
	// error, unwinding the stack through all pending defer statements.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "fabric: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("fabric %s\n", version)
		return nil
	case "init":
		return runInit(args[1:])
	case "plan":
		return runPlan(args[1:])
	case "exec":
		return runExec(args[1:])
	case "fix":
		return runFix(args[1:])
	case "test":
		return runTest(args[1:])
	case "review":
		return runReview(args[1:])
	case "linear":
		return runLinear(args[1:])
	case "help":
		if len(args) > 1 {
			printCommandHelp(args[1])
		} else {
			printUsage()
		}
		return nil
	case "--help", "-h":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\nRun 'fabric help' for usage", args[0])
	}
}

func printUsage() {
	fmt.Print(`╭──────────────────────────────────────────────────────────╮
│                                                          │
│     ███████╗ ██████╗ ██████╗      ██╗ █████╗            │
│     ██╔════╝██╔═══██╗██╔══██╗     ██║██╔══██╗           │
│     █████╗  ██║   ██║██████╔╝     ██║███████║           │
│     ██╔══╝  ██║   ██║██╔══██╗██   ██║██╔══██║           │
│     ██║     ╚██████╔╝██║  ██║╚█████╔╝██║  ██║           │
│     ╚═╝      ╚═════╝ ╚═╝  ╚═╝ ╚════╝ ╚═╝  ╚═╝           │
│                                                          │
│             ai-powered plan + execute                    │
│                                                          │
╰──────────────────────────────────────────────────────────╯

` + planHelp() + `
` + execHelp() + `
` + testHelp() + `
` + fixHelp() + `
` + reviewHelp() + `
` + linearHelp() + `
` + initHelp() + `
` + pipelineHelp())
}

func printCommandHelp(cmd string) {
	switch cmd {
	case "plan":
		fmt.Print(planHelp())
	case "exec":
		fmt.Print(execHelp())
	case "test":
		fmt.Print(testHelp())
	case "fix":
		fmt.Print(fixHelp())
	case "review":
		fmt.Print(reviewHelp())
	case "linear":
		fmt.Print(linearHelp())
	case "init":
		fmt.Print(initHelp())
	case "pipeline":
		fmt.Print(pipelineHelp())
	default:
		fmt.Printf("Unknown command %q.\n\nRun 'fabric help' to see all commands.\n", cmd)
	}
}

func planHelp() string {
	return `╭─ plan ────────────────────────────────────────────────────
│
│  Generate an implementation plan from a natural language task.
│  Pipeline: compressor → augmenter → architect → validate.
│
│  fabric plan "add a person deactivation endpoint"
│  fabric plan "add idempotency" --format json -y
│  fabric plan --issue SWACCPP-722 --out plan.json
│
│  Flags
│    --config <path>   Path to fabric.yaml
│    --model  <model>  Override architect model
│    --format <fmt>    json | yaml (default: yaml)
│    --out    <path>   Save plan to file
│    --issue  <KEY>    Fetch Jira issue as task input
│    --linear <KEY>    Fetch Linear issue as task input (needs LINEAR_API_KEY)
│    --exec-ready      Fail if plan has validation errors
│    -y, --yes         Skip approval and feedback prompts
│
│  Compressor        Fires when input >500 chars stdin, >2000 char task,
│                    or Jira JSON. Extracts signal into structured YAML.
│  Augmenter         Always runs. Extracts success criteria, asks clarifying
│                    questions (max 3) only when KB can't resolve ambiguity.
│                    User confirms success criteria — architect never invents them.
│  Architect         Generates structured JSON plan. Interactive approve/feedback
│                    loop (max 5 iterations).
│  Validate          Deterministic path checks — parent dirs exist, modify
│                    targets resolve, test paths under test dirs.
│
╰────────────────────────────────────────────────────────────`
}

func execHelp() string {
	return `╭─ exec ───────────────────────────────────────────────────
│
│  Execute a plan: codegen → tester → format → build-fix.
│  Reads plan from stdin or file. Outputs plan JSON on stdout.
│  Idempotent: skips files already written by a previous run.
│
│  fabric plan "task" -y | fabric exec
│  fabric exec plan.json
│  fabric exec plan.json --dry-run
│
│  Flags
│    --config <path>   Path to fabric.yaml
│    --model  <model>  Override executor and tester model
│    --dry-run         Print files to stdout, don't write to disk
│
│  Executor           Batched codegen (5 files per batch). Each batch sees
│                     the full plan for cross-file coordination. Uses session
│                     reuse to cache Persona+KB across batches.
│  Tester             Batched test generation (2 tests per batch). Reads
│                     production files and existing tests to copy style,
│                     fixtures, and assertion patterns.
│  Format             Runs command.format from fabric.yaml.
│  Build-fix          Self-correcting loop. Runs project_check, extracts
│                     errors via error_filter, and auto-fixes root causes.
│                     Stops when build passes, output is unchanged,
│                     or no files are produced.
│
╰───────────────────────────────────────────────────────────`
}

func testHelp() string {
	return `╭─ test ───────────────────────────────────────────────────
│
│  Run only the tester phase from a plan. Useful for iterating
│  on test prompts without re-running the full executor pipeline.
│
│  fabric test plan.json
│  fabric test plan.json --dry-run
│
│  Flags
│    --config <path>   Path to fabric.yaml
│    --model  <model>  Override tester model
│    --dry-run         Print tests to stdout, don't write files
│
╰───────────────────────────────────────────────────────────`
}

func fixHelp() string {
	return `╭─ fix ────────────────────────────────────────────────────
│
│  Run the build-fix loop standalone. Use when build is broken
│  and you want the fixer to resolve it without re-running codegen.
│  Requires command.project_check configured in fabric.yaml.
│
│  fabric fix
│  fabric fix --dry-run
│  fabric fix --model claude-sonnet-4-6
│
│  Flags
│    --config <path>   Path to fabric.yaml
│    --model  <model>  Override fix model
│    --dry-run         Show fixes without writing files
│
│  The fixer scans build output for a shared root cause — one
│  fix often resolves dozens of cascading failures. It reads
│  source files at fault locations before deciding what to change.
│
╰───────────────────────────────────────────────────────────`
}

func reviewHelp() string {
	return `╭─ review ─────────────────────────────────────────────────
│
│  Validate executed code against the plan. Mechanical checks
│  run first (guaranteed), then the reviewer LLM, then a fix loop.
│
│  fabric review plan.json
│  fabric review plan.json --dry-run
│  fabric review plan.json --skip-project-check --skip-security-check
│
│  Flags
│    --config <path>          Path to fabric.yaml
│    --model  <model>         Override reviewer model
│    --dry-run                Print fixes, don't write to disk
│    --skip-project-check     Skip project_check (build + tests)
│    --skip-security-check    Skip security_check (snyk etc.)
│
│  Mechanical checks  test_troubleshooting → project_check → security_check.
│                     Failures auto-generate error findings — broken builds
│                     always surface regardless of LLM attention.
│  Reviewer LLM       Validates against mechanical results, user-confirmed
│                     success criteria, and KB conventions. Outputs structured
│                     JSON findings with severity, file, problem, fix_where,
│                     fix_what.
│  Feedback gate      Pass 1: [a]pprove — auto-fix / [f]eedback — add yours /
│                     [s]kip — accept. In pipe mode, auto-fix proceeds
│                     immediately. Timeout configurable (default 30s).
│  Fix loop           Up to 3 iterations. Executor applies findings surgically.
│  Circuit breaker    Final pass: [r]eplan / [f]eedback — 1 more fix / [a]ccept.
│  Resume             State saved to plan.review.json after each pass.
│                     Re-running detects the file and offers resume or restart.
│  Non-interactive    With no TTY (pipe/CI/agent) every gate takes its safe
│                     default without blocking: pass 1 and the circuit breaker
│                     auto-fix/accept, the empty-fix gate skips. Safe unattended.
│
╰───────────────────────────────────────────────────────────`
}

func initHelp() string {
	return `╭─ init ───────────────────────────────────────────────────
│
│  Bootstrap fabric.yaml and build the knowledge base.
│  Auto-detects build and format commands from the project.
│
│  fabric init                Create fabric.yaml + build knowledge-base.md
│  fabric init --refresh      Regenerate fabric.yaml + rebuild knowledge base
│  fabric init --skip-index   Create fabric.yaml only (skip KB build)
│
│  Flags
│    --config <path>   Path to fabric.yaml
│    --refresh         Overwrite existing fabric.yaml and rebuild KB
│    --skip-index      Skip knowledge-base.md generation
│
╰───────────────────────────────────────────────────────────`
}

func pipelineHelp() string {
	return `╭─ pipeline ───────────────────────────────────────────────
│
│  Commands compose via pipes — exec and review read plans
│  from stdin. exec outputs the plan JSON on stdout.
│
│  Full chain:
│    fabric plan "task" -y --out plan.json && fabric exec plan.json | fabric review
│
│  Direct pipe:
│    fabric plan "task" -y | fabric exec | fabric review
│
│  Step by step:
│    fabric plan "task" -y --out plan.json
│    fabric exec plan.json
│    fabric review plan.json
│
│  fabric version     Print version
│  fabric help        Print this message
│  fabric help <cmd>  Print help for a specific command
│
╰───────────────────────────────────────────────────────────`
}

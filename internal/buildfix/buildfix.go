package buildfix

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/shared"
)

type LoopStats struct {
	Written   int
	Calls     int
	TotalIn   int
	TotalOut  int
	TotalCost llm.Cost
}

type LoopOptions struct {
	ProjectCheck    string
	Format          string
	CheckFilter     []string
	KB              []byte
	DryRun          bool
	Troubleshooting []string
	OnFixStart      func(pass int)
	OnFixDone       func(ok bool)
	OnFixWorking    func(pass int) func() // returns stop function for spinner
	OnBeforeFix     func(pass int) string // returns guidance or "" to proceed normally
}

func RunLoop(driver llm.Driver, root string, opts LoopOptions, writeFiles func(map[string]string) int, syncBack func(map[string]string)) (LoopStats, error) {
	var ls LoopStats
	if opts.ProjectCheck == "" {
		return ls, nil
	}

	fixOpts := FixOptions{KB: opts.KB}

	if len(opts.Troubleshooting) > 0 && !opts.DryRun {
		runTroubleshooting(root, opts.Troubleshooting)
	}

	if opts.Format != "" {
		runShellCmd(root, opts.Format)
	}

	checkCmd := opts.ProjectCheck
	var lastOutput string
	for fixPass := 1; ; fixPass++ {
		if fixPass > 1 {
			checkCmd = strings.Replace(checkCmd, "clean ", "", 1)
		}
		fmt.Fprintf(os.Stderr, "  ⏳ %-12s %s\n", "\033[1mbuild check\033[0m", checkCmd)
		chkOut, chkOK := captureCmd(root, checkCmd)
		if chkOK {
			statusLine("✓", "build", "passes")
			break
		}
		statusLine("✗", "build", "failing")

		buildOutput := ExtractErrorLines(chkOut, opts.CheckFilter)
		if isEmptyOrFormatOnly(buildOutput) {
			fmt.Fprintf(os.Stderr, "  %s build failing on non-fixable errors (format/lint only)\n", "⚠️")
			break
		}
		if buildOutput == lastOutput {
			fmt.Fprintf(os.Stderr, "  %s build output unchanged — no progress possible\n", "⚠️")
			break
		}
		lastOutput = buildOutput

		fmt.Fprintf(os.Stderr, "  %s fix pass %d\n", "🔧", fixPass)
		if opts.OnFixStart != nil {
			opts.OnFixStart(fixPass)
		}
		effectiveOutput := shared.DeduplicateTestFailures(buildOutput)
		if opts.OnBeforeFix != nil {
			if guidance := opts.OnBeforeFix(fixPass); guidance != "" {
				effectiveOutput = guidance + "\n\n" + effectiveOutput
			}
		}
		fixResult, fixErr := executeFixWithSpinner(driver, effectiveOutput, fixOpts, opts.OnFixWorking, fixPass)
		if opts.OnFixDone != nil {
			opts.OnFixDone(fixErr == nil)
		}
		if fixErr != nil {
			statusLine("✗", "fix", fixErr.Error())
			continue
		}
		fixed := writeFiles(fixResult.Files)
		if fixed == 0 {
			break
		}
		syncBack(fixResult.Files)
		ls.Written += fixed
		ls.Calls++
		ls.TotalIn += fixResult.Metrics.TotalInputTokens()
		ls.TotalOut += fixResult.Metrics.OutputTokens
		ls.TotalCost = ls.TotalCost.Add(fixResult.Metrics.Cost)

		if opts.Format != "" {
			runShellCmd(root, opts.Format)
		}
	}
	return ls, nil
}

func executeFixWithSpinner(driver llm.Driver, buildOutput string, opts FixOptions, onWorking func(int) func(), pass int) (FixResult, error) {
	if onWorking != nil {
		stop := onWorking(pass)
		defer stop()
	}
	return ExecuteFix(driver, buildOutput, opts)
}

func statusLine(icon, label, detail string) {
	cBold := "\033[1m"
	cDim := "\033[2m"
	cReset := "\033[0m"
	fmt.Fprintf(os.Stderr, "  %s %-12s %s%s%s\n", icon, cBold+label+cReset, cDim, detail, cReset)
}

func runShellCmd(dir, command string) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func runTroubleshooting(dir string, commands []string) {
	var filtered []string
	for _, c := range commands {
		if strings.TrimSpace(c) != "" {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}
	// Append "env" after the user's commands so we can capture any
	// exported env vars (DOCKER_HOST, TESTCONTAINERS_* from source ~/.zshrc)
	// and apply them to this process before running project_check.
	script := strings.Join(append(filtered, "env"), "\n")
	fmt.Fprintf(os.Stderr, "  ⚡ troubleshooting (%d commands via %s)\n", len(filtered), shell)
	cmd := exec.Command(shell, "-c", script)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr      // user sees troubleshooting progress
	envOut, err := cmd.Output() // captures "env" dump on stdout
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ troubleshooting shell exited with error: %v\n", err)
	}
	for _, line := range strings.Split(string(envOut), "\n") {
		if idx := strings.Index(line, "="); idx >= 0 {
			key, val := line[:idx], line[idx+1:]
			if !isEnvKey(key) {
				continue
			}
			os.Setenv(key, val)
		}
	}
}

var envKeyRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

func isEnvKey(key string) bool {
	return envKeyRe.MatchString(key)
}

func captureCmd(dir, command string) (string, bool) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = dir

	var buf strings.Builder
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		out, _ := cmd.CombinedOutput()
		return string(out), cmd.ProcessState != nil && cmd.ProcessState.Success()
	}
	if err := cmd.Start(); err != nil {
		return "", false
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		buf.WriteString(line + "\n")
		fmt.Fprintf(os.Stderr, "  │ %s\n", line)
	}
	cmd.Wait()
	ok := cmd.ProcessState != nil && cmd.ProcessState.Success()
	return buf.String(), ok
}

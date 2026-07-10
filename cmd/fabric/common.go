package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vitorcvargas-code/fabric/internal/architect"
	"github.com/vitorcvargas-code/fabric/internal/config"
	"github.com/vitorcvargas-code/fabric/internal/executor"
	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/pipeline"
	"github.com/vitorcvargas-code/fabric/internal/reviewer"
	"github.com/vitorcvargas-code/fabric/internal/shared"
	"gopkg.in/yaml.v3"
)

// ── Display ─────────────────────────────────────────────────────────────

const (
	cBold  = "\033[1m"
	cDim   = "\033[2m"
	cReset = "\033[0m"
	cGreen = "\033[32m"
	cRed   = "\033[31m"
	cCyan  = "\033[36m"
)

func status(icon, label, detail string) {
	fmt.Fprintf(os.Stderr, "  %s %-12s %s%s%s\n", icon, cBold+label+cReset, cDim, detail, cReset)
}

func separator(char string) {
	fmt.Fprintf(os.Stderr, "  %s\n", cDim+strings.Repeat(char, 58)+cReset)
}

func banner(icon, title string) {
	fmt.Fprintf(os.Stderr, "\n  %s %s\n", icon, cBold+title+cReset)
	separator("─")
}

func totalBanner(icon, title string) {
	fmt.Fprintf(os.Stderr, "\n  %s %s\n", icon, cBold+title+cReset)
	separator("━")
}

func writeTotalLine(label, value string) {
	fmt.Fprintf(os.Stderr, "  %-10s %s%s%s\n", cBold+label+cReset, cDim, value, cReset)
}

// spinner starts an animated spinner line. The label describes the current
// activity (e.g. "codegen", "tests", "review").  Close stop to clear the
// line; then read from done to know the goroutine exited.
func spinner(model, label, batch string) (stop chan struct{}, done chan struct{}) {
	if model == "" {
		model = "llm"
	}
	stop = make(chan struct{})
	done = make(chan struct{})
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		start := time.Now()
		defer t.Stop()
		defer close(done)
		frames := []string{"⣷", "⣯", "⣟", "⡿", "⢿", "⣻", "⣽", "⣾"}
		var i int
		render := func() {
			elapsed := time.Since(start).Round(time.Second)
			if batch != "" {
				fmt.Fprintf(os.Stderr, "\r\033[K  %s %-12s %s%s%s %s %s%s%s  %s%v%s",
					cCyan+"🧠"+cReset, label,
					cDim, model, cReset,
					cDim+batch+cReset,
					cGreen, frames[i%len(frames)], cReset,
					cDim, elapsed, cReset)
			} else {
				fmt.Fprintf(os.Stderr, "\r\033[K  %s %-12s %s%s%s  %s%s%s  %s%v%s",
					cCyan+"🧠"+cReset, label,
					cDim, model, cReset,
					cGreen, frames[i%len(frames)], cReset,
					cDim, elapsed, cReset)
			}
		}
		render() // first frame immediately
		i++
		for {
			select {
			case <-stop:
				fmt.Fprintf(os.Stderr, "\r\033[K")
				return
			case <-t.C:
				render()
				i++
			}
		}
	}()
	return
}

// ── Config & setup ──────────────────────────────────────────────────────

type loadedConfig struct {
	cfg        *config.Config
	configPath string
	isDefault  bool
}

func loadConfig(path string) (*loadedConfig, error) {
	if path == "" {
		wd, err := os.Getwd()
		if err != nil {
			return &loadedConfig{cfg: defaultConfig(), isDefault: true}, nil
		}
		found := config.FindConfig(wd)
		if found == "" {
			fmt.Fprintf(os.Stderr, "  %s no fabric.yaml found — using defaults\n", "⚠️")
			return &loadedConfig{cfg: defaultConfig(), isDefault: true}, nil
		}
		path = found
	}

	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	return &loadedConfig{cfg: cfg, configPath: path}, nil
}

func defaultConfig() *config.Config {
	return &config.Config{
		Project: config.ProjectConfig{Name: "unknown"},
		LLM:     config.LLMConfig{Driver: "claude-code"},
	}
}

func resolveLLM(cfg *config.Config, modelOverride string) (drv, model string) {
	drv = cfg.LLM.Driver
	model = cfg.LLM.Model
	if modelOverride != "" {
		model = modelOverride
	}
	if drv == "" {
		drv = "claude-code"
	}
	if model == "" && drv != "pi" {
		model = config.DefaultArchitectModel
	}
	return drv, model
}

func logLLMSetup(lc *loadedConfig, modelOverride string) error {
	drv, loadModel := resolveLLM(lc.cfg, modelOverride)

	if lc.configPath != "" {
		fmt.Fprintf(os.Stderr, "  %-8s %s%s%s\n", "📋 config", cDim, lc.configPath, cReset)
	}

	source := "fabric.yaml"
	if modelOverride != "" {
		source = "--model"
	} else if lc.isDefault {
		source = "default"
	}

	fmt.Fprintf(os.Stderr, "  %-8s %s%s %s(%s)%s  %s%s%s\n",
		"⚙️  llm", cBold, drv, cDim, source, cReset, cBold, loadModel, cReset)

	_, err := llm.New(drv, loadModel)
	return err
}

// ── Project helpers ────────────────────────────────────────────────────

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return os.Getwd()
		}
		dir = parent
	}
}

func isStdinTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func readStdin() (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", nil
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return "", nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func promptWithTimeout(promptText string, timeout time.Duration, defaultChoice string) string {
	if !isStdinTerminal() {
		// Pipe mode: read raw, no line editing.
		var buf [256]byte
		n, _ := os.Stdin.Read(buf[:])
		return strings.TrimSpace(string(buf[:n]))
	}
	fmt.Fprint(os.Stderr, promptText)
	type result struct{ text string }
	ch := make(chan result, 1)
	go func() {
		text := shared.ReadLine()
		select {
		case ch <- result{text}:
		default:
		}
	}()
	select {
	case r := <-ch:
		if r.text == "" {
			return defaultChoice
		}
		return r.text
	case <-time.After(timeout):
		fmt.Fprintf(os.Stderr, "\n  %stimed out — defaulting to %q%s\n", cDim, defaultChoice, cReset)
		return defaultChoice
	}
}

func promptForGuidance() string {
	input := promptWithTimeout(fmt.Sprintf("  %s[enter] continue  type feedback  [q]uit%s\n  > ", cDim, cReset), 20*time.Second, "")
	if input == "" {
		return ""
	}
	if input == "q" || input == "quit" {
		fmt.Fprintf(os.Stderr, "  %sbuild-fix aborted by user%s\n", cDim, cReset)
		os.Exit(0)
	}
	return fmt.Sprintf("## User guidance\n\n%s", input)
}

func promptNoTimeout(promptText string) string {
	return promptWithTimeout(promptText, 24*time.Hour, "")
}

// ── Plan loading ───────────────────────────────────────────────────────

func loadPlan(planFile string) (*architect.Plan, []byte, error) {
	input, err := readStdin()
	if err != nil {
		return nil, nil, fmt.Errorf("reading stdin: %w", err)
	}
	if input == "" {
		input = planFile
	}
	if input == "" {
		return nil, nil, fmt.Errorf("plan required (stdin or file path)")
	}
	if strings.HasSuffix(input, ".yaml") || strings.HasSuffix(input, ".yml") || strings.HasSuffix(input, ".json") {
		data, err := os.ReadFile(input)
		if err != nil {
			return nil, nil, fmt.Errorf("reading plan file %q: %w", input, err)
		}
		input = string(data)
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil, fmt.Errorf("empty plan input")
	}

	var p architect.Plan
	if input[0] == '{' {
		if err := json.Unmarshal([]byte(input), &p); err != nil {
			return nil, nil, fmt.Errorf("parsing plan JSON: %w", err)
		}
	} else {
		if err := yaml.Unmarshal([]byte(input), &p); err != nil {
			return nil, nil, fmt.Errorf("parsing plan YAML: %w", err)
		}
	}
	if p.Summary == "" {
		return nil, nil, fmt.Errorf("plan missing 'summary' field")
	}

	root, _ := repoRoot()
	kb := loadFile(root, "knowledge-base.md")
	return &p, kb, nil
}

func loadFile(root, path string) []byte {
	data, _ := os.ReadFile(filepath.Join(root, path))
	return data
}

func mergeMaps(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}

// resolveKB returns filtered KB content for a phase. Priority:
// 1. per-phase kb_sections in fabric.yaml (replaces defaults entirely)
// 2. global kb_sections in fabric.yaml (replaces defaults entirely)
// 3. hardcoded default sections passed as fallback
func resolveKB(kb []byte, ctx *pipeline.Context, phase string, defaultSections ...string) []byte {
	if sections := ctx.PhaseKBSections(phase); len(sections) > 0 {
		return shared.FilterKBSections(kb, sections...)
	}
	return shared.FilterKBSections(kb, defaultSections...)
}

// ── Mechanical checks ──────────────────────────────────────────────────

func runMechanical(root string, cfg *loadedConfig, dryRun bool, skipProject, skipSecurity bool) reviewer.Mechanical {
	m := reviewer.Mechanical{
		Format:       reviewer.MechResult{Pass: true},
		ProjectCheck: reviewer.MechResult{Pass: true},
		Security:     reviewer.MechResult{Pass: true},
	}
	if dryRun {
		return m
	}
	prefix := "       "
	if cfg.cfg.Command.Format != "" {
		fmt.Fprintf(os.Stderr, "  %s▶ %s%s\n", cDim, cfg.cfg.Command.Format, cReset)
		t := time.Now()
		out, ok := captureCmdStreaming(root, cfg.cfg.Command.Format, prefix)
		m.Format.Output = out
		m.Format.Pass = ok
		fmt.Fprintf(os.Stderr, "  %s  %s (%.1fs)%s\n", passLabel(ok), "format", time.Since(t).Seconds(), "")
	}
	if !skipProject && cfg.cfg.Command.ProjectCheck != "" {
		fmt.Fprintf(os.Stderr, "  %s▶ %s%s\n", cDim, cfg.cfg.Command.ProjectCheck, cReset)
		t := time.Now()
		out, ok := captureCmdStreaming(root, cfg.cfg.Command.ProjectCheck, prefix)
		m.ProjectCheck.Output = out
		m.ProjectCheck.Pass = ok
		fmt.Fprintf(os.Stderr, "  %s  %s (%.1fs)%s\n", passLabel(ok), "project", time.Since(t).Seconds(), "")
	}
	if !skipSecurity && cfg.cfg.Command.SecurityCheck != "" {
		fmt.Fprintf(os.Stderr, "  %s▶ %s%s\n", cDim, cfg.cfg.Command.SecurityCheck, cReset)
		t := time.Now()
		out, ok := captureCmdStreaming(root, cfg.cfg.Command.SecurityCheck, prefix)
		m.Security.Output = out
		m.Security.Pass = ok
		fmt.Fprintf(os.Stderr, "  %s  %s (%.1fs)%s\n", passLabel(ok), "security", time.Since(t).Seconds(), "")
	}
	return m
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
	out, _ := cmd.CombinedOutput()
	ok := cmd.ProcessState != nil && cmd.ProcessState.Success()
	return string(out), ok
}

func runShellCmd(dir, command string) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

// captureCmdStreaming runs a command, streams its output line-by-line to
// stderr with the given prefix, and returns the full captured output.
func captureCmdStreaming(dir, command, prefix string) (string, bool) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = dir

	var buf bytes.Buffer
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
		fmt.Fprintf(os.Stderr, "%s%s\n", prefix, line)
	}
	cmd.Wait()
	ok := cmd.ProcessState != nil && cmd.ProcessState.Success()
	return buf.String(), ok
}

// filterBatch deletes entries from files that are not in the current batch.
// The LLM may overproduce — this keeps only the files intended for this batch.
func filterBatch(files map[string]string, entries []executor.FileEntry) {
	batchSet := map[string]bool{}
	for _, e := range entries {
		batchSet[e.Path] = true
	}
	for path := range files {
		if !batchSet[path] {
			delete(files, path)
		}
	}
}

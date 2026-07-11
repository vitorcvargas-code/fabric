package pipeline

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vitorcvargas-code/fabric/internal/config"
	"github.com/vitorcvargas-code/fabric/internal/llm"
	"github.com/vitorcvargas-code/fabric/internal/persona"
)

const (
	cDim   = "\033[2m"
	cReset = "\033[0m"
)

// Context holds the project environment shared across all pipelines.
type Context struct {
	Root       string // worktree path (all phases run here)
	Orig       string // original repo root (for sync-back)
	Config     *config.Config
	DryRun     bool
	wtClean    func() // removes worktree
	wtBaseline string // tree hash after syncOrigToWT — syncBack diffs against this, not HEAD
}

// Close syncs changes back to the original repo and removes the worktree.
// Must be called when the pipeline exits.
func (c *Context) Close() {
	if c.wtClean != nil {
		c.wtClean()
	}
}

// SyncBack copies specific files from the worktree back to the original repo
// and advances the baseline so later syncs (including the final Close) only
// pick up net-new changes. Only the given file paths are touched — no
// full-tree scan. Call after every writeFiles so files land incrementally.
func (c *Context) SyncBack(files map[string]string) {
	if c.wtBaseline == "" || c.DryRun {
		return
	}
	for path := range files {
		copyFile(filepath.Join(c.Root, path), filepath.Join(c.Orig, path))
		// Stage in worktree so baseline tracks this file.
		exec.Command("git", "-C", c.Root, "add", "--", path).Run()
	}
	treeCmd := exec.Command("git", "-C", c.Root, "write-tree")
	if out, err := treeCmd.Output(); err == nil {
		c.wtBaseline = strings.TrimSpace(string(out))
	}
}

// NewContext creates a pipeline context, optionally with an isolated git
// worktree. When useWorktree is false, the pipeline runs directly in the repo
// root. .claude/ isolation comes from --bare mode, not the worktree.
func NewContext(cfg *config.Config, dryRun bool, useWorktree bool) (*Context, error) {
	orig, err := RepoRoot()
	if err != nil {
		return nil, err
	}

	if !useWorktree {
		return &Context{Root: orig, Orig: orig, Config: cfg, DryRun: dryRun}, nil
	}

	wtPath, baseline, wtClean, err := createWorktree(orig)
	if err != nil {
		return nil, fmt.Errorf("creating worktree: %w", err)
	}

	c := &Context{
		Root:       wtPath,
		Orig:       orig,
		Config:     cfg,
		DryRun:     dryRun,
		wtClean:    wtClean,
		wtBaseline: baseline,
	}
	return c, nil
}

// createWorktree creates a detached worktree from HEAD in /tmp.
// Returns the worktree path, a tree hash snapshot after syncing origin
// changes, and a cleanup function.
func createWorktree(orig string) (wtPath string, baseline string, cleanup func(), err error) {
	wtPath, err = os.MkdirTemp("", "fabric-wt-")
	if err != nil {
		return "", "", nil, err
	}

	cmd := exec.Command("git", "-C", orig, "worktree", "add", "--detach", wtPath, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(wtPath)
		return "", "", nil, fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}

	syncOrigToWT(orig, wtPath)

	// Snapshot the tree after syncing origin changes so syncBack can diff
	// against this baseline instead of HEAD — excluding origin's pre-existing
	// changes from the patch avoids conflicts when applying back.
	addCmd := exec.Command("git", "-C", wtPath, "add", "-A")
	addCmd.Run()
	treeCmd := exec.Command("git", "-C", wtPath, "write-tree")
	if out, err := treeCmd.Output(); err == nil {
		baseline = strings.TrimSpace(string(out))
	}

	cleanup = func() {
		syncBack(orig, wtPath, baseline)
		exec.Command("git", "-C", orig, "worktree", "remove", "--force", wtPath).Run()
		os.RemoveAll(wtPath)
	}
	return wtPath, baseline, cleanup, nil
}

// syncOrigToWT applies the original repo's working tree changes (including
// untracked files) into the worktree. This brings plan.json, exec output, etc.
func syncOrigToWT(orig, wt string) {
	// tracked changes
	cmd := exec.Command("git", "-C", orig, "diff", "--binary", "HEAD")
	if patch, err := cmd.Output(); err == nil && len(patch) > 0 {
		apply := exec.Command("git", "-C", wt, "apply", "--allow-empty")
		apply.Stdin = bytes.NewReader(patch)
		if out, err := apply.CombinedOutput(); err != nil && !alreadyApplied(wt, patch) {
			fmt.Fprintf(os.Stderr, "fabric: syncOrigToWT git apply failed: %v\n%s\n", err, string(out))
		}
	}
	// untracked files
	cmd = exec.Command("git", "-C", orig, "ls-files", "--others", "--exclude-standard", "-z")
	if out, err := cmd.Output(); err == nil && len(out) > 0 {
		for _, f := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if f == "" {
				continue
			}
			copyFile(filepath.Join(orig, f), filepath.Join(wt, f))
		}
	}
}

// syncBack copies files changed since baseline from the worktree back to the
// original repo. Uses direct file copies (not git apply) so it doesn't
// conflict with incremental SyncBack calls that already mutated the original.
// baseline is a tree hash from right after syncOrigToWT.
func syncBack(orig, wt, baseline string) {
	if baseline == "" {
		return
	}
	// Tracked files changed since baseline — copy directly, no patches.
	cmd := exec.Command("git", "-C", wt, "diff", "--name-only", "-z", baseline)
	if out, err := cmd.Output(); err == nil && len(out) > 0 {
		for _, f := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if f == "" {
				continue
			}
			copyFile(filepath.Join(wt, f), filepath.Join(orig, f))
		}
	}
	// Untracked (new) files created by the pipeline.
	cmd = exec.Command("git", "-C", wt, "ls-files", "--others", "--exclude-standard", "-z")
	if out, err := cmd.Output(); err == nil && len(out) > 0 {
		for _, f := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if f == "" {
				continue
			}
			copyFile(filepath.Join(wt, f), filepath.Join(orig, f))
		}
	}
}

// alreadyApplied returns true when the patch is already present in repo.
// It attempts a reverse dry-run; if that succeeds, the forward patch is a no-op.
func alreadyApplied(repo string, patch []byte) bool {
	rev := exec.Command("git", "-C", repo, "apply", "-R", "--check")
	rev.Stdin = bytes.NewReader(patch)
	return rev.Run() == nil
}

func copyFile(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(dst), 0755)
	os.WriteFile(dst, data, 0644)
}

// KB loads the knowledge base from the project root.
func (c *Context) KB() []byte {
	data, _ := os.ReadFile(filepath.Join(c.Root, "knowledge-base.md"))
	return data
}

// InjectContent resolves inject paths from global + per-phase config and
// returns concatenated rule/skill content for injection into the LLM prompt.
// Global injects are resolved first, per-phase injects are appended. Paths are
// resolved from the original repo (Orig) since worktree isolation removes
// .claude/ files. Each path is a prefix under .claude/ — fabric resolves
// the file: "rules/security" → .claude/rules/security/RULE.md,
// "skills/observability" → .claude/skills/observability/SKILL.md.
func (c *Context) InjectContent(phase string) string {
	injects := c.Config.LLM.Inject
	if pc, ok := c.Config.LLM.Phases[phase]; ok {
		injects = append(injects, pc.Inject...)
	}
	if len(injects) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, p := range injects {
		label := p
		var data []byte
		// Try RULE.md first (rules/ prefix), then SKILL.md (skills/ prefix).
		data, _ = os.ReadFile(filepath.Join(c.Orig, ".claude", p, "RULE.md"))
		if data == nil {
			data, _ = os.ReadFile(filepath.Join(c.Orig, ".claude", p, "SKILL.md"))
		}
		if data != nil {
			sb.WriteString(fmt.Sprintf("### %s\n\n", label))
			sb.Write(data)
			sb.WriteString("\n\n")
		}
	}
	if sb.Len() == 0 {
		return ""
	}
	return "## Injected constraints\n\n" +
		"**Precedence:** When injected constraints (skills/rules) conflict with the KB, " +
		"injected constraints take precedence. Rules and skills override KB patterns.\n\n" +
		sb.String()
}

// PhaseKBSections returns the kb_sections for a phase, merging global defaults
// with per-phase overrides. Per-phase takes full precedence when set (it replaces
// the global list, not merges). Returns nil when neither is configured (meaning
// "use the full KB").
func (c *Context) PhaseKBSections(phase string) []string {
	global := c.Config.LLM.KBSections
	if pc, ok := c.Config.LLM.Phases[phase]; ok && len(pc.KBSections) > 0 {
		return pc.KBSections
	}
	return global
}

// NewDriver creates an LLM driver configured for the given persona,
// with fabric.yaml phase overrides applied.
func (c *Context) NewDriver(p persona.Persona) (llm.Driver, error) {
	model := p.Model
	thinking := p.Thinking
	timeout := time.Duration(p.Timeout) * time.Second

	if pc, ok := c.Config.LLM.Phases[p.Name]; ok {
		if pc.Model != "" {
			model = pc.Model
		}
		if pc.Thinking != "" {
			thinking = pc.Thinking
		}
		if pc.TimeoutSeconds > 0 {
			timeout = time.Duration(pc.TimeoutSeconds) * time.Second
		}
	}
	if model == "" {
		model = c.Config.LLM.Model
	}

	drvName := c.Config.LLM.Driver
	if drvName == "" {
		drvName = "claude-code"
	}

	d, err := llm.New(drvName, model)
	if err != nil {
		return nil, err
	}

	if tc, ok := d.(llm.TimeoutConfigurable); ok && timeout > 0 {
		tc.SetTimeout(timeout)
	}
	if tc, ok := d.(llm.ThinkingConfigurable); ok {
		tc.SetThinking(thinking)
	}
	if isc, ok := d.(llm.IsolateSessionConfigurable); ok {
		isc.SetIsolateSessions(true)
	}
	if bc, ok := d.(llm.BareConfigurable); ok && c.Config.LLM.UseBare != nil {
		bc.SetBare(*c.Config.LLM.UseBare)
	}

	if needsWorkDir(p.Name) {
		if wc, ok := d.(llm.WorkDirConfigurable); ok {
			wc.SetWorkDir(c.Root)
		}
		if vc, ok := d.(llm.VerboseConfigurable); ok {
			vc.SetVerbose(true)
		}
	}

	return d, nil
}

// needsWorkDir returns true for all phases — the worktree is always used
// as CWD so claude never auto-discovers .claude/ project rules.
func needsWorkDir(phase string) bool { return true }

// RepoRoot finds the git repository root from the current directory.
func RepoRoot() (string, error) {
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

// LoadConfig loads fabric.yaml, falling back to defaults.
func LoadConfig(path string) (*config.Config, error) {
	if path == "" {
		wd, _ := os.Getwd()
		found := config.FindConfig(wd)
		if found == "" {
			return &config.Config{
				Project: config.ProjectConfig{Name: "unknown"},
				LLM:     config.LLMConfig{Driver: "claude-code"},
			}, nil
		}
		path = found
	}
	return config.Load(path)
}

// Tail truncates s to the last n characters.
func Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "...(truncated)\n" + s[len(s)-n:]
}

// FormatTokens formats a token count for display.
func FormatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// LoadFile reads a file relative to root. Returns nil on error.
func LoadFile(root, path string) []byte {
	data, _ := os.ReadFile(filepath.Join(root, path))
	return data
}

// ShortPath shows the last 3 segments of a path.
func ShortPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 4 {
		return p
	}
	return ".../" + strings.Join(parts[len(parts)-3:], "/")
}

// ModelName returns the requested model name from a driver.
func ModelName(d llm.Driver) string {
	if rm, ok := d.(interface{ RequestedModel() string }); ok {
		if m := rm.RequestedModel(); m != "" {
			return m
		}
	}
	return "llm"
}

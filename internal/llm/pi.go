package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PiDriver invokes the `pi` CLI as a subprocess in print mode.
// Session output is captured to a temp JSONL file and parsed for usage metrics.
// When a CostEstimator is set, token counts come from the JSONL but cost is
// computed via the estimator (e.g. provider rate cards) instead of relying on
// pi's embedded cost reporting.
type PiDriver struct {
	model         string
	timeout       time.Duration
	costEstimator CostEstimator
}

// NewPiDriver creates a driver that calls `pi -p --no-context-files`.
func NewPiDriver(model string) *PiDriver {
	return &PiDriver{model: model, timeout: 5 * time.Minute}
}

// SetTimeout caps how long a single pi subprocess may run.
func (b *PiDriver) SetTimeout(d time.Duration) {
	if d > 0 {
		b.timeout = d
	}
}

// SetCostEstimator configures an external cost estimator. When set, cost is
// computed from token counts using the estimator instead of pi's JSONL cost.
func (b *PiDriver) SetCostEstimator(e CostEstimator) {
	b.costEstimator = e
}

// Call sends a prompt to pi via stdin and returns the text response with
// usage metrics extracted from the session JSONL file.
func (b *PiDriver) Call(prompt string, contextFiles ...ContextFile) (Response, error) {
	sessionDir, err := os.MkdirTemp("", "fabric-pi-session-*")
	if err != nil {
		return Response{}, fmt.Errorf("creating session dir: %w", err)
	}
	defer os.RemoveAll(sessionDir)

	sessionID := newSessionID()

	args := []string{"-p", "--no-context-files", "--session-dir", sessionDir, "--session-id", sessionID}
	// Only pass --model for fully-qualified provider/model pairs (e.g. "bedrock/deepseek-v4").
	// Bare names like "deepseek" or "haiku" are ambiguous in pi's model registry
	// and may resolve to a different provider than intended. Leave empty → pi uses
	// its configured default from ~/.pi/agent/settings.json.
	if strings.Contains(b.model, "/") {
		args = append(args, "--model", b.model)
	}

	if len(contextFiles) > 0 {
		tmpDir, err := os.MkdirTemp("", "fabric-pi-ctx-*")
		if err != nil {
			return Response{}, fmt.Errorf("creating context dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		for _, cf := range contextFiles {
			path := filepath.Join(tmpDir, cf.Name)
			if err := os.WriteFile(path, []byte(cf.Content), 0600); err != nil {
				return Response{}, fmt.Errorf("writing context file %s: %w", cf.Name, err)
			}
			args = append(args, "--append-system-prompt", path)
		}
	}

	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	var cancel context.CancelFunc
	if b.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "pi", args...)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return Response{}, fmt.Errorf(
				"pi timed out after %s — API may be blocked or the model is stuck; retry",
				b.timeout.Round(time.Second),
			)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = "(no output)"
		}
		return Response{}, fmt.Errorf("pi exited with error: %s", msg)
	}
	elapsed := time.Since(start)

	// Parse session file for usage metrics. When a cost estimator is
	// configured (e.g. provider rate cards), cost is computed from token
	// counts instead of reading pi's embedded cost reporting.
	metrics := parseSessionMetrics(sessionDir, sessionID, b.model, elapsed, b.costEstimator)

	text := strings.TrimSpace(stdout.String())

	return Response{
		Text:    text,
		Metrics: metrics,
	}, nil
}

// parseSessionMetrics reads the JSONL session file and extracts token/cost
// usage from assistant messages. When estimator is non-nil, cost is computed
// via the estimator (rate cards) instead of pi's embedded cost reporting.
func parseSessionMetrics(sessionDir, sessionID, requestedModel string, elapsed time.Duration, estimator CostEstimator) Metrics {
	// pi names sessions as <timestamp>_<uuid>.jsonl under a cwd-slug directory.
	// We navigate the session dir to find the file.
	sessionFile := findSessionFile(sessionDir, sessionID)
	if sessionFile == "" {
		return Metrics{
			DurationMs:     elapsed.Milliseconds(),
			Calls:          1,
			RequestedModel: requestedModel,
			Model:          requestedModel,
		}
	}

	f, err := os.Open(sessionFile)
	if err != nil {
		return Metrics{
			DurationMs:     elapsed.Milliseconds(),
			Calls:          1,
			RequestedModel: requestedModel,
			Model:          requestedModel,
		}
	}
	defer f.Close()

	var (
		inputTokens              int
		cacheCreationInputTokens int
		cacheReadInputTokens     int
		outputTokens             int
		costUSD                  float64
		actualModel              string
	)

	scanner := bufio.NewScanner(f)
	// Increase buffer for large session lines.
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		var entry struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "message" || len(entry.Message) == 0 {
			continue
		}

		var msg struct {
			Role     string `json:"role"`
			Provider string `json:"provider"`
			Model    string `json:"model"`
			Usage    struct {
				Input      int `json:"input"`
				Output     int `json:"output"`
				CacheRead  int `json:"cacheRead"`
				CacheWrite int `json:"cacheWrite"`
				Cost       struct {
					Input      float64 `json:"input"`
					Output     float64 `json:"output"`
					CacheRead  float64 `json:"cacheRead"`
					CacheWrite float64 `json:"cacheWrite"`
					Total      float64 `json:"total"`
				} `json:"cost"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(entry.Message, &msg); err != nil {
			continue
		}
		if msg.Role != "assistant" || msg.Usage.Input == 0 && msg.Usage.Output == 0 {
			continue
		}

		inputTokens += msg.Usage.Input
		cacheCreationInputTokens += msg.Usage.CacheWrite
		cacheReadInputTokens += msg.Usage.CacheRead
		outputTokens += msg.Usage.Output

		// Accumulate cost from JSONL as fallback; estimator takes priority below.
		costUSD += msg.Usage.Cost.Total
		if msg.Model != "" {
			actualModel = msg.Model
		}
	}

	// When a cost estimator is configured, override the JSONL cost with the
	// rate-card computation. This gives provider-billed accuracy for pi sessions.
	if estimator != nil {
		modelForCost := actualModel
		if modelForCost == "" {
			modelForCost = requestedModel
		}
		costUSD = estimator.EstimateCost(modelForCost, inputTokens, cacheCreationInputTokens, cacheReadInputTokens, outputTokens)
	}

	return Metrics{
		DurationMs:               elapsed.Milliseconds(),
		InputTokens:              inputTokens,
		CacheCreationInputTokens: cacheCreationInputTokens,
		CacheReadInputTokens:     cacheReadInputTokens,
		OutputTokens:             outputTokens,
		CostUSD:                  costUSD,
		Calls:                    1,
		RequestedModel:           requestedModel,
		Model:                    actualModel,
	}
}

// findSessionFile walks sessionDir to find the JSONL file matching sessionID.
// pi creates subdirectories by cwd-slug, so we need to recurse.
func findSessionFile(dir, sessionID string) string {
	var found string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".jsonl") && strings.Contains(d.Name(), sessionID) {
			found = path
		}
		return nil
	})
	return found
}



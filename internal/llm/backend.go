package llm

import "time"

// Metrics holds usage data returned by the LLM driver after a call.
type Metrics struct {
	DurationMs               int64
	InputTokens              int // uncached prompt tokens
	CacheCreationInputTokens int // prompt cache write
	CacheReadInputTokens     int // prompt cache read
	OutputTokens             int
	CostUSD                  float64
	Calls                    int // number of LLM subprocesses summed into this metric
	RequestedModel           string // --model value passed to the driver
	Model                    string // model reported by the provider after the call
	SessionID                string // claude session id when using session resume
}

// Response is returned by every Driver call.
type Response struct {
	Text    string
	Metrics Metrics
}

// Driver is the abstraction over all LLM providers. Fabric never imports
// provider-specific SDKs; all interaction goes through this interface via a
// subprocess or adapter.
type Driver interface {
	// Call sends a prompt to the LLM. Optional context files are written to
	// temp files and passed as system prompt appendages to avoid prompt size limits.
	Call(prompt string, contextFiles ...ContextFile) (Response, error)
}

// TimeoutConfigurable is implemented by drivers that support per-call
// timeouts. ConfigureLLMTimeout in plan.go uses this instead of type-asserting
// to a specific driver.
type TimeoutConfigurable interface {
	SetTimeout(d time.Duration)
}

// IsolateSessionConfigurable is implemented by drivers that can force a
// fresh session ID per call to prevent cache stacking across pipeline stages.
type IsolateSessionConfigurable interface {
	SetIsolateSessions(v bool)
}

// WorkDirConfigurable is implemented by drivers that support setting a
// working directory for the subprocess. When set, tools like Read/Bash
// resolve relative paths against this directory.
type WorkDirConfigurable interface {
	SetWorkDir(dir string)
}

// ThinkingConfigurable is implemented by drivers that support controlling
// extended thinking per call.
type ThinkingConfigurable interface {
	SetThinking(v string)
}

// VerboseConfigurable is implemented by drivers that can stream live
// subprocess output (tool calls, thinking) to stderr for progress feedback.
type VerboseConfigurable interface {
	SetVerbose(v bool)
}

// CostEstimatorConfigurable is implemented by drivers that accept an
// external cost estimator (e.g. pi with provider rate cards).
type CostEstimatorConfigurable interface {
	SetCostEstimator(e CostEstimator)
}

// SessionDriver is implemented by llm drivers that support session resume
// across multiple calls, reusing uploaded context without re-sending it.
// Drivers that don't support sessions are handled via the normal Call path.
type SessionDriver interface {
	Driver
	// BootstrapCall starts a new session with prompt plus context files.
	BootstrapCall(prompt string, sessionID string, contextFiles ...ContextFile) (Response, error)
	// ResumeCall continues an existing session with a follow-up prompt only.
	ResumeCall(prompt string, sessionID string) (Response, error)
}

// ContextFile is a block of context passed alongside the prompt but delivered
// via a separate mechanism (temp file, system prompt append) to avoid prompt
// size limits.
type ContextFile struct {
	Name    string
	Content string
}

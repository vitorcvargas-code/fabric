package llm

import (
	"crypto/rand"
	"fmt"
)

// callOptions is internal to backends that need session/resume flags.
type callOptions struct {
	sessionID string
	resume    bool
}

// Session reuses a single provider conversation across multiple calls so
// static context is not re-uploaded on every layer.
type Session struct {
	driver      SessionDriver
	id           string
	bootstrapped bool
}

// NewSession wraps a SessionDriver with a persistent session ID.
// Returns nil if the backend does not implement SessionDriver.
func NewSession(drv Driver, sessionID string) *Session {
	sb, ok := drv.(SessionDriver)
	if !ok {
		return nil
	}
	if sessionID == "" {
		sessionID = newSessionID()
	}
	return &Session{driver: sb, id: sessionID}
}

func (s *Session) ID() string {
	return s.id
}

// CallBootstrap starts the session with static context files attached once.
func (s *Session) CallBootstrap(prompt string, contextFiles ...ContextFile) (Response, error) {
	s.bootstrapped = true
	return s.driver.BootstrapCall(prompt, s.id, contextFiles...)
}

// CallResume continues the session with a follow-up prompt only (no context files).
func (s *Session) CallResume(prompt string) (Response, error) {
	if !s.bootstrapped {
		return Response{}, fmt.Errorf("session %s: bootstrap required before resume", s.id)
	}
	return s.driver.ResumeCall(prompt, s.id)
}

func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

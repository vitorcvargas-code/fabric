package architect

import (
	"errors"
	"strings"
	"testing"

	"github.com/vitorcvargas-code/fabric/internal/llm"
)

// fakeSessionDriver records what ResumeSession sends and returns a canned plan.
type fakeSessionDriver struct {
	resumePrompt    string
	resumeSessionID string
	respText        string
	resumeErr       error
}

func (f *fakeSessionDriver) Call(prompt string, _ ...llm.ContextFile) (llm.Response, error) {
	return llm.Response{Text: f.respText}, nil
}

func (f *fakeSessionDriver) BootstrapCall(prompt, sessionID string, _ ...llm.ContextFile) (llm.Response, error) {
	return llm.Response{Text: f.respText}, nil
}

func (f *fakeSessionDriver) ResumeCall(prompt, sessionID string) (llm.Response, error) {
	f.resumePrompt = prompt
	f.resumeSessionID = sessionID
	return llm.Response{Text: f.respText, Metrics: llm.Metrics{SessionID: sessionID}}, f.resumeErr
}

func TestResumeSession_SendsFeedbackAgainstSessionID(t *testing.T) {
	f := &fakeSessionDriver{respText: `{"summary": "revised plan"}`}

	res, err := ResumeSession(f, "sess-123", "make it idempotent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Plan.Summary != "revised plan" {
		t.Errorf("plan summary = %q, want revised plan", res.Plan.Summary)
	}
	if f.resumeSessionID != "sess-123" {
		t.Errorf("resumed session id = %q, want sess-123", f.resumeSessionID)
	}
	if !strings.Contains(f.resumePrompt, "make it idempotent") {
		t.Errorf("feedback not forwarded to provider; prompt = %q", f.resumePrompt)
	}
}

func TestResumeSession_PropagatesResumeErrorForColdFallback(t *testing.T) {
	f := &fakeSessionDriver{resumeErr: errors.New("session expired")}

	if _, err := ResumeSession(f, "gone", "x"); err == nil {
		t.Fatal("expected an error when the provider can no longer resume the session")
	}
}

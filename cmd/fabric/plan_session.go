package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vitorcvargas-code/fabric/internal/architect"
)

// planSession is the sidecar written next to a plan file (plan.json →
// plan.session.json), mirroring the review-state sidecar convention. It lets a
// later cold `fabric plan --in <plan> --feedback` process resume the architect
// conversation — or, if the provider session is gone, rebuild a fresh one from
// the original task — without re-running the compressor and augmenter.
type planSession struct {
	SessionID       string   `json:"session_id"`
	Model           string   `json:"model"`
	OriginalTask    string   `json:"original_task"`
	SuccessCriteria []string `json:"success_criteria,omitempty"`
}

func planSessionPath(planFile string) string {
	ext := filepath.Ext(planFile)
	return strings.TrimSuffix(planFile, ext) + ".session.json"
}

func writePlanSession(planFile string, s planSession) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(planSessionPath(planFile), data, 0644)
}

func loadPlanSession(planFile string) (planSession, bool) {
	data, err := os.ReadFile(planSessionPath(planFile))
	if err != nil {
		return planSession{}, false
	}
	var s planSession
	if err := json.Unmarshal(data, &s); err != nil {
		return planSession{}, false
	}
	return s, true
}

// resolveFeedback returns the feedback text from --feedback or --feedback-file.
func resolveFeedback(opts planOpts) (string, error) {
	if opts.feedbackFile != "" {
		data, err := os.ReadFile(opts.feedbackFile)
		if err != nil {
			return "", fmt.Errorf("reading feedback file %q: %w", opts.feedbackFile, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if strings.TrimSpace(opts.feedback) == "" {
		return "", fmt.Errorf("--in requires --feedback or --feedback-file")
	}
	return strings.TrimSpace(opts.feedback), nil
}

// warmResumeTask is sent when the provider still holds the prior conversation:
// the plan is already in the session, so only the feedback is needed.
func warmResumeTask(feedback string) string {
	return "Revise your previous plan to address this feedback:\n\n" + feedback
}

// coldRebuildTask is sent when there is no resumable session: it re-anchors the
// architect with the original task and the prior plan JSON before the feedback.
func coldRebuildTask(sess planSession, priorPlan *architect.Plan, feedback string) string {
	base := sess.OriginalTask
	if strings.TrimSpace(base) == "" {
		base = "(original task unavailable — infer intent from the prior plan)"
	}
	priorJSON, _ := json.MarshalIndent(priorPlan, "", "  ")
	return fmt.Sprintf("%s\n\nYou previously produced this plan:\n%s\n\nRevise it to address this feedback:\n%s",
		base, string(priorJSON), feedback)
}

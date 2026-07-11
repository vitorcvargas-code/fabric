# ADR 012: Revise a Plan From Feedback Without Re-Planning

**Date**: 2026-07-11
**Status**: Accepted
**Context**: `fabric/cmd/fabric/plan.go`, `fabric/cmd/fabric/plan_session.go`, `fabric/internal/architect/architect.go` — the `plan` command and architect session handling. Companion to ADR 005 (session reuse).

## Problem

The architect's approve/feedback loop lived entirely behind an interactive TTY
prompt (`[a]pprove [f]eedback [q]uit`). An agent or script driving `fabric` with
`--yes` skips the loop and gets no way to feed back on a plan and re-plan as a
discrete, non-interactive step. The only revision path re-ran the whole pipeline
(compressor + augmenter + architect), paying for context and clarifying-question
work that had already been done.

## Decision

Add `fabric plan --in <plan.json> --feedback <text>` (or `--feedback-file`) that
revises an existing plan in one non-interactive step, skipping the compressor and
augmenter.

### Cross-process session resume

`SessionRunner.Resume` reuses an **in-memory** `llm.Session`, which a fresh
`--in` process does not have. The Claude Code backend, however, resumes by id
across processes: bootstrap registers `claude --session-id <uuid>`, and a later
call passes `claude --resume <uuid>`. The authoritative, resumable id is returned
on `Metrics.SessionID` on both the session and non-session architect paths.

So `plan` persists a **sidecar** next to the plan file, mirroring the
`plan.review.json` convention:

```
plan.json → plan.session.json
{ "session_id", "model", "original_task", "success_criteria" }
```

A new `architect.ResumeSession(drv, sessionID, task)` calls the driver's
`ResumeCall` directly (no live `Session`), sending only the feedback. The
provider still holds the persona, KB, and manual from the original bootstrap, so
they are not re-sent — the same token win as ADR 005, now across processes.

### Warm vs cold

`reviseWithFeedback` resumes (warm) only when the driver supports sessions, the
sidecar has a session id, **and** the recorded model equals the current model —
a resumed session is only correct if it is the same conversation that uploaded
the context and the driver config matches. Otherwise, or if `ResumeCall` fails
because the provider pruned the session, it falls back (cold) to a fresh
`architect.Run` seeded with the original task and the prior plan JSON. The cold
fallback is mandatory, not optional: provider sessions expire.

## Consequences

- Agents can do read → critique → revise → exec as discrete steps.
- No change to the interactive `plan` path; the sidecar is additive.
- The revised plan is written to `--out`, or back to the `--in` file in place.
- Tests: `ResumeSession` forwards feedback against the session id and surfaces a
  resume error for the cold fallback (`internal/architect`); the sidecar
  round-trips and `coldRebuildTask` carries task + prior plan + feedback
  (`cmd/fabric`).

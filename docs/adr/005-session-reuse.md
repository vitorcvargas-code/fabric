# ADR 005: Session Reuse Across Batches

**Date**: 2026-06-24
**Status**: Accepted
**Context**: `fabric/internal/llm/session.go`, `fabric/internal/executor/executor.go`, `fabric/internal/tester/tester.go`

## Problem

Multi-batch executor and tester phases upload the same static context (persona prompt + KB sections, ~20K tokens) on every single batch call. With 3 executor batches, the same 20K tokens are processed and billed 3 times — 40K tokens of input that could be cached.

The `SessionDriver` interface (with `BootstrapCall` and `ResumeCall`) already existed in the codebase but was never used — every driver had `SetIsolateSessions(true)`, forcing a fresh session per call.

## Decision: Bootstrap once, resume for subsequent batches

For multi-batch plans:

1. Disable per-call session isolation (`SetIsolateSessions(false)`)
2. Create a session via `llm.NewSession(driver, "")`
3. First batch: `BootstrapCall(prompt)` — uploads persona + KB
4. Subsequent batches: `ResumeCall(prompt)` — sends only the batch-specific prompt

The provider caches the static prefix (persona + KB) after the bootstrap call. Resume calls hit the cache, saving ~20K input tokens per additional batch.

For single-batch plans, the normal `Call()` path is used — no session overhead.

## Expected savings

| Scenario | Without sessions | With sessions | Savings |
|---|---|---|---|
| 3 exec batches (15 files) | 60K input | 20K + 2 × ~2K | ~36K tokens |
| 5 exec batches (25 files) | 100K input | 20K + 4 × ~2K | ~72K tokens |
| 2 test batches (4 tests) | 40K input | 20K + 1 × ~1K | ~19K tokens |

Actual savings depend on batch-specific prompt size (all-files list + specs).

## Implementation note

The session approach is used **only** for executor and tester batches — these are the only phases with multi-call patterns within the same phase. Architect, reviewer, and fix phases use single calls per iteration and get no benefit from session reuse.

The fix goroutine pool (`ExecuteFix`) uses the `Call()` path because each goroutine fixes a different file with different content — the context is never shared, and session reuse wouldn't help.

## References

- `fabric/internal/llm/session.go` — `Session`, `BootstrapCall`, `ResumeCall`
- `fabric/internal/executor/executor.go` — `ExecuteBatchesSession()`, `buildBatchPrompt()`
- `fabric/internal/tester/tester.go` — `RunBatchesSession()`, `buildTestBatchPrompt()`
- `fabric/cmd/fabric/exec.go` — batches pre-computed, session path for multi-batch only

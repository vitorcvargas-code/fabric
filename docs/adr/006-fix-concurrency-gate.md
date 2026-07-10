# ADR 006: Fix Concurrency Gate

**Date**: 2026-06-24
**Status**: Accepted
**Context**: `fabric/internal/executor/executor.go` — `ExecuteFix()`

## Problem

`ExecuteFix` spawns one goroutine (and thus one LLM subprocess) per broken file. A large refactor that fails across 30+ files would spawn 30+ concurrent `claude` subprocesses, each making independent API calls. This can hit provider rate limits, exhaust system resources, or cause cascading failures.

## Decision: Semaphore with max 5 concurrent fix calls

A buffered channel `make(chan struct{}, 5)` gates goroutine entry:

```go
sem := make(chan struct{}, maxFixConcurrency)
for filePath, errors := range byFile {
    go func(fp string, errs []string) {
        sem <- struct{}{}    // acquire
        defer func() { <-sem }() // release
        defer wg.Done()
        // ... fix logic
    }(filePath, errors)
}
```

At most 5 fix LLM calls run concurrently. Additional files queue behind the semaphore.

### Rejected: Sequential fix (one file at a time)

Too slow. A 5-file build failure would take 5× longer when the fixes are independent.

### Rejected: Configurable via fabric.yaml

The optimal concurrency level depends on the LLM provider's rate limits, not the project. A per-project config would be the wrong abstraction. The constant is tuned for claude-code rate limits and provides headroom for parallel executor batches.

## References

- `fabric/internal/executor/executor.go` — `maxFixConcurrency`, `ExecuteFix()`
- `fabric/cmd/fabric/exec.go` — `runBuildFixLoop()` calls `ExecuteFix`

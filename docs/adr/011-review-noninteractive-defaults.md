# ADR 011: Review Gates Take Safe Defaults When Non-Interactive

**Date**: 2026-07-11
**Status**: Accepted
**Context**: `fabric/cmd/fabric/review.go`, `fabric/cmd/fabric/common.go` — the human gates in the review fix loop

## Problem

The review fix loop has three human gates: the first-pass feedback gate, the
circuit breaker (max iterations reached), and the empty-fix recovery gate. Only
the first-pass gate guarded its prompt with `isStdinTerminal()`. The other two
called `promptNoTimeout`, which in non-TTY mode did a bare blocking
`os.Stdin.Read` with **no timeout** (the 24h timeout only applies on the TTY
path). If stdin was inherited and held open — a pipe with a live writer, a
parent process, CI, or an agent — `review` could block indefinitely.

`fabric exec plan.json | fabric review` was therefore only *accidentally* safe
unattended: it relied on stdin reaching EOF, not on any explicit contract.

## Decision

Every review gate takes a safe default without reading stdin when the session is
non-interactive. A single helper expresses the rule:

```go
func choiceOrDefault(isTerminal func() bool, promptText, def string) string {
    if !isTerminal() {
        return def
    }
    return promptNoTimeout(promptText)
}
```

`isTerminal` is injected rather than calling `isStdinTerminal` directly so the
decision is unit-testable without touching `os.Stdin`. Defaults per gate:

| Gate | Non-interactive default | Effect |
|------|-------------------------|--------|
| First-pass feedback | `""` | fall through to auto-fix |
| Circuit breaker | `""` | accept & exit |
| Empty-fix recovery | `"s"` | skip & exit cleanly |

This makes "safe to call unattended" a guarantee rather than an accident.

## Also fixed

`promptWithTimeout`'s non-TTY branch read a fixed `[256]byte` buffer, truncating
longer piped feedback. It now reads a full line via `bufio.Reader.ReadString`.

## Consequences

- No behavioral change for interactive users — the prompts render as before.
- Non-interactive runs are deterministic and cannot hang on a gate.
- New first tests in `cmd/fabric` (`common_test.go`) cover the default selection
  and the no-truncation guarantee.

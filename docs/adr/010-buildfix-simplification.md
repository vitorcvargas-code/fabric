# ADR 010: Build-Fix Simplification — Pure Grep, No Heuristics

**Date**: 2026-06-28
**Status**: Accepted
**Supersedes**: ADR 008 (build-fix enhancements — the approach, not the intent)
**Context**: `fabric/internal/buildfix/` — extracted from `internal/executor/` in db07e302

## Problem

ADR 008 introduced `isErrorLine` — a language-aware error classifier with 15+ rules covering Kotlin, Java, TypeScript, C/C++, Rust, Go, and generic linter patterns. Combined with `groupErrorsByFile`, `extractFilePath`, and `buildBasenameIndex`, the pipeline attempted to:

1. Detect error lines via language-specific heuristics
2. Resolve file paths from compiler output, stack traces, and bare filenames
3. Group errors by file for targeted per-file LLM fixes

This failed in practice:

- **`isErrorLine` gaps**: test method failures (`SomeIT > test() FAILED`), `AssertionError`, and `IllegalStateException` stack frames weren't recognized — they passed through to the filter fallback
- **`extractFilePath` dead ends**: stack traces referenced library classes (`CoroutineDebugging.kt`, `ExceptionFactory.java`) that don't exist in the project. Bare filenames from test output (`DeletePersonIntegrationTest.kt:53` — no directory path) couldn't be resolved without a full filesystem walk
- **`groupErrorsByFile` returned empty**: when no file paths resolved, the entire fix pass failed with `"no file paths found in build output"` — the LLM never saw the errors
- **Heuristic maintenance**: every language added new edge cases. The `FAILED` check had to exclude `BUILD FAILED`, `> Task`, and standalone `FAILED` before it could safely match test names

The fundamental mistake: **fabric was trying to understand the errors**. The LLM is better at that.

## Decision

### Replace `isErrorLine` + `groupErrorsByFile` with `grep -C 5`

`ExtractErrorLines` now applies user-supplied filter patterns directly to the build output, like `grep`, with ±5 context lines around each match. No language-specific heuristics. No file path resolution.

```go
func ExtractErrorLines(output string, filters []string) string
```

Each filter is compiled as a regex. Lines matching any filter are kept, plus 5 lines of surrounding context. Non-adjacent match groups are separated by `---`.

### `error_filter` is now a list

```yaml
command:
  error_filter:
    - ") FAIL"
    - Exception
```

Multiple patterns are ORed together. Each can be a literal string (auto-quoted for regex) or a full regex. This is language-agnostic — any build tool's failure format can be caught.

### `ExecuteFix` is a single bulk LLM call

No per-file splitting. No path resolution. No file index. The filtered output is passed directly to the LLM:

```
## Build output with errors
```
<filtered grep output>
```

Fix the reported issues. Output each changed file as:
=== FILE: path/to/file ===
```
content
```
```

The LLM reads the error context and decides which files to modify. This is what LLMs are good at — pattern matching and context inference.

### Removed components

| Component | Reason |
|-----------|--------|
| `isErrorLine` (15+ rules) | Replaced by user-supplied filter patterns |
| `groupErrorsByFile` | No longer needed — single bulk call |
| `extractFilePath` | No longer needed — LLM finds files |
| `buildBasenameIndex` | No longer needed |
| `pathColLineRe` | No longer needed |
| `fixSingleFile` (per-file fix) | Replaced by `fixBulk` |
| `fixByFile` (concurrent per-file) | Replaced by single call |

### What stays

- `isEmptyOrFormatOnly` — trivially checks if filtered output is non-empty
- `contextLines = 5` — grep-style context window
- `RunLoop` — the 5-pass fix loop with troubleshooting, format, check, fix cycle
- `runTroubleshooting` — env var capture with `isEnvKey` validation
- `FixOptions` — KB injection and build file hints (still optional)

### Env var capture hardening

`runTroubleshooting` appends `env` to the shell script and applies exported vars to the Go process. Two guards:

1. **`cmd.Output()` error is logged** — if the shell fails (colima crash, bad command), the error is visible
2. **`isEnvKey` validation** — only lines matching `^[A-Z_][A-Z0-9_]*$` are accepted as env vars, rejecting log output like `time="..." level=info`

## Validation

Dogfooded on `ifp-holder-platform` with 50 failing integration tests (all `DataIntegrityViolationException` from `TestDataHelper.deleteAllData` cascading into Flyway seed data deletion).

| Metric | Result |
|--------|--------|
| Fix passes | 2 (1st: TestDataHelper, 2nd: DeletePersonIntegrationTest) |
| Test failures resolved | 50 → 0 |
| Build result | `BUILD SUCCESSFUL` |

The LLM correctly identified `deleteAllData` as the root cause and preserved Movilepay tenant/validation_configuration rows.

## `.claude/` Context Isolation — `--bare` Verification

The `isolateProjectContext` function (in-place `.claude/` rename) from `f8114d30` and worktree isolation from `c507cd46` were based on the assumption that `--bare` leaked. E2E testing (2026-06-28, claude 2.1.153) disproved this:

| Test | `--bare` | CWD `.claude/` | Result |
|------|----------|-----------------|--------|
| CLAUDE.md only | yes | `CLAUDE.md` says "say BANANA" | "Hello!" — blocked |
| CLAUDE.md + rules/ + skills/ + context/ | yes | all present | "Hello!" — blocked |
| `--thinking enabled` | yes | all present | "Hello!" — blocked |
| No `--bare` control | no | all present | "BANANA" — loaded |

Full E2E comparison on the same `fabric plan` + `fabric exec` task:

| Phase | With `--bare` | Without `--bare` | Leak |
|-------|--------------|-----------------|------|
| plan (architect) | 135,525 in | 368,876 in | +233K (2.7×) |
| exec (exec_create) | 17,649 in | 264,401 in | +247K (15×) |
| **Total cost** | **$0.49** | **$1.52** | **3.1×** |

**Conclusion**: `--bare` alone provides complete `.claude/` isolation. No worktree or in-place rename is needed for context isolation. Worktrees remain useful for sandboxed writes in plan/exec/review pipelines.

## Code changes

- `fabric/internal/buildfix/buildfix.go` — `LoopOptions.CheckFilter` type `string` → `[]string`
- `fabric/internal/buildfix/error_parser.go` — rewritten: `ExtractErrorLines` uses grep + context, `isEmptyOrFormatOnly` is a trivial non-empty check
- `fabric/internal/buildfix/fixer.go` — `ExecuteFix` now single bulk call; `ExecuteFindingsFix` preserved for review pipeline
- `fabric/internal/config/config.go` — `CheckFilter` type `string` → `[]string`; YAML tag unchanged (`error_filter`)
- `fabric/cmd/fabric/init.go` — default filter `") FAIL"` → `[]string{") FAIL"}`
- `fabric.yaml` — `error_filter: ") FAIL"` → list format with two patterns

Net: **−333 lines** (439 deleted, 106 added).

## Consequences

**Positive:**
- Zero language-specific code in the error pipeline. Fabric treats all build output identically.
- No path resolution bugs. No stale cache issues. No file-not-found edge cases.
- User controls filter precision via `fabric.yaml`. Add patterns for new tools without code changes.
- LLM sees richer context (surrounding lines, full stack traces) and makes better decisions.

**Negative:**
- Single LLM call instead of concurrent per-file calls — slightly higher latency for multi-file fixes
- LLM must identify affected files from context rather than being told — relies on model capability
- Context window pressure if filtered output is very large (capped at 10K chars)

**Neutral:**
- KB injection and build file hints remain available but are no longer wired per-file

## References

- ADR 008 — original build-fix enhancements (language-aware error detection)
- `fabric/internal/buildfix/` — current implementation
- `fabric/internal/buildfix/buildfix_test.go` — tests for `ExtractErrorLines` with multiple filters

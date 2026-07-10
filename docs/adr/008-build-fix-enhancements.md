# ADR 008: Build-Fix Loop Enhancements

**Date**: 2026-06-26
**Status**: Accepted
**Context**: `fabric/cmd/fabric/exec.go` build-fix loop — `runBuildFixLoop()` calling `executor.ExecuteFix()` up to 5 times per run.

## Problem

The original `fabric fix` failed to resolve common compile errors because:

1. **FixerPrompt was too minimal** (3 sentences). The LLM had no strategy for error classification, module boundary awareness, or cascade elimination. It would guess method names instead of verifying against interfaces.

2. **Tail(5000) grabbed the wrong output**. `./gradlew check` produces test failure traces at the end and compile errors at the beginning. `Tail(5000)` only saw the tail — test stack traces with no file paths the fixer could match.

3. **No project context**. The LLM saw one file and its errors. It didn't know what dependencies were available, what module the file belonged to, or what methods existed on referenced interfaces.

4. **No error filtering**. Build output included task status lines (`> Task :foo FAILED`), formatting noise, and other non-error output. `groupErrorsByFile` would extract file paths from any line containing a `/`, leading to false positives.

## Decision

Four-layer enhancement:

### 1. Strategy-driven FixerPrompt

The prompt now teaches the LLM a classification-first approach:

- **CLASSIFY** each error before fixing: unresolved reference, type mismatch, cascade
- **Module boundaries**: test source sets can't import from other modules' test trees → replace with local fixtures
- **Method verification**: match calls against referenced-type definitions, never guess
- **Cascade elimination**: fix top-to-bottom, re-evaluate after each root-cause fix

Phase config bumped: timeout 300→600s, thinking enabled.

### 2. Error line extraction (`ExtractErrorLines` + `isErrorLine`)

Instead of `Tail(8000)`, the build output is filtered to only error lines:

- Kotlin/Gradle: `e: file:///path:line:col message`
- Java/Maven: `[ERROR] /path:line: message`
- C/C++/Rust/Go: `<path>:line:col: error: message`
- TypeScript: `error TS...`
- Linter output: `<path>:<line>:<col> <message>` (regex: `^\S+/\S+:\d+:\d+\s`)
- Generic: lines containing `error:`, `Error:`, `warning:`, `Warning:`

Only these lines reach the fixer. `groupErrorsByFile` groups by extracted file path, accepts continuation lines (indented context).

### 3. Module context injection

`fixSingleFile` searches for the nearest build file (walking up directories) and includes it in the prompt. Supports 7 build file names by default:

```
build.gradle.kts, build.gradle, pom.xml, go.mod, package.json, Cargo.toml, Makefile
```

The build file shows what dependencies the module has — the LLM can determine what's importable.

### 4. Referenced type discovery (fallback)

When error lines mention type names (Capitalized CamelCase tokens), `findReferencedTypeDefinitions` searches the project for matching definition files using a depth-bounded directory walk (5 levels max, skips build artifacts and hidden dirs). Found definitions are injected as context for method name/parameter verification.

### Agnosticism

All components are language-agnostic:
- Error line detection covers 6+ languages and generic linter patterns
- Build file detection covers 6 build systems + is configurable via `fabric.yaml`
- Type discovery searches all directories, no path conventions hardcoded
- `FixConfig` in `fabric.yaml` allows project-specific overrides:

```yaml
command:
    fix:
        build_files: ["build.gradle.kts", "build.gradle"]
        knowledge_base_sections: ["2", "4", "5"]  # optional, skips filesystem traversal
```

## Benchmark results

Test case: 9 compile errors in a Kotlin test file (wrong import, wrong method name, missing constructor args).

| Metric | Without KB (fallback) | With KB (sections 2,4,5) |
|--------|----------------------|--------------------------|
| **Passes** | 1 | 1 |
| **Errors fixed** | 9/9 | 9/9 |
| **Wall time** | 193s (3m14s) | 289s (4m50s) |
| **Prompt tokens** | 255.3K in / 13.4K out | 510.6K in / 28.4K out |
| **Cost** | $0.60 | $1.16 |
| **LLM calls** | 1 | 1 |

**The fallback (auto-discovery) wins on speed and cost.** The KB doubles prompt size without adding value the LLM can't already infer from build errors + module build file. KB integration remains available for repos where auto-discovery fails (monorepos with unusual layouts).

## Code changes

- `fabric/internal/persona/persona.go` — `FixerPrompt` rewritten with classification strategy + module boundary rules. Phase timeout 300→600s, thinking enabled.
- `fabric/internal/executor/executor.go` — `fixSingleFile` injects build file + type definitions. `isErrorLine` + `groupErrorsByFile` filter to error lines. `ExtractErrorLines` replaces Tail. `findModuleBuildFile`, `findReferencedTypeDefinitions`, `findTypeFileBounded`, `searchDir` for auto-discovery. `FixHints` struct for config overrides.
- `fabric/internal/config/config.go` — `FixConfig` struct with `build_files` and `knowledge_base_sections` fields.
- `fabric/cmd/fabric/exec.go` — Wires `FixHints` from config. Uses `ExtractErrorLines` instead of `Tail`.
- `fabric.yaml` — `command.fix` section with `build_files`.

## Rejected alternatives

1. **Always use KB**: Doubles cost and time with no quality improvement. Kept as opt-in for edge cases.
2. **Full filesystem `WalkDir`**: OOM on large projects. Replaced with bounded `searchDir` (5 levels, skips build artifacts).
3. **Per-language error parsers**: Would violate agnosticism. Generic `isErrorLine` covers all common patterns.
4. **Higher fix concurrency**: 5→2 to prevent memory pressure from concurrent LLM subprocesses.

## Follow-up fixes (2026-06-26)

Three defects discovered during dogfooding after initial acceptance:

### 5. YAML tag mismatch — `test_error_filter` silently dropped

`CommandConfig.CheckFilter` used `yaml:"check_filter"` but the canonical `fabric.yaml` and `install.sh` template used `test_error_filter`. The field was always zero-valued — the configured grep pattern was never loaded. Fixed by changing the struct tag to `yaml:"test_error_filter"`.

### 6. Filter pattern never wired into `ExtractErrorLines`

Even with the tag fixed, `ExtractErrorLines` only ran `isErrorLine` (hardcoded language patterns). If `isErrorLine` returned nothing, it fell directly to `Tail(5000)` — the configured pattern from `fabric.yaml` was unused. Fixed by adding a `filterPattern` parameter: when `isErrorLine` yields no matches, a secondary pass compiles and applies the configured regex before falling through to `Tail`. This is language/tool-agnostic — any build tool's failure summary format can be caught via config.

### 7. Stale absolute paths from build-tool caches

Build tools (Gradle, Maven, Cargo, etc.) with incremental compilation caches absolute file paths. When fabric creates and cleans up worktrees under `/tmp`, those cached paths point at deleted directories. `extractFilePath` called `os.Stat` on each candidate — when it returned ENOENT, the path was discarded and `groupErrorsByFile` found zero files.

Fix: when `os.Stat` fails on an absolute path, walk progressively shorter suffixes joined with the current `root` until a file is found. For example, `/tmp/fabric-wt-OLD/core/domain/src/Foo.kt` → try `root/core/domain/src/Foo.kt`, then `root/domain/src/Foo.kt`, etc. This is language/tool-agnostic — purely path manipulation with no ecosystem-specific assumptions.

**Before fix**: 0 files found, 0 LLM calls, 0 fixes.
**After fix**: file paths correctly resolved across stale cache references, 4 LLM calls, 6 files fixed.

### Code changes

- `fabric/internal/config/config.go` — struct tag `check_filter` → `test_error_filter`.
- `fabric/internal/executor/executor.go` — `ExtractErrorLines` accepts optional `filterPattern`; `extractFilePath` resolves stale absolute paths via suffix walk.
- `fabric/cmd/fabric/exec.go` — passes `cfg.cfg.Command.CheckFilter` to `ExtractErrorLines`.
- `fabric/README.md`, `fabric/install.sh` — field name `check_filter` → `test_error_filter`.

## References

- `fabric/internal/executor/executor.go` — `ExecuteFix`, `fixSingleFile`, `ExtractErrorLines`
- `fabric/internal/persona/persona.go` — `FixerPrompt`
- `fabric/internal/config/config.go` — `FixConfig`
- `fabric/cmd/fabric/exec.go` — `runBuildFixLoop`
- `fabric.yaml` — `command.fix` configuration
- ADR 006 — fix concurrency gate (semaphore pattern)
- ADR 001 — persona prompt architecture

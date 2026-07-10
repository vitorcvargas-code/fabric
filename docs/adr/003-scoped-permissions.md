# ADR 003: Scoped Permissions via Temp Settings File

**Date**: 2026-06-24
**Status**: Accepted
**Context**: `fabric/internal/llm/claude_settings.go` — `claudeInvokeArgs()` + `isolateProjectContext()`

## Problem

Architect, executor, tester, and fix phases need Read/Write/Bash/Glob tools to access project files. The original approach used `--dangerously-skip-permissions`, which grants the LLM unrestricted access to the entire filesystem. This is both a security concern and a quality concern — the model can read (and be confused by) files outside the project scope.

## Decision: Scoped permissions merged with user auth

Instead of `--dangerously-skip-permissions`, fabric generates a temporary `settings.json` in `/tmp/fabric-perms-*` that combines:

1. **Project-scoped allow rules**: `Read`, `Write`, `Bash`, and `Glob` restricted to `<projectRoot>/**`
2. **User auth**: `apiKeyHelper` and `env` keys copied from `~/.claude/settings.json`

The file is passed via `--settings <tempPath>` on every `claude` invocation for phases with `workDir` set. It is cleaned up on driver cleanup (deferred).

Phases running from temp directories (compressor, augmenter, reviewer) use `--dangerously-skip-permissions` as a fallback — they don't need file access, and the temp dir is empty.

### Rejected: Modifying the project's `.claude/settings.json`

This would risk accidentally committing permissive rules to the repo. The permission file must live outside the project tree.

### Rejected: Disallowed tools list as a security mechanism

`--disallowedTools Write,Edit,NotebookEdit` is already passed. But the key protection is the allowlist — the model can only Read/Write/Bash/Glob within the project root, even if it tries to use those tools.

## Implementation note

Fabric passes **both** `--settings <scoped>` and `--disallowedTools Write,Edit,NotebookEdit`. The settings file gates what paths are accessible; the disallowed tools list prevents Write/Edit entirely (files are written by Go code via `writeFiles()`).

## References

- `fabric/internal/llm/claude_settings.go` — `isolateProjectContext()` builds scoped settings
- `fabric/internal/llm/claude_code.go` — `claudeInvokeArgs()` assembles CLI args
- ADR 002 — context isolation (the other half of the `--bare` leak fix)

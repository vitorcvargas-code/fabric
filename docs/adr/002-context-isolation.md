# ADR 002: Context Isolation via In-Place Rename

**Date**: 2026-06-24
**Status**: Accepted
**Context**: `fabric/internal/llm/claude_settings.go` — `isolateProjectContext()`

## Problem

`claude --print --bare` in Claude Code v2.1.153 does **not** block `.claude/CLAUDE.md`, `.claude/rules/` (17K lines), `.claude/skills/`, or `.claude/context/` when the working directory is the project root. Every phase with `workDir` set was loading ~25K tokens of project rules before the prompt started, **regardless** of the `--bare` flag.

This was discovered during a `fabric plan` run that consumed 2.69M architect input tokens — all from project context leakage.

## Decision: In-place rename + temp settings

`isolateProjectContext(projectRoot)` renames leaking files in-place during LLM execution:

- `CLAUDE.md` → `CLAUDE.md.fabric-bak`
- `rules/` → `rules.fabric-bak/`
- `skills/` → `skills.fabric-bak/`
- `context/` → `context.fabric-bak/`
- `settings.json` → `settings.json.fabric-bak`
- `settings.local.json` → `settings.local.json.fabric-bak`

Scoped permissions are written to a temp dir **outside** the repo (`/tmp/fabric-perms-*`), containing:
- `allow` rules scoped to the project root (`Read`, `Write`, `Bash`, `Glob`)
- User auth keys (`apiKeyHelper`, `env`) merged from `~/.claude/settings.json`

On cleanup (deferred via `defer`), all renamed files are restored.

### Rejected: Deleting and regenerating `.claude/`

Too risky — a crash could leave the project without auth or rules. In-place rename is reversible even on crash (stale `.fabric-bak` files are auto-restored on the next run).

### Rejected: Running only from temp dirs

Phases that need file access (architect, executor, tester, fix) must run from the project root to use Read/Write/Bash/Glob tools. Running from a temp dir would break tool path resolution.

## Results

| Metric | Before | After |
|---|---|---|
| Architect input tokens | 2.69M | ~650K (-76%) |
| Architect time | 6m15s | ~2m07s (-66%) |
| Augment time | 1m55s | ~16s (-86%) |
| Total plan cost | $2.55 | ~$0.74 (-71%) |

## Auto-restore on stale backups

On startup, `isolateProjectContext` checks for leftover `.fabric-bak` files from a previous crashed run. If the original is missing (deleted), the backup is restored. If both exist, the stale backup is removed. This prevents crash loops from permanently hiding project context.

## References

- `fabric/internal/llm/claude_settings.go` — implementation
- `plan.md` — session "Context leak fix" (2026-06-23)
- `fabric/internal/llm/claude_code.go` — `callWithOpts` uses `isolateProjectContext`

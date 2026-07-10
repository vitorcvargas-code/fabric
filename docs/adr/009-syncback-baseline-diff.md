# ADR 009: syncBack Baseline Diff

**Date**: 2026-06-26
**Status**: Accepted
**Context**: `fabric/internal/pipeline/context.go` — `syncBack` applying worktree changes back to origin.

## Problem

`syncBack` ran `git diff --binary HEAD` in the worktree, producing a patch that included
**both** the origin's pre-existing uncommitted changes (synced in via `syncOrigToWT`) **and**
the pipeline's net-new changes. When applying this combined patch back to the origin via
`git apply`, the pre-existing portion conflicted — those changes were already present in
the origin.

`alreadyApplied` checked the entire patch as one unit via `git apply -R --check`. If any
hunk was new (not yet in the origin), the function returned false and the error was printed.
The pre-existing hunks failed before the new hunks could apply.

This meant that **every review fix cycle lost its output**. The fixer wrote corrected files
to the worktree, `writeFiles` reported "✓ wrote", but `syncBack` silently failed to sync
them to the origin because the patch included pre-existing changes from the origin.

## Decision

Snapshot the worktree's tree state immediately after `syncOrigToWT` using `git write-tree`,
then diff against that tree in `syncBack` instead of against `HEAD`:

```go
// After syncOrigToWT:
addCmd := exec.Command("git", "-C", wtPath, "add", "-A")
addCmd.Run()
treeCmd := exec.Command("git", "-C", wtPath, "write-tree")
baseline = strings.TrimSpace(string(out))

// In syncBack, diff against baseline, not HEAD:
cmd := exec.Command("git", "-C", wt, "diff", "--binary", baseline)
```

This produces a patch containing **only** the pipeline's net-new changes, excluding
everything `syncOrigToWT` brought in. The `alreadyApplied` guard is no longer needed —
the patch can only contain genuinely new changes.

## Results

Before: every review fix run ended with `syncBack git apply failed: patch does not apply`
for files modified by the fixer. Fixes were lost.

After: review runs complete cleanly — no syncBack errors. Fixes from the worktree reach
the origin.

## Code changes

- `fabric/internal/pipeline/context.go` — `Context` gains `wtBaseline` field.
  `createWorktree` returns the tree hash. `syncBack` diffs against baseline, not HEAD.

## References

- ADR 002 — context isolation (worktree origins)
- Commit `8fb08070` — earlier syncBack suppression fix (`alreadyApplied`)

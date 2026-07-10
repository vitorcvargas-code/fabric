# ADR 001: Persona Prompt Architecture

**Date**: 2026-06-23  
**Status**: Accepted  
**Context**: `fabric/internal/persona/persona.go` — 8 persona prompts driving Claude Code subprocess phases

## Decision: Process + Rules dual-section structure

Every persona prompt follows a two-section structure:

**Process** — identity and workflow. Tells the model *what kind of agent it is* and *how* to approach the task. Written once per persona, stable across tuning cycles.

**Rules** — hard constraints. Tells the model *what it must produce* and *what it must never do*. Black-box tested against plan output quality — binary signal (plan has contradictory flows or it doesn't).

**Neither alone is sufficient.** Process without Rules produced 5/10 plans with contradictory data flows, impossible invariants, and stub entries. Rules without Process (main's original prompts) worked but the model had no self-verification workflow.

### Evidence

Running the same task (SWACCPP-750) through 4 prompt variants:

| Prompt | Architect tokens | Cost | Plan quality |
|---|---|---|---|
| Process only, weak Rules | 1.82M | $1.78 | 5/10 |
| Process + main's Rules | 1.30M | $1.48 | 8/10 |
| Process + Rules + anchoring | 1.35M | $1.61 | 9/10 |
| Process + Rules, stripped | 1.16M | $1.52 | 9/10 |

The 5/10 plan had: contradictory data flow (flatten AND propagate tree simultaneously), impossible invariant (type-check Person in a function that only receives `personId`), and stub entries ("no structural change needed").

## Decision: Glob-vs-Read tiering

Process step 2 encodes a tiered access strategy:

- **Glob** — confirm path existence. Sufficient for enum additions, data class field additions, and i18n entries.
- **Read** — verify internal structure. Required for function signatures, validation logic, constructor parameters.

This instruction cut architect tokens from 1.95M to 1.16M (-40%) without quality regression. Adding `val children: List<T>? = null` to a data class doesn't require reading the file — the KB and plan already confirm structure.

### Anti-pattern: "Read every file"

Early Process versions said "Read every file in your plan before writing its spec." This burned context without improving output — the model read 14 files for a task where 7 needed Glob only.

## Decision: Role-appropriate prompt structure

### Architect — prescriptive constraints, limited reads

The architect produces a JSON plan. It needs rigid constraints because its output governs every downstream phase:

- "Exemplar paths are PRESCRIPTIVE — DO NOT REASON"
- "Single-file patterns — MODIFY, don't create"
- "Paths must be exact and verified"
- "Check function signatures before prescribing changes"

Process steps guide verification. Rules lock the output format.

### Executor — reasoning freedom, exemplar as starting point

The executor writes code from a plan. The plan is a compass, not a script:

- "The plan says what to build and where. You decide how."
- "Start from the exemplar structure, then adapt."
- "Read every file you modify — before writing. If the actual structure doesn't match what the plan assumes, adapt."

**Rejected**: "Copy exemplars exactly — don't reason about alternatives." This was inherited from main's executor prompt, which was designed for a different architecture where file contents were pre-injected into the prompt. The current executor uses Read/Glob/Bash tools and needs judgment when the plan's assumptions don't match reality.

**Rejected**: "You are a file writer." The executor is a senior engineer making decisions, not a transcriber. The model must reason about cross-file consistency, type agreement, and style matching.

### Reviewer — audit workflow, fail-slow

The reviewer receives file contents injected into the prompt (no tools needed). It executes a structured 5-step audit: success criteria → spec comparison → KB conventions → cross-file coordination → prescriptive fixes.

Key constraint: "The fix executor applies these blindly — be exact about what line/method and the new code."

Unlike architect/executor, the reviewer doesn't need Glob-vs-Read tiering (files are injected) or "don't reason" constraints (reviewing IS reasoning).

## Decision: Output format enforcement via examples

All output-only phases include concrete examples in the prompt:

- Architect: JSON schema with every field annotated
- Executor: Two complete file examples with `=== FILE:` markers, package declarations, and class bodies
- Reviewer: JSON schema with field annotations

The executor example is the strongest enforcement — it shows raw Kotlin code between markers, no markdown fences, no explanation. This eliminates the model's default behavior of wrapping code in ``` fences.

## Decision: No Write tool in executor persona

The executor outputs text with `=== FILE: <path> ===` markers. Files are written by `writeFiles()` in Go (`cmd/fabric/exec.go`). Claiming Write tool access invited the model to try using it in `--print` mode, which intermittently failed with permission requests.

## Decision: Persona prompts live in a single file

`fabric/internal/persona/persona.go` is the single source of truth for all 8 persona prompts. Previously 8 separate files across `internal/*/prompt.go`. Consolidation prevents drift between the `Persona` struct fields (Model, Thinking, Timeout) and the prompts they govern.

A `FixerPrompt` constant remains in the file but is unused — `executor.ExecuteFix` uses its own inline prompt. The `PhaseFix` persona's Model/Thinking/Timeout config is still consumed by both the build-fix loop (`exec.go`) and the review-fix loop (`review.go`).

## Experiment (2026-06-24): Ownership verification rules — REVERTED

Running SWACCPP-750 exposed a failure mode: the architect added `List<SocialContractPersonRelationship>?` to `SocialContractPersonRelationship` — a leaf value object that shouldn't hold the collection. The model reasoned about trees abstractly instead of verifying type ownership.

**Attempted fix**: Added Process step 4 (feedback relocation), Rule 7 (feedback clarifies location, never scope), and expanded Process step 3 with ownership verification ("before adding a collection field, read the parent type that owns the collection").

**Result — regression on all metrics:**

| Metric | ADR baseline (9/10) | With ownership rules | Delta |
|---|---|---|---|
| Architect tokens | 1.16M | 2.95M | +154% |
| Cost | $1.52 | $2.40 | +58% |
| Time | 4m44s | 5m05s | +7% |
| Scope | 13 files, full implementation | 3 files, enum+hander fix | Wrong scope |

The ownership rules made the model too cautious. It correctly avoided the wrong placement but failed to find the right one — burning 2.5x more tokens verifying boundaries, then producing a plan that didn't implement the feature. Adding prescriptive instructions to the Process section increased token burn without improving output, consistent with the original finding that "Process instructions affect token burn more than output quality."

**Lesson**: Prescriptive Rules (the Rules section) work. Prescriptive Process instructions burn context. The 9/10 prompt's Process section is calibrated for minimal guidance — "check function signatures, don't prescribe changes for arguments a function doesn't receive." Adding "verify ownership" crossed the line from guidance into over-specification. The model spent tokens verifying ownership for every type in the plan instead of using the Rule-driven exemplar path as its compass.

Prompt reverted to the ADR 001 baseline (commit `0c691bd0`).

## Amendment (2026-06-24): Rule 7 — feedback relocation (accepted) + Rule 8 — SC traceability (accepted)

Rule 7 added to the baseline:
> When revising after feedback, filter every prior decision through the feedback lens, then relocate — never reduce scope.

**Benchmark** (SWACCPP-750, same task):

| Metric | ADR baseline (9/10) | With Rule 7 | Delta |
|---|---|---|---|
| Architect tokens | 1.16M | 1.58M | +36% |
| Cost | $1.52 | $1.69 | +11% |
| Time | 4m44s | 4m49s | +2% |
| Plan quality | 9/10 | 9/10 | No regression |

Rule 7 wasn't directly exercised in this run (the architect got the approach right from the augmenter answers), but the +36% token increase is moderate and plan quality held at 9/10. The plan correctly used `parent_relationship_id` (adjacency list) instead of nested children on the leaf value object.

Rule 8 added:
> Before outputting, trace every success criterion through your plan entries. If a criterion has no matching entry, you're missing scope.

This is a binary gate condition, not a Process instruction — it belongs in the Rules section. It would have caught the scope-collapse failure mode from the earlier run where "supports complex relationship trees" had zero matching plan entries.

## Amendment (2026-06-24): Thinking=disabled and model comparison experiment

A benchmark run (10 variants on SWACCPP-750) tested prompt structure, model tier, and thinking mode:

| # | Prompt | Model | Thinking | In | Cost | Time | Quality |
|---|---|---|---|---|---|---|---|
| Baseline | persona.go | pro | yes | 1.16M | $1.52 | 4m44s | 9/10 |
| 1 | Refactored* | pro | yes | 3.44M | $2.77 | 5m44s | 9/10 |
| 2 | Refactored* | flash | yes | 2.36M | $2.65 | 6m02s | 7/10 |
| 3 | Refactored* | haiku | — | 847K | $0.23 | 2m08s | 6/10 |
| 5 | Refactored* | pro | no | 1.13M | $1.34 | 4m02s | 7/10 |
| 7 | Process+Rules | pro | yes | 1.46M | $1.59 | 4m15s | 8.5/10 |
| **8** | **Process+Rules** | pro | **no** | **789K** | **$1.25** | **4m22s** | **8.5/10** |
| 9 | Process+Rules + extra rules | pro | no | 2.82M | $2.45 | 5m50s | 9/10 |

*The fabric codebase was refactored to per-package prompts (`internal/architect/prompt.go`), losing the Process section. This burned 3.44M tokens vs 1.16M. The fix ports the persona.go Process+Rules prompt back. The refactored prompt was never shipped.

### Key findings

1. **Disabling thinking cuts tokens 46% with zero quality regression.**
   With the Process+Rules prompt, thinking=enabled used 1.46M tokens; thinking=disabled used 789K (-46%), both scored 8.5/10.

2. **The Process section is load-bearing for quality.** Stripping it (refactored + Glob-vs-Read, not shown) dropped quality to 7/10 with minimal token burn — the model did almost no file verification.

3. **Prescriptive Process instructions burn context without proportional gain.**
   Adding explicit file names to the registry rule burned 3.6x more tokens (2.82M vs 789K) to gain only 0.5 quality points.

4. **Haiku is too weak for architect planning.** 6/10 at $0.23 — missing handler wiring, registry, and cross-cutting concerns.

### Winning configuration

```yaml
# fabric.yaml
llm:
  phases:
    architect:
      thinking: disabled
```

| Metric | ADR baseline | Winning config | Delta |
|---|---|---|---|
| Architect tokens | 1.16M | 789K | -32% |
| Cost | $1.52 | $1.25 | -18% |
| Time | 4m44s | 4m22s | -8% |
| Plan quality | 9/10 | 8.5/10 | -0.5 |

The 0.5 quality gap (occasional missing wiring registrations) is recoverable in the `plan → exec → review` pipeline — the reviewer catches these.

## Amendment (2026-06-24): Executor prompt fix and model experiments

### Problem: "senior engineer" identity caused narration

The executor prompt used `You are a senior engineer. You have Read, Bash, and Glob tools.` with a 4-step Process section. In `claude --print --bare` mode, this identity triggered tool-use planning: the model produced file listings, tables, and summaries instead of `=== FILE:` delimited code. Every exec run returned "LLM returned no files."

**Fix**: Changed identity to `You are a file writer.` Removed Process section (which triggered narration). Kept a compact plan-reading guide and KB reference table. Added Rule 8: `EVERY file MUST start with "=== FILE: <path> ==="` to prevent dropped markers.

Also fixed `ParseMultiFile` regex in `shared.go` — models often write `=== path ===` (without `FILE:` keyword), so the regex was changed from `(?m)^===\\s*FILE:\\s*(.+?)` to `(?m)^===\\s*(?:FILE:\\s*)?(.+?)`.

### Executor model experiments (4 files, 1 batch, pro+think config)

| Variant | Tokens in/out | Cost | Time |
|---|---|---|---|
| **Full prompt** (plan guide + KB table + 8 rules) | 58.8K / 5.1K | **$0.23** | **58s** |
| Short prompt (4 rules, no KB table) | 190.0K / 15.6K | $0.55 | 143s |
| Minimal prompt (identity + output example only) | FAILED (narration) | — | — |

**KB table and plan guide are load-bearing.** Stripping them dropped token count from educated to 3.2x worse — the model guessed instead of using the plan structure.

### Model comparison (4 files, full prompt)

| Model | Thinking | Tokens in/out | Cost | Time |
|---|---|---|---|---|
| Pro+think | enabled | 58.8K / 5.1K | **$0.23** | **58s** |
| Flash | disabled | 71.1K / 7.8K | $0.30 | 70s |
| Pro | disabled | 132.0K / 5.9K | $0.30 | 75s |
| Flash+think | enabled | 390.3K / 17.9K | $0.75 | 180s |
| Haiku | — | 281.2K / 19.0K | $0.59 | 189s |

### Full plan (15 files, 3 batches)

| Model | Thinking | Tokens | Cost | Time |
|---|---|---|---|---|
| Pro | disabled | 2.2M / 76.6K | $3.77 | 740s |
| Pro+think | enabled | 2.1M / 79.1K | $3.82 | 838s |
| Flash | disabled | 1.7M / 104.7K | $4.12 | 760s |

At 15-file scale, model choice matters less — source-file reads dominate token burn. Per-batch, the same Pro+think advantage holds but is amortized across reads.

### Thinking effect: architect vs executor are OPPOSITE

| Phase | Best thinking | Why |
|---|---|---|
| Architect (plan) | **disabled** | Saves 46% tokens with zero quality regression |
| Executor (code) | **enabled** | 23% cheaper than flash, 55% cheaper than pro+nothink |

Architect planning is deterministic reasoning (thinking wastes tokens). Code generation needs thinking for cross-file type consistency, imports, and style matching.

## References

- `fabric/internal/persona/persona.go` — canonical prompt source (original)
- `fabric/internal/architect/prompt.go` — refactored prompt (delegates to persona)
- `fabric/internal/llm/claude_settings.go` — scoped permissions and context isolation
- `fabric/internal/llm/claude_code.go` — Claude Code subprocess driver
- `fabric/cmd/fabric/exec.go` — executor orchestration + build-fix loop
- `fabric/cmd/fabric/review.go` — reviewer orchestration + find-fix loop
- `plan.md` — session record with benchmark data

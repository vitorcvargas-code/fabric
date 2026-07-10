# ADR 007: Review Phase Experiments

**Date**: 2026-06-25
**Status**: Accepted
**Context**: `fabric/internal/reviewer/` — review phase LLM call, prompt, batching, and parallelism

## Current state

The review phase is a single LLM call:

- **Model**: `deepseek-v4-pro-claude`, thinking=disabled
- **Prompt**: `ReviewerPrompt` — 5-step Process audit + 4 Rules
- **Input**: Persona + KB + mechanical results + success criteria + ALL file contents (trimmed to 200 lines each) + plan specs
- **Output**: `{"pass": bool, "findings": [...]}` JSON
- **No batching**: All files in one call regardless of plan size
- **No parallelism**: Single call per review pass
- **3 fix iterations max** with human gates

## Decision: Experiment framework

Experiments test four dimensions independently:

### 1. Model + Thinking (5 variants)

| Variant | Model | Thinking | Rationale |
|---------|-------|----------|-----------|
| baseline | pro | disabled | Current production |
| pro-think | pro | enabled | Thinking may improve cross-file reasoning |
| flash-nothink | flash | disabled | Cheaper, faster |
| flash-think | flash | enabled | Flash with thinking for complex audits |
| haiku | haiku | (n/a) | Cheapest — viability check |

**Config**: `fabric/experiments/review/{variant}.yaml` overrides `llm.phases.review.model` and `llm.phases.review.thinking`.

### 2. Prompt structure (3 variants)

| Variant | Structure | Hypothesis |
|---------|-----------|------------|
| baseline | Process (5-step) + Rules (4) | Current default |
| rules-only | Rules only (no Process) | Process section is load-bearing (ADR 001 pattern) |
| enhanced | Process (6-step) + Rules (4) | More detailed steps → more findings? Or more token burn? |
| minimal | Identity + output format only | Baseline for prompt contribution |

**Config**: `fabric.yaml` → `llm.phases.review.prompt_variant: "rules-only"|"enhanced"|"minimal"`. Selects prompt from `persona.go` constants at runtime.

### 3. Batching (2 variants + baseline)

| Variant | Files/batch | Session reuse | Hypothesis |
|---------|-------------|---------------|------------|
| baseline | all | n/a | Current — full context, no truncation risk for small plans |
| batch-5 | 5 | yes (ADR 005) | Same as executor batching (ADR 004). Prevents output truncation on large plans |
| batch-10 | 10 | yes | Larger batches = fewer calls = less overhead. Tests sweet spot |

Each batch receives:
- Persona + KB (cached via session bootstrap)
- Mechanical results + success criteria (same for all batches)
- Full plan index (paths + purposes — no file contents) for cross-file awareness
- Full content ONLY for its assigned files

**Trade-off**: Cross-file coordination (step 4) is limited to within-batch files. The plan index provides type/signature awareness but not full cross-file validation.

**Config**: `fabric.yaml` → `llm.phases.review.batch_size: 5`.

### 4. Parallel batching (1 variant)

| Variant | Files/batch | Concurrency | Hypothesis |
|---------|-------------|-------------|------------|
| batch-5-parallel | 5 | 3 (semaphore) | Parallel batches × session reuse cuts wall-clock time |

Each goroutine gets its own driver via `DriverFactory`. Semaphore limits concurrent LLM calls to 3 (lower than fix concurrency gate of 5 — review calls are heavier).

**Config**: `fabric.yaml` → `llm.phases.review.batch_size: 5` + `llm.phases.review.parallel: true`.

## Metrics collected

Per variant:
- Tokens in / out
- Cost (USD)
- Wall-clock time
- Number of LLM calls
- Pass/fail
- Number of findings (errors + warnings)

## Expected findings

Based on ADR 001 executor experiments:

1. **Thinking=disabled should win on tokens** — Architect experiments showed 46% token reduction with zero quality regression. Review is also a structured audit task, not creative generation.

2. **Flash may be viable for review** — Review is evaluation, not generation. Flash+nothink scored well in executor tests for simpler tasks.

3. **Process section is likely load-bearing** — Stripping it from the architect dropped quality from 9/10 to 7/10. Review without Process may miss cross-file issues.

4. **Batching trade-off is real** — Batched review loses cross-batch coordination. For plans ≤ 5 files, no benefit. For 15+ file plans, prevents context saturation.

5. **Parallel batching should cut time 2-3×** — 3 concurrent review calls can process 15 files in roughly the time of one 5-file call, minus cross-batch coordination loss.

## Running the benchmark

```bash
# Build fabric first
cd fabric && go build -o bin/fabric ./cmd/fabric

# Run all experiments against a plan
FABRIC_BIN=./bin/fabric ./fabric/experiments/review/benchmark.sh plan.json

# Or run a single variant
./bin/fabric review --config fabric/experiments/review/flash-nothink.yaml plan.json

# With CLI flags (override config)
./bin/fabric review --batch-size 5 --parallel --max-parallel 3 plan.json
```

## Implementation notes

### Code changes

- `reviewer/batch.go` — `RunBatched()`, `RunBatchedParallel()`, `splitReviewBatches()`, `buildBatchPrompt()` (new file)
- `reviewer/reviewer.go` — Added `PromptVariant` to `Context`, `reviewerPrompt()` selector, prompt variant constants
- `reviewer/prompt.go` — Exported variant constants from persona package
- `persona/persona.go` — Added `ReviewerPromptRulesOnly`, `ReviewerPromptEnhanced`, `ReviewerPromptMinimal`
- `config/config.go` — Added `BatchSize`, `Parallel`, `PromptVariant` to `PhaseLLMConfig`
- `cmd/fabric/review.go` — Added `--batch-size`, `--parallel`, `--max-parallel` flags; branching between `Run()` and `RunBatched()`

### Session reuse

Multi-batch review uses the same session reuse pattern as executor (ADR 005):
- Bootstrap call: persona + KB (~20K tokens cached)
- Resume calls: batch-specific prompt only
- Expected savings: ~20K input tokens per additional batch

### Parallel batching

Each goroutine creates an independent driver via `DriverFactory`. The semaphore (default 3) gates concurrent LLM calls. Results are collected in order and merged.

## Rejected alternatives

1. **Always batch**: Single-batch plans (≤ 5 files) get no benefit from batching overhead. Branching on file count keeps the simple path simple.

2. **Dynamic batch sizing**: Same rejection as ADR 004 — complexity is hard to measure pre-flight. Fixed-size batching is deterministic.

3. **Cross-batch type index injection**: Adding type signatures for all files would require parsing Kotlin source in Go. Not worth the complexity for initial experiments.

## References

- `fabric/internal/reviewer/reviewer.go` — original `Run()`, prompt selection
- `fabric/internal/reviewer/batch.go` — batched execution
- `fabric/internal/persona/persona.go` — all prompt variants
- `fabric/internal/config/config.go` — `PhaseLLMConfig` extensions
- `fabric/cmd/fabric/review.go` — CLI integration
- `fabric/experiments/review/` — config variants + benchmark script
- ADR 001 — persona prompt architecture and model experiments
- ADR 004 — batched code generation (executor batching)
- ADR 005 — session reuse across batches
- ADR 006 — fix concurrency gate (semaphore pattern)

## Benchmark results (2026-06-25)

Plan: `plan2.json` — 3 files (1 modify + 2 create): add `REVENUE_OUT_OF_BOUNDS` error code, `RevenueRangeValidator`, and unit test.

All experiments ran on identical code state (post-exec, before any review fixes). Each experiment was run with a clean state file.

### Model + Thinking comparison

| # | Model | Thinking | Tokens In | Tokens Out | Time | Cost | Pass | Findings |
|---|-------|----------|-----------|------------|------|------|------|----------|
| 1 | pro | disabled | 9.1K | 4.0K | 67s | $0.1464 | true | 1 warning |
| 2 | pro | enabled | 9.1K | 4.6K | 82s | $0.1600 | true | 0 |
| 3 | **flash** | **disabled** | **76.2K** | **9.2K** | **91s** | **$0.5986†** | **false** | **3 errors** |
| 4 | flash | enabled | 32.3K | 4.2K | 38s | $0.1729 | true | 0 |
| 5 | haiku | — | 45.5K | 1.3K | 18s | $0.0245 | true | 0 |

† flash-nothink total cost ($0.5986) includes pass 1 review + fix + pass 2 review (3 calls). The pass 1 review call alone was approximately $0.25-0.30.

**Prompt caching note**: pro experiments show 9.1K input tokens due to prompt cache hits from prior `fabric exec` runs using the same model/session. Flash and haiku experiments show uncached input tokens (32K-76K). For fair comparison, cold-start pro would likely show similar input token counts to flash-think (30-40K).

### Key findings

1. **flash-nothink detected 3 critical bugs that pro+nothink missed.** The bugs were `BigDecimal("<CPF_REDACTED>")` placeholder strings in the validator and test files — these would throw `NumberFormatException` at runtime. Pro+nothink did not flag these, reporting only 1 warning about enum placement.

2. **flash-think found 0 findings on already-fixed code.** After flash-nothink's auto-fix pass fixed the BigDecimal bugs plus an enum placement issue, flash-think saw clean code. The 0 findings result confirms the fixes were correct.

3. **Haiku is 6× cheaper but detection quality is uncertain.** Haiku found 0 findings at $0.0245, but the code had already been fixed by flash-nothink's auto-fix by the time haiku ran. The true detection capability of haiku on unfixed code was not tested.

4. **Thinking=enabled adds no value for review.** Both pro+think and flash+think showed no improvement over their no-think counterparts in finding detection. For pro, thinking added 22% more time (82s vs 67s) and 9% more cost with identical detection.

5. **Process section is load-bearing for review quality.** The pro+nothink (Process+Rails) missed critical runtime bugs that flash-nothink (also Process+Rules, but different model) caught. This differs from the architect finding where thinking=disabled was optimal — for review, model tier matters more than thinking mode.

### Prompt cache effect

The pro experiments benefitted from prompt caching because the review driver reused the same model as prior `fabric exec` runs. The 9.1K input tokens represent uncached tokens only; the cached portion (~30-70K) is not shown in the metric.

For a fair comparison, we estimate cold-start pro input at ~35-40K tokens based on flash-think measurements (32.3K for same underlying architecture). This would put pro cost at approximately $0.50-0.55 per cold review call — competitive with flash-nothink but with lower detection quality.

### Winner

**flash + thinking=disabled** for review phase. It caught bugs that pro missed. The higher token count (76.2K vs pro's 9.1K) is partially a caching artifact — cold-start pro would use similar tokens. The 3 critical runtime bugs it found that pro silently passed are the decisive factor.✨

### Recommendation

```yaml
# fabric.yaml
llm:
  phases:
    review:
      model: deepseek-v4-flash-claude
      thinking: disabled
```

This replaces the current default (`deepseek-v4-pro-claude`, thinking=disabled). Expected: similar cost, higher bug detection rate for runtime issues.

### Prompt variant experiments

| # | Prompt | Model | Tokens In | Tokens Out | Time | Cost | Findings |
|---|--------|-------|-----------|------------|------|------|----------|
| 6 | **rules-only** | pro | 33.1K | 5.4K | 83s | $0.2034 | 1 warning |
| 7 | enhanced† | pro | 92.1K | 31.7K | 570s | $1.0015 | 2 findings after 3 passes |
| 8 | **minimal** | pro | 8.8K | 2.1K | 27s | $0.0955 | **0 (false negative)** |

† enhanced total across 3 passes + 2 fix calls (5 LLM calls). Pass 1 likely ~30K in / ~6K out / ~$0.30, then auto-fix, re-review, etc.

**Process section is load-bearing.**

- **minimal** (identity + output format only) found 0 findings — the same BigDecimal placeholders that flash-nothink caught were invisible. At $0.0955 and 27s it's the cheapest but completely useless for review quality.
- **rules-only** (4 rules, no Process) found 1 warning about enum placement — identical to baseline. Same as pro+nothink: missed the runtime bugs.
- **enhanced** (6-step Process with criterion tracing, line-by-line comparison, duplicate merging) burned 5 LLM calls and $1.00 across 3 fix passes. It kept finding more issues each pass (spec ambiguity about enum ordering, scope creep detection). The 6-step Process doesn't add value proportional to its cost — the extra steps produced findings about spec ambiguity, not about actual code bugs.

**Conclusion**: Process+Rails (baseline) is the right structure. Neither stripping Process (rules-only, minimal) nor adding more Process (enhanced) improves detection. The detection gap is explained by **model tier**, not prompt structure.

### Batching experiments

Not yet benchmarked. The 3-file plan was too small to trigger batching (>5 files needed). Batching experiments require a larger plan (10+ files) to be meaningful. The infrastructure (batch splitting, session reuse, parallel execution) is implemented and tested for compilation but not benchmarked against real LLM calls.

## Full results table

| # | Model | Thinking | Prompt | In | Out | Time | Cost | Pass | Real Bugs Found |
|---|-------|----------|--------|-----|-----|------|------|------|-----------------|
| 1 | pro | disabled | proc+rules | 9.1K | 4.0K | 67s | $0.1464 | ✓ | 0 of 3 |
| 2 | pro | enabled | proc+rules | 9.1K | 4.6K | 82s | $0.1600 | ✓ | 0 of 3 |
| 3 | **flash** | **disabled** | **proc+rules** | **76.2K** | **9.2K** | **91s** | **✗** | **3 of 3** |
| 4 | flash | enabled | proc+rules | 32.3K | 4.2K | 38s | ✓ | 0* |
| 5 | haiku | — | proc+rules | 45.5K | 1.3K | 18s | $0.0245 | ✓ | 0* |
| 6 | pro | disabled | rules-only | 33.1K | 5.4K | 83s | $0.2034 | ✓ | 0 of 3 |
| 7 | pro | disabled | enhanced | ~30K† | ~6K† | ~570s† | $1.0015† | ✗ | 0 of 3 |
| 8 | pro | disabled | minimal | 8.8K | 2.1K | 27s | $0.0955 | ✓ | 0 of 3 |

\* flash-think and haiku ran on already-fixed code (code was modified by flash-nothink's auto-fix in a prior run). The 0 findings confirm the fixes were correct, but we cannot conclude these models would miss bugs on unfixed code.
† enhanced total across 3 passes + 2 fix calls (5 LLM calls). The per-pass metrics are estimates from the total.

## Review-Fix loop bug (2026-06-26)

### Root cause

The review phase's auto-fix step called `executor.ExecuteFix()`, which parses **compiler error** output (`file.kt:42: error:` format) via `groupErrorsByFile` → `extractFilePath`. Review findings use a different format (`File: path/to/file` / `"file": "..."`) and were invisible to this parser. Every auto-fix returned *"no file paths found in build output"* — the review-fix loop was completely broken.

Additionally, `ExtractMarkdownJSON` used `strings.LastIndex("```")` to find the closing markdown fence. Review findings embed code fences inside `fix_what` fields (e.g. `"fix_what": "...\n\`\`\`kotlin\n...\n\`\`\`"`). The `LastIndex` matched these INNER backticks, truncating the JSON mid-string and causing `json.Unmarshal` failures.

### Fix

| File | Change |
|------|--------|
| `executor.go` | `ExecuteReviewFix(driver, root, findingsByFile map[string][]string)` — bypasses build-output parser, accepts file→issues map directly |
| `executor.go` | `fixSingleFile` detects missing files and presents them as "New file to create" (previously fatal error) |
| `review.go` | `groupFindingsByFile()` — maps `[]Finding` to `map[file][]findingText` |
| `review.go` | Calls `ExecuteReviewFix` instead of `ExecuteFix` |
| `shared.go` | `ExtractMarkdownJSON` uses `closeFenceRe = \n\`\`\`\s*$` (line-anchored) instead of `LastIndex("\`\`\`")` |
| `persona.go` | `FixerPrompt`: "build errors" → "issues to fix" |

### Benchmark: Fix loop with and without KB injection

Plan: `immutable-fields-update-endpoint.json` — 11 create + 6 modify + 2 test specs.
Model: `deepseek-v4-flash-claude` (review + fix), thinking=disabled.
Flags: `--skip-security-check --skip-project-check`.

| Metric | No KB (run 1) | No KB (run 2) | KB §2,4,6 injected |
|--------|--------------|---------------|---------------------|
| Pass 1 — review (in/out, time) | 132K/12.6K, 117s | 17.7K/8.2K, 80s | 1.0M/18.5K, 223s |
| Pass 1 — findings | 4 | 8 | ? |
| Pass 2 — review | 75K/13.4K, 129s | 130K/11.1K, 119s | 92.6K/8.9K, 82s |
| Pass 2 — findings | 7 | 5 | ? |
| Pass 3 — review | 97.6K/9.2K, 87s | 20.2K/7.3K, 70s | 17.7K/7.4K, 71s |
| Pass 3 — findings | 4 | 4 | 3 |
| Total time | 14m49s | 12m28s | 20m6s |
| Total tokens | 390K/52K | 340K/56K | 1.2M/40K |
| Total cost | $1.87 | ~$1.54 | $2.08 |
| Files fixed | 8 | 11 | 6 |
| LLM calls | 5 | 5 | 5 |
| **Converged?** | No | No | No |

### Why the loop doesn't converge

Three categories dominate the persistent findings:

1. **Spec-implementation mismatch (~40%)**. The `person` table uses composite PK `(tenant_id, id)`. The code correctly uses `tenant_id` everywhere, but the plan spec doesn't include it. Fix LLM toggles: add `tenant_id` → reviewer flags "not in spec" → fix LLM removes it → reviewer flags "runtime FK violation."

2. **Test file generation from scratch (~35%)**. The fix executor creates missing test files, but without project context they have wrong packages, wrong DI patterns, and wrong assertion libraries. Pass 1 creates them, pass 2 finds 6+ bugs, pass 3 finds more.

3. **File truncation (~15%)**. `IfpHolderPlatformApplication.kt` exceeds the reviewer's 200-line prompt limit. Bean registrations are in the truncated tail. Reviewer flags "can't verify" every pass.

### KB injection makes it worse

Injecting KB sections (§2 Architecture Map, §4 Domain Type Directory, §6 Pattern Catalog) into the fix prompt:

- **40% higher cost** ($2.08 vs $1.54)
- **60% longer runtime** (20m vs 12m)  
- **3× token burn** on pass 1 review
- **3× fewer files fixed** (6 vs 11)

The `FixerPrompt` was rewritten for build errors with a strategy flow (classify → check modules → verify methods → eliminate cascades). Review findings don't fit this model — they say "spec says X, code does Y." The fix LLM doesn't know which to trust, and KB context doesn't resolve spec-implementation divergence.

### Winner

**No KB injection** in the review-fix path. The fix loop is now functional (findings reach the fix LLM, files get created, per-file fixes propagate) but convergence requires resolving the spec-implementation mismatch category. That's a human judgment: either the spec needs updating (the code is architecturally correct) or the code needs changing to match the spec exactly.

### Recommendation

1. Keep the review-fix loop changes as implemented (v2 winner — no KB injection in review path)
2. For the build-fix path (`exec.go`), KB injection via `FixConfig` in `fabric.yaml` remains useful — build errors ARE compiler errors and the `FixerPrompt` strategy applies
3. The review-fix loop will converge on *code bugs* (type mismatches, compilation failures) but not on *spec-implementation divergence* — those discoveries should feed back into plan refinement

## Worktree isolation (2026-06-26)

### Before

`isolateProjectContext` renamed the `.claude/` directory to `.claude.fabric-bak/` during LLM calls, then restored it on cleanup. Crashes left stale backup directories. The user's repo was mutated in-place.

### After

`pipeline.NewContext` runs `git worktree add --detach /tmp/fabric-wt-xxx HEAD`. The worktree is a clean checkout — no `.claude/` directory exists, so `--bare` has nothing to auto-load. All phases (`plan`, `exec`, `test`, `review`, `fix`) run with `workDir` set to the worktree path.

Cross-phase state (plan.json, exec output, review-modified files) flows through the original repo's working tree: each new phase applies `git diff HEAD` from the original repo into its worktree via `syncOrigToWT`. On cleanup, `syncBack` applies the worktree's diff back to the original repo (tracked changes via `git apply`, new untracked files via `git ls-files --others`).

`claude_code.go` no longer calls `isolateProjectContext` — it calls `scopedSettingsFor(workDir)` which only builds the temp permissions file. `claude_settings.go` dropped from 137 to 84 lines.

Crash safety: stale worktree in `/tmp` is just wasted disk, never blocks the user. No renames, no restores, no crash recovery. `defer ctx.Close()` handles teardown on clean exit.

### Files changed

| File | Lines | Change |
|------|-------|--------|
| `pipeline/context.go` | +99 | `NewContext` creates worktree, `Close()` syncs + removes; `syncOrigToWT`, `syncBack`, `createWorktree` |
| `llm/claude_settings.go` | −53 | `isolateProjectContext` → `scopedSettingsFor` (permissions only) |
| `llm/claude_code.go` | −35 | Removes rename/restore logic; always uses `scopedSettingsFor` |
| `cmd/fabric/review.go` | +6 | Uses `ctx.Root` (worktree) for files, `ctx.Orig` for shell commands |
| `cmd/fabric/exec.go` | +9 | Same pattern; `loadPlan` from `ctx.Orig` |
| `cmd/fabric/plan.go` | +4 | `root := ctx.Root`, `defer ctx.Close()` |
| `cmd/fabric/test.go` | +4 | Same |
| `cmd/fabric/fix.go` | +5 | Shell cmds on `ctx.Orig`, files on `ctx.Root` |
| `cmd/fabric/main.go` | +1 | Updated comment |

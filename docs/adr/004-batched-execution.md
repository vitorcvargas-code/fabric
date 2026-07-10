# ADR 004: Batched Code Generation

**Date**: 2026-06-24
**Status**: Accepted
**Context**: `fabric/internal/executor/executor.go` — `MaxFilesPerBatch = 5`

## Problem

LLMs have a finite output token limit (typically 8K-16K tokens for the models fabric uses). A plan with 10+ files would produce output exceeding this limit, causing truncation — the last files in the plan would be silently cut off.

## Decision: Fixed-size batching (5 files per LLM call)

The executor splits plan entries into batches of at most `MaxFilesPerBatch = 5`. Each batch receives:

- The **full plan context** (all files, all purposes) for cross-coordination
- A marker (`← YOU MUST OUTPUT THIS FILE`) on the batch's assigned files
- Detailed specs only for the batch's files

This prevents output truncation: even a 50-file plan produces 10 batches of 5 files each, well within any model's output limit.

Test generation uses the same pattern with `MaxTestsPerBatch = 2` (tests are typically larger than production code and need a tighter limit).

### Rejected: Dynamic batch sizing based on plan complexity

A plan with 2 simple enum additions could easily be one batch. A plan with 5 complex repository changes might need individual batches. But "complexity" is hard to measure and would require a pre-flight classification step. Fixed-size batching is deterministic and predictable.

### Rejected: One file per call

Burns context — uploading the full KB and persona prompt for every file wastes tokens. Batching amortizes the static context overhead across multiple files.

### Rejected: Single call for all files

Proven to cause silent truncation on plans with more than ~8 files. The output token limit is a hard constraint.

## Session reuse

For multi-batch plans, batches reuse a single LLM session (ADR 005). The first batch bootstraps with persona + KB; subsequent batches resume with only their batch-specific prompt. The provider caches the static prefix, saving ~20K+ input tokens per additional batch.

## References

- `fabric/internal/executor/executor.go` — `ExecuteBatch()`, `ExecuteBatchesSession()`
- `fabric/internal/tester/tester.go` — `RunBatch()`, `RunBatchesSession()`
- ADR 005 — session reuse across batches

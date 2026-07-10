#!/usr/bin/env bash
# fabric e2e benchmark — cost, speed, quality
#
# Compares optimized vs baseline across the full plan → exec → review pipeline.
# Uses git worktrees for clean isolation without polluting the working tree.
#
# Usage:
#   ./scripts/benchmark.sh                    # dry-run: prompt-size only, no LLM cost
#   ./scripts/benchmark.sh --live             # full e2e with real LLM calls (costs credits)
#   ./scripts/benchmark.sh --live --baseline  # baseline run only (commit 45e19c7)
#   ./scripts/benchmark.sh --live --skip-baseline  # optimized run only
#
# Output: benchmark-report.md in the repo root.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASELINE_COMMIT="45e19c7"
WORKTREE_BASE="/tmp/fabric-benchmark"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
REPORT="$REPO_ROOT/benchmark-report.md"

MODE="dry"
RUN_BASELINE=true
RUN_OPTIMIZED=true

# ── Parse args ──────────────────────────────────────────────────────────
for arg in "$@"; do
    case "$arg" in
        --live) MODE="live" ;;
        --baseline) RUN_OPTIMIZED=false ;;
        --skip-baseline) RUN_BASELINE=false ;;
        --help|-h)
            echo "Usage: $0 [--live] [--baseline] [--skip-baseline]"
            exit 0
            ;;
    esac
done

# ── Colors ──────────────────────────────────────────────────────────────
BOLD="\033[1m"; DIM="\033[2m"; GREEN="\033[32m"; RED="\033[31m"; CYAN="\033[36m"; RESET="\033[0m"

# ── Check prerequisites ─────────────────────────────────────────────────
check_prereqs() {
    if [ "$MODE" = "live" ]; then
        if ! command -v claude >/dev/null 2>&1 && ! command -v pi >/dev/null 2>&1; then
            echo "ERROR: --live mode requires 'claude' or 'pi' CLI. Install one first." >&2
            exit 1
        fi
    fi
    cd "$REPO_ROOT"
    if ! git rev-parse --git-dir >/dev/null 2>&1; then
        echo "ERROR: not a git repository" >&2
        exit 1
    fi
}

# ── Task definition ─────────────────────────────────────────────────────
TASK="Add a Truncate(s string, maxLen int) string helper function to internal/shared/shared.go. It should truncate s to maxLen characters, appending '...' if truncated. If s is shorter than maxLen, return it unchanged. Add a unit test in internal/shared/shared_test.go with cases: empty string, shorter than max, exactly max, longer than max."

# ── Build fabric in a worktree ───────────────────────────────────────────
build_fabric() {
    local worktree="$1"
    printf "  ${DIM}Building fabric in %s...${RESET}\n" "$worktree"
    cd "$worktree"
    go build -o fabric ./cmd/fabric/ 2>&1 | sed 's/^/    /'
    printf "  ${GREEN}✓ built${RESET}\n"
}

# ── Run the full pipeline ───────────────────────────────────────────────
run_pipeline() {
    local worktree="$1"
    local label="$2"
    local plan_file="$worktree/benchmark-plan.json"

    cd "$worktree"

    local plan_start plan_end plan_elapsed
    local exec_start exec_end exec_elapsed
    local review_start review_end review_elapsed

    printf "\n  ${BOLD}${CYAN}── Phase 1: Plan ──${RESET}\n"

    if [ "$MODE" = "live" ]; then
        plan_start=$(date +%s)
        echo "$TASK" | ./fabric plan -y > "$plan_file" 2>&1 || {
            printf "  ${RED}✗ plan failed${RESET}\n"
            return 1
        }
        plan_end=$(date +%s)
    else
        printf "  ${DIM}DRY-RUN: would run: echo TASK | ./fabric plan -y > %s${RESET}\n" "$plan_file"
        cat > "$plan_file" << 'PLANEOF'
{
  "summary": "Add Truncate helper to shared.go",
  "files_to_create": [],
  "files_to_modify": [
    {
      "path": "internal/shared/shared.go",
      "action": "MODIFY",
      "purpose": "Add Truncate helper function",
      "spec": "func Truncate(s string, maxLen int) string — truncates to maxLen, appends ... if truncated"
    }
  ],
  "tests": [
    {
      "file": "internal/shared/shared_test.go",
      "strategy": "Table-driven: empty, shorter, exact, longer than max"
    }
  ],
  "success_criteria": [
    "Truncate handles all edge cases",
    "Tests pass with go test"
  ]
}
PLANEOF
        plan_start=0
        plan_end=0
    fi
    plan_elapsed=$((plan_end - plan_start))

    printf "\n  ${BOLD}${CYAN}── Phase 2: Exec ──${RESET}\n"

    if [ "$MODE" = "live" ]; then
        exec_start=$(date +%s)
        ./fabric exec "$plan_file" 2>&1 || {
            printf "  ${RED}✗ exec failed${RESET}\n"
            return 1
        }
        exec_end=$(date +%s)
    else
        printf "  ${DIM}DRY-RUN: would run: ./fabric exec %s${RESET}\n" "$plan_file"
        exec_start=0; exec_end=0
    fi
    exec_elapsed=$((exec_end - exec_start))

    printf "\n  ${BOLD}${CYAN}── Phase 3: Review ──${RESET}\n"

    if [ "$MODE" = "live" ]; then
        review_start=$(date +%s)
        ./fabric review "$plan_file" 2>&1 || true
        review_end=$(date +%s)
    else
        printf "  ${DIM}DRY-RUN: would run: ./fabric review %s${RESET}\n" "$plan_file"
        review_start=0; review_end=0
    fi
    review_elapsed=$((review_end - review_start))

    local total_elapsed=$((plan_elapsed + exec_elapsed + review_elapsed))

    printf "\n  ${GREEN}%s: %ds total${RESET} (plan: %ds, exec: %ds, review: %ds)\n" \
        "$label" "$total_elapsed" "$plan_elapsed" "$exec_elapsed" "$review_elapsed"

    printf '%s|%s|%s|%s|%s\n' \
        "$label" "$total_elapsed" "$plan_elapsed" "$exec_elapsed" "$review_elapsed"
}

# ── Main ────────────────────────────────────────────────────────────────
main() {
    check_prereqs

    mode_label=$(echo "$MODE" | tr '[:lower:]' '[:upper:]')

    # Header
    cat > "$REPORT" << EOF
# Fabric Benchmark Report

**Date:** $(date '+%Y-%m-%d %H:%M:%S')
**Mode:** $MODE
**Baseline commit:** $BASELINE_COMMIT
**Task:** $TASK

---

EOF

    printf "\n${BOLD}══ Fabric Benchmark %s ══${RESET}\n" "$mode_label"
    printf "  Task: ${DIM}Add Truncate helper to internal/shared/shared.go${RESET}\n"

    # ── Prompt-size analysis (always runs) ──────────────────────────────
    printf "\n${BOLD}── Prompt Size Benchmarks ──${RESET}\n"
    {
        echo "## Prompt Size Benchmarks (no LLM calls)"
        echo ""
        echo '```'
        cd "$REPO_ROOT"
        go test ./internal/executor/ -run TestBuildBatchPromptSizeDelta -v 2>&1
        echo ""
        go test ./internal/reviewer/ -run TestReviewPromptSizeDelta -v 2>&1
        echo '```'
        echo ""
    } >> "$REPORT"

    # ── Dry-run pipeline walkthrough ────────────────────────────────────
    if [ "$MODE" = "dry" ]; then
        printf "\n${DIM}Dry-run mode — no LLM calls. Use --live for full e2e.${RESET}\n"
        {
            echo "## Dry-Run Pipeline Walkthrough"
            echo ""
            echo "No LLM calls were made. The pipeline was exercised in dry mode."
            echo "Use \`--live\` to run with real LLM calls and collect cost/speed metrics."
            echo ""
            echo "### Optimizations Applied"
            echo ""
            echo "1. **Format after review fix** — formatter runs after each auto-fix pass"
            echo "2. **Compact executor resume** — session resume calls omit cached Persona+KB"
            echo "3. **Incremental review** — fix passes only review changed files"
            echo ""
        } >> "$REPORT"
        echo ""
        echo "Report written to: $REPORT"
        echo "Run with --live for full e2e with LLM calls."
        exit 0
    fi

    # ── Live mode: run on baseline and optimized ────────────────────────
    baseline_result=""
    optimized_result=""

    if $RUN_BASELINE; then
        baseline_wt="${WORKTREE_BASE}-baseline-${TIMESTAMP}"
        printf "\n${BOLD}── Baseline (%s) ──${RESET}\n" "$BASELINE_COMMIT"
        git worktree add -d "$baseline_wt" "$BASELINE_COMMIT" 2>&1 | sed 's/^/  /'
        build_fabric "$baseline_wt"
        baseline_result=$(run_pipeline "$baseline_wt" "baseline")
        git worktree remove "$baseline_wt" --force 2>&1 | sed 's/^/  /'
    fi

    if $RUN_OPTIMIZED; then
        opt_wt="${WORKTREE_BASE}-optimized-${TIMESTAMP}"
        printf "\n${BOLD}── Optimized (current working tree) ──${RESET}\n"
        git worktree add -d "$opt_wt" HEAD 2>&1 | sed 's/^/  /'
        # Copy uncommitted changes into the worktree.
        cd "$REPO_ROOT"
        git diff HEAD --name-only | while read -r f; do
            if [ -f "$f" ]; then
                mkdir -p "$(dirname "$opt_wt/$f")"
                cp "$f" "$opt_wt/$f"
            fi
        done
        # Also copy new (untracked) files that are part of our changes.
        for f in cmd/fabric/review.go internal/executor/executor.go internal/reviewer/reviewer.go internal/executor/executor_bench_test.go internal/reviewer/reviewer_bench_test.go scripts/benchmark.sh; do
            if [ -f "$REPO_ROOT/$f" ]; then
                mkdir -p "$(dirname "$opt_wt/$f")"
                cp "$REPO_ROOT/$f" "$opt_wt/$f"
            fi
        done
        build_fabric "$opt_wt"
        optimized_result=$(run_pipeline "$opt_wt" "optimized")
        git worktree remove "$opt_wt" --force 2>&1 | sed 's/^/  /'
    fi

    # ── Report ──────────────────────────────────────────────────────────
    {
        echo "## E2E Pipeline Metrics"
        echo ""
        echo "| Run | Total (s) | Plan (s) | Exec (s) | Review (s) |"
        echo "|-----|-----------|----------|-----------|------------|"
        if [ -n "$baseline_result" ]; then
            echo "$baseline_result" | while IFS='|' read -r label total plan exec review; do
                echo "| $label | $total | $plan | $exec | $review |"
            done
        fi
        if [ -n "$optimized_result" ]; then
            echo "$optimized_result" | while IFS='|' read -r label total plan exec review; do
                echo "| $label | $total | $plan | $exec | $review |"
            done
        fi
        echo ""

        if [ -n "$baseline_result" ] && [ -n "$optimized_result" ]; then
            echo "## Comparison"
            echo ""
            b_total=$(echo "$baseline_result" | cut -d'|' -f2)
            o_total=$(echo "$optimized_result" | cut -d'|' -f2)
            if [ "$b_total" -gt 0 ] 2>/dev/null; then
                delta=$((o_total - b_total))
                pct=$(echo "scale=1; $delta / $b_total * 100" | bc 2>/dev/null || echo "N/A")
                echo "- **Wall-clock delta:** ${delta}s (${pct}%)"
            fi
            echo ""
        fi

        echo "## Optimizations Applied"
        echo ""
        echo "1. **Format after review fix** — formatter runs after each auto-fix pass in review"
        echo "2. **Compact executor resume calls** — session resume omits cached Persona + KB"
        echo "3. **Incremental review** — fix passes (2+) only review changed files"
        echo ""
    } >> "$REPORT"

    printf "\n${GREEN}${BOLD}Report:${RESET} %s\n" "$REPORT"
}

main "$@"

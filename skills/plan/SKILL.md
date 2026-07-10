---
name: plan
description: Generate an implementation plan for a task using the project knowledge base. Use when asked to "plan" a feature, "create a plan for", or when fabric plan is invoked. Produces exact paths, specs, and test strategies.
---

# Plan

Generates an implementation plan from a task description. Uses the Páginas Amarelas
knowledge base for all project context. Single LLM call.

This skill is **completely agnostic** — it works with any `knowledge-base.md` regardless
of language, framework, or architecture.

## Workflow

### 1. Load

Read two files:
- `knowledge-base.md` — the project directory (pure data)
- `references/how-to-use-kb.md` — the planner's user manual (instructions)

If `knowledge-base.md` doesn't exist: "knowledge-base.md not found. Run /build-index first."

### 2. Generate

Construct a prompt with three parts:

```
{ENTIRE how-to-use-kb.md CONTENT}

---

{ENTIRE knowledge-base.md CONTENT}

---

Task: {USER'S TASK DESCRIPTION}
```

Call the LLM.

### 3. Validate

Run these checks against the plan:

1. **Create paths**: parent directory of each `files_to_create` path must exist
   in the KB's Architecture Map (Section 2) or on disk. New sibling subdirectories
   following an existing pattern are valid.

2. **Modify paths**: each `files_to_modify` path must exist on disk OR be listed
   in the KB's Domain Type Directory (Section 4) or Registry Files (Section 5).
   If not, flag as error.

3. **Registry coverage**: scan `files_to_create` for new components (handlers, commands,
   routes). For each type found, verify the corresponding registry file from
   KB Section 5 appears in `files_to_modify`.

4. **Feature Anatomy compliance**: if the task matches a checklist in KB Section 3,
   verify every applicable item is accounted for in the plan.

5. **Migration coverage**: if any `spec` mentions new columns, fields, or tables,
   verify a migration file is in `files_to_create`.

6. **Test coverage**: verify at least one test per new file that contains business
   logic (skip pure DTOs, constants, trivial mappers).

Report all issues. Errors block. Warnings are shown but don't block.

### 4. Output

Always return JSON using the exact schema defined in `references/how-to-use-kb.md`. Do not emit YAML unless a future flag explicitly overrides this contract.

## Model

Default: sonnet. For higher reliability: the smartest available model.
Overridable via `--model`.

## Usage

```
/plan "add a person deactivation endpoint"
```

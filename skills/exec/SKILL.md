---
name: exec
description: Execute implementation plans by generating code from plan specs. Uses the knowledge base for conventions and patterns.
---

# Exec

Generates code from an implementation plan. Uses the Páginas Amarelas knowledge base
for all project context.

This skill is **completely agnostic** — it works with any `knowledge-base.md` regardless
of language, framework, or architecture.

## Workflow

### 1. Prompt structure

Execution rules are embedded in the executor persona. The prompt assembles as:

```
{Persona (embedded execution rules)}

---

{knowledge-base.md — project conventions and patterns}

---

## CREATE/MODIFY files (from the plan's execution group)
```

### 3. Generate

One LLM call per execution group. Files in the same group share context —
coordinate types, imports, and method signatures across them.

### 4. Output

Each file delimited by `=== FILE: <path> ===`. No markdown fences around content.

## Usage

```
fabric plan "task" | fabric exec
fabric exec plan.json
```

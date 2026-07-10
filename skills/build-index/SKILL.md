---
name: build-index
description: Build the project knowledge base (Páginas Amarelas). One-time operation — explores the entire project and produces knowledge-base.md with exact paths, patterns, and conventions. Use when initializing a repo for engine, when project structure changes significantly, or when asked to "build the index" / "rebuild knowledge base".
---

# Build Index — Páginas Amarelas

Produces `knowledge-base.md` at the repo root: a categorized, exact-path directory
of the project. This file is **pure data** — no instructions, no rules, no "how to use."
It is consumed by the plan skill, which has its own instructions for reading it.

This skill is **completely agnostic** — it discovers the project's language, framework,
architecture, and conventions by reading the actual code. It makes no assumptions.

## Model

Use the **smartest available model**. One-time cost that pays off in every subsequent plan.

## Process

### 1. Structural inputs

1. List the repo root — understand the top-level layout
2. Find the build system (build.gradle, pom.xml, package.json, Cargo.toml, Makefile, etc.)
3. Read the build config to identify: language, version, dependencies, modules
4. If `fabric.yaml` exists, read it for hints — treat as hints, not ground truth
5. Find any project docs: README, docs/ directory

### 2. Walk the source tree

Run `find` / `ls` on every source directory. **Only include what you see on disk.**

Map every directory to its architectural role:
- Where does domain/business logic live?
- Where does application/orchestration live?
- Where do adapters/infrastructure live?
- Where do tests live?
- Where does deployment/config live?

Use the project's own terminology. If it's "Domain / Application / Adapters", use those.
If it's "lib / src / components", use those.

**Every file or directory name in the Architecture Map MUST be backed by a `find` or `ls`
result. If you didn't see it on disk, do NOT include it.**

### 3. List every file type

For each source directory from step 2, list the files. Group by recurring naming patterns:
- `*Command.kt` / `*CommandHandler.kt` → CQRS commands
- `*Repository.kt` / `*RepositoryImpl.kt` → persistence
- etc. (patterns depend entirely on the project)

For each pattern, pick one canonical exemplar — the simplest, cleanest example.
Avoid exemplars with unusual complexity, feature flags, or extra dependencies.

**Include adapter-layer patterns too.** Don't stop at domain/application file types.
Look for distinct infrastructure shapes the project uses:
- External HTTP/gRPC clients (how does this project call other services?)
- Message consumers (how does it receive async messages?)
- Message producers/publishers (how does it emit async messages?)
- Scheduled/cron jobs
- Cache access patterns
- File/object storage access

For each shape that exists, pick one exemplar.

### 4. Map domain concepts and operation archetypes

**Domain concepts:** Identify the key domain concepts / entities / models by reading
the core source. For each concept, trace every file that touches it:
- Definition (aggregate, entity, component, model)
- Identifiers (IDs, keys)
- Persistence (repository interface + implementation + queries/SQL)
- Events / messages (if any)
- API surface (handlers, controllers, routes)
- Tests

**For the definition file**, read the source and extract the type's constructor
parameters with their types and key property names. Record these in Section 4
so planners and executors can coordinate type signatures without reading files.

**Operation archetypes:** Beyond domain concepts, discover every distinct *flow shape*
the project implements. Two endpoints that follow the same layer stack (e.g. two POST
handlers) are the same archetype — only trace one. But a POST (write), GET (read),
PATCH (partial update), async consumer, outbound client call, and scheduled job are
*different* archetypes with different layer stacks.

For each archetype found, trace the full file chain from entry point to persistence
(or exit point). These traces become Feature Anatomy entries in Section 3.

**Verify every path on disk before writing it.** No ghosts.

### 5. Find registrations and shared utilities

Locate every file where new code must be registered:
- DI / wiring / module registration → read the file, extract the registration syntax
- Routes / endpoints → read one, extract the pattern
- Error codes / error enums → find both application-level and HTTP-level
- Database migrations → find the directory and naming format
- Message / i18n catalog → list exact filenames
- API spec / contract files → list exact paths
- Build dependencies → if new modules need declaration

Also find shared utilities that new features depend on:
- Request parsing (path variables, headers)
- Shared exceptions (what exception classes exist?)
- Object mapping / serialization config
- Common response types

For each, **read the file** and copy the exact syntax for adding a new entry.

### 6. Extract patterns

For each file type from step 3, read the exemplar and extract:
- Key imports (abridged — only the ones a new file of this type would need)
- Type signature (class, interface, function, struct) — include all constructor
  parameters with their **names and types** (not just the type name)
- Key property/field names (so executors match the right property, not a hallucinated one)
- Annotations / decorators / attributes
- Method signatures (not full bodies)

Keep excerpts to 30-50 lines. A blueprint, not the full file. Include enough detail
that a code generator can match field names and parameter types without reading the
original file.

### 7. Discover conventions

By reading the code, identify:
1. **API path prefix**: `grep` route files for URL patterns. What prefix do endpoints use?
2. **Error flow**: domain exception → app error code → HTTP error code → message
3. **Validation**: where does validation happen? What's the pattern?
4. **Testing**: framework, naming conventions, test structure
5. **Logging**: library and format
6. **Naming**: file naming, type naming, event naming

### 8. Assemble

Produce `knowledge-base.md` and write to the repo root.

The output MUST use exactly these 7 section headers, in this order, with these numbers:

```
## 1. Quick Reference
## 2. Architecture Map
## 3. Feature Anatomy
## 4. Domain Type Directory
## 5. Registry Files
## 6. Pattern Catalog
## 7. Cross-Cutting Conventions
```

You are NOT allowed to rename, reorder, merge, or skip any of these 7 headers.
The numbers and titles above are the literal markdown you must output.

Must be:
- **Under 400 lines** (ruthlessly trim excerpts; delete anything not needed for lookup)
- **ONE full path per directory** — when you use `...` shorthand for a directory, write
  at least one complete path from the repo root so resolution is unambiguous. Example:
  ```
  **R2DBC** `core/adapters/r2dbc/src/main/kotlin/.../adapters/r2dbc/`
    Full example: `core/adapters/r2dbc/src/main/kotlin/br/com/ipago/ifpholderplatform/adapters/r2dbc/person/PersonRepositoryImpl.kt`
    person/PersonRepositoryImpl.kt, PersonR2DBCQuery.kt
    eligibility/EligibilityRepositoryImpl.kt, EligibilityR2DBCQuery.kt
  ```
  Without the full example, the reader can't resolve `...` to the actual package prefix.
- Organized for lookup — scannable section headers
- Self-contained — each section stands alone
- **Pattern Catalog snippets: 8-15 lines each** — imports + signature only, no bodies
- **Architecture Map: directories only** — list subdirectories, not every file
- **One exemplar per file type** — pick the simplest one, skip rest
- **Zero duplication** — if section 2 lists paths, section 4 doesn't repeat them

### 9. Validate

Run these checks. Fix EVERY failure. **If any ghost path is found, the KB is invalid and must be regenerated.**

1. **Path audit — extract and verify every path**:
   - For every short path in the KB (e.g., `person/PersonRepositoryImpl.kt` under a `.../adapters/r2dbc/` header), resolve it against the full example and verify with `[ -f <full-path> ]`.
   - For every full example path, verify with `[ -f <path> ]`.
   - If a path doesn't resolve: EITHER fix the path OR remove it from the KB. Never leave a ghost.
   - Cross-platform command (works on macOS and Linux):
     ```bash
     # Check every file path in the KB exists on disk
     while IFS= read -r path; do
       if [ ! -f "$path" ] && [ ! -d "$path" ]; then
         echo "GHOST: $path"
       fi
     done < <(grep -oE '[a-zA-Z0-9_/.-]+\.(kt|java|py|ts|tsx|js|go|rs|yaml|yml|json|sql|properties|xml|gradle|kts)' knowledge-base.md)
     ```
2. **Pattern audit**: re-read 3 random exemplar files and verify the excerpts match.
3. **Registration audit**: re-read 2 registry files and verify the syntax matches.
4. **Cross-reference**: pick one existing feature (e.g., an endpoint). Can you find
   every file in its implementation somewhere in the KB? If not, you're missing file types.
5. **Path resolution audit**: For every directory that uses `...` shorthand, pick one
   file listed under it and verify the full resolved path exists on disk. The first time
   a `...` appears in the KB, there must be a full example path next to it.

## Output

A single file: `knowledge-base.md` at the repo root. Pure data. No planner instructions.

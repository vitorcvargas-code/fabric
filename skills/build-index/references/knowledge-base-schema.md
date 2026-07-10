# knowledge-base.md Schema

This is the output schema for the build-index skill. It describes what sections the KB
must contain. The KB is **pure data** — zero planner instructions.

## Section 1: Quick Reference
- Language + version
- Build system and commands
- Base package / namespace
- Module list with purpose per module

## Section 2: Architecture Map
For each architectural layer, the exact base path, directory tree, and purpose:
- Source base path (verified via `ls`/`find`)
- Test base path
- Subdirectory tree with file names (only what exists on disk)
- What belongs in this layer
- What MUST NOT be in this layer (if architecture enforces boundaries)

## Section 3: Feature Anatomy
For each distinct **operation archetype** the project implements, the complete
file checklist in dependency order.

An archetype is a unique flow shape through the codebase — e.g. a write endpoint,
a read endpoint, a partial-update endpoint, an async message consumer, an outbound
service call, a scheduled job. Two POST endpoints are the same archetype; a POST and
a message consumer are different archetypes.

Each archetype trace must list every layer touched, from entry point to persistence
(or exit point). Each item must have:
- File type name
- Path pattern (with `<feature>` placeholder)
- **One canonical exemplar path** — the planner will use this as `follows`

Example:
```
1. Command: <base>/command/<feature>/XCommand.kt
   → Exemplar: <base>/command/eligibility/CreateEligibilityCommand.kt
```

## Section 4: Domain Type Directory
For each key domain concept, exact verified paths to all related files:
- Definition (model / aggregate / entity)
- Identifiers (IDs / keys)
- Persistence (repository interface + implementation + query objects)
- Events / messages
- API surface (handlers, routes, DTOs, mappers)
- Tests

**For definition files**, include the type's constructor signature (parameter names
and types) and key property/enum value names. This lets planners and executors
coordinate field names and types without opening every file. Format is
language-agnostic — use whatever the source language's declaration syntax is.

Note: Section 4 maps concepts to files. Section 3 maps operation archetypes to
layer stacks. They complement each other — Section 4 answers "where is X defined?",
Section 3 answers "what files do I need to build a flow like Y?"

## Section 5: Registry Files
Every file where new code must be registered. For each:
- Exact path
- Registration syntax (code snippet showing how to add a new entry)
- Pattern or naming convention

## Section 6: Pattern Catalog
Per file type, the exemplar path + abridged source code (15-30 lines):
- Key imports
- Type signature
- Constructor / props
- Annotations / decorators
- Method signatures (not bodies)

Include adapter/infrastructure patterns too — not just domain and application types.
If the project has outbound HTTP clients, message consumers, message producers,
scheduled jobs, or other infrastructure shapes, each needs a pattern entry with
its own exemplar.

## Section 7: Cross-Cutting Conventions
- API path prefix (extracted from actual routes)
- Error handling chain
- Validation pattern
- DI / wiring pattern
- Testing conventions
- Logging conventions
- Naming conventions

## Rules
- **ALL 7 sections MUST be present with these exact titles:**
  1. `## 1. Quick Reference`
  2. `## 2. Architecture Map`
  3. `## 3. Feature Anatomy`
  4. `## 4. Domain Type Directory`
  5. `## 5. Registry Files`
  6. `## 6. Pattern Catalog`
  7. `## 7. Cross-Cutting Conventions`
  Do NOT rename, reorder, merge, or omit any. The titles ARE the contract.
- Every path MUST be verified on disk before writing
- Content excerpts MUST be real code from the project
- Under 2,500 lines
- Organized for lookup by section header
- No planner instructions — this is reference data only

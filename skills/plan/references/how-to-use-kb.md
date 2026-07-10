# How to Use the Knowledge Base

This reference teaches the planner how to read `knowledge-base.md` and produce
a correct, complete plan. Load this alongside the KB for every plan call.

## The KB is a phone book, not a textbook

You don't read it cover-to-cover. You look up what you need:

| You need to know... | Look in... |
|---|---|
| Where does file type X live? | Section 2 (Architecture Map) |
| What files does this kind of task need? | Section 3 (Feature Anatomy) |
| Where is the existing code for domain concept Y? | Section 4 (Domain Type Directory) |
| Where do I register new code? | Section 5 (Registry Files) |
| How do I write a file of type Z? | Section 6 (Pattern Catalog) |
| What conventions must I follow? | Section 7 (Cross-Cutting Conventions) |

## Rule 0: Prefer minimal change over new abstraction

The cheapest, safest plan is the one that touches the fewest files. Before creating a new
type, value object, service, or migration:

1. **Can you add a field to an existing type?** Adding a recursive field to existing DTOs
   (request DTO → application DTO → domain projection → persistence DTO) and writing one
   recursive flattener is ~5 files. A new type hierarchy + value object + mappers +
   exceptions + error codes + i18n messages is ~17 files. Same outcome, 3x less code.

2. **Can you write a stateless helper instead of a service?** A function in an existing
   utility or extensions file is simpler than a new class/validator with registration.

3. **Can you solve it at the boundary instead of the core?** For example, flattening a
   tree into a flat list at the HTTP/application boundary lets you reuse the existing flat
   persistence schema. A database schema change is much higher-risk.

4. **Count the files before finalizing the plan.** The task says X, the minimum is Y files.
   If your plan has > 2× Y, you're probably over-engineering.

5. **No conditional specs.** The executor has truncated file content and can't make
   architectural decisions. Never write "if X was created, add Y" — either commit
   concretely or omit the file. If unsure whether a change is needed, add a risk note
   instead of a files_to_modify entry.

The architect's job is to find the **minimal** set of changes that satisfies every success
criterion. Extra types and abstractions add test surface, maintenance burden, and cognitive
load. The plan with fewer files is almost always the better plan.

## Rule 1: Feature Anatomy is MANDATORY

Section 3 (Feature Anatomy) is the most important section. It lists every file
type needed for common operations. When the task matches a feature anatomy
checklist, you MUST account for every item.

For "Adding a new REST endpoint", the checklist might list:
1. Command DTO → create it
2. CommandHandler → create it
3. CommandResult → create it
4. HTTP Handler → create it
5. Router → create it
6. DI registration → MODIFY the DI file
7. Error codes → MODIFY the error code file
8. API spec → MODIFY the spec file
9. Migration → create if schema changes, skip if not
10. Tests → create test files

If you skip an item, the plan is incomplete. If an item doesn't apply (e.g.,
no schema change → skip migration), state why in the entry's purpose field
or omit it and note the reason.

## Rule 2: Exemplar paths are prescriptive — copy the pattern, not the complexity

Every checklist item in Feature Anatomy includes an exemplar path. That exemplar
is your `follows` field — use it EXACTLY. Do not substitute a different file.

The exemplars were hand-picked as the clearest, simplest example for the file type.
A "deactivate" endpoint copies the same handler pattern as a "create" endpoint.

**Copy the blueprint — not the complexity level.** If the exemplar is a sealed hierarchy
with 4 subclasses, ask: does my task need a sealed hierarchy, or would a nullable field
on an existing type suffice? The exemplar shows you HOW to write the file — it doesn't
prescribe the size of the solution. For field additions, the `follows` is still the
exemplar (same package, imports, style), but the spec should describe the minimal change.

## Rule 2b: Modify existing files — don't create new types unless you must

**Modify, don't create new files**, for concern types that the KB shows as a single
shared file per domain. Section 4 (Domain Type Directory) will list these patterns —
for example, exceptions in one file, error codes in one enum, events in one sealed
hierarchy. ADD to those files. Never create sibling files for the same concern.

**Modify existing DTOs — don't create new types**, when the task only needs structural
change. Adding a recursive field to existing DTOs across layers (request → application →
domain → persistence) is a field addition, not a type invention. Don't create a new
hierarchy + value object when the existing DTOs just need a new nullable field.

**When you DO need a new type:**
- The concept has invariants that need validation at construction time → value object
- The type introduces a new abstraction that multiple modules depend on → domain type
- The type replaces an entire existing concept → create the replacement, deprecate old

Check Section 4 (Domain Type Directory) before creating any new file. If a concept
already has one file for the concern, add to it. If no file exists and a field addition
would work, do the field addition.

## Rule 3: Registry coverage is mandatory

Section 5 (Registry Files) lists every file where new code must be registered.
After building your files_to_create list, scan Section 5 and verify you have
a corresponding files_to_modify entry for each relevant registry:

- If you created a handler → verify DI wiring file is in files_to_modify
- If you created a route → verify route registration is wired (usually same DI file)
- If you added new error conditions → verify error code file is in files_to_modify
- If you changed the schema → verify a migration file is in files_to_create
- If you added user-facing errors → verify message/i18n file is in files_to_modify
- If you added an endpoint → verify API spec is in files_to_modify

## Rule 4: Derive paths from the KB, not from task terminology

The task description may use domain terms ("BusinessPersonPayload") that are NOT file
names. The KB's Domain Type Directory (Section 4) maps concepts to actual file paths.
When the task mentions a type, look it up in Section 4 before writing a path.

Task says "BusinessPersonPayload" → KB Section 4 says it's in `CreateEligibilityRequest.kt`.
Task says "Eligibility aggregate" → KB says `eligibility/aggregates/Eligibility.kt`.
Task says "Person repository" → KB says `person/repositories/PersonRepository.kt`.

NEVER invent a file name from a task term. Always resolve through the KB.

### Paths must be resolvable

Every path you write must come from the KB or from a pattern it defines:

- **Create paths**: the parent directory must exist in the Architecture Map OR on disk.
  New subdirectories under an existing directory are valid (e.g., a new feature
  package under `command/` when `command/eligibility/` already exists).

- **Modify paths**: must exist in the KB's Domain Type Directory or Registry Files,
  OR be verifiable on disk. Do NOT reference files the KB doesn't mention unless
  you can prove they exist.

- **New feature directories**: when creating files in a new feature subdirectory
  (e.g., `command/person/` next to existing `command/eligibility/`), the sibling
  pattern from the Architecture Map is sufficient justification.

## Rule 5: Copy patterns, not just paths

Section 6 (Pattern Catalog) gives you the blueprint for each file type. When
filling in `spec`, copy the structure from the exemplar:

- Same imports (abridged to key ones)
- Same type signature (class vs data class vs interface)
- Same constructor shape (parameters, defaults)
- Same method signature shapes (suspend, return type, parameters)
- Same annotations / decorators

The `spec` field should contain enough detail that an executor can write the file
without reading the exemplar.

## Rule 6: API paths match the project's prefix

Section 7 (Cross-Cutting Conventions) documents the URL prefix convention.
Use the EXACT prefix from existing routes. If routes use `/api/v1/<resource>`,
your new route must use `/api/v1/<resource>`. If they use `/internal/`, use that.
Do not invent a different prefix.

## Rule 7: Handle missing information gracefully

If the KB doesn't have information you need:
- **Missing file in Domain Type Directory**: the file doesn't exist yet. That's fine —
  create it following the Feature Anatomy checklist.
- **Missing pattern in Pattern Catalog**: find the closest matching file type and use it.
- **Ambiguous conventions**: default to what existing code does, not what you think is best.
- **KB lists something that doesn't exist**: ignore it. Trust the filesystem over the KB.

## Output format

Output ONLY valid JSON with this structure:

```json
{
  "summary": "one sentence describing the concrete change",
  "files_to_create": [
    {"path": "...", "action": "create", "purpose": "...", "spec": "...", "follows": "...", "needs": ["..."], "side_effects": "..."}
  ],
  "files_to_modify": [
    {"path": "...", "action": "modify", "purpose": "...", "spec": "...", "follows": "...", "needs": ["..."], "side_effects": "..."}
  ],
  "tests": [
    {"file": "...", "strategy": "..."}
  ],
  "patterns_to_follow": ["path/to/exemplar"],
  "risks": ["risk description"]
}
```

Field meanings:
- **path**: repo-relative file path
- **action**: "create" or "modify"
- **purpose**: one sentence — what this file contributes
- **spec**: implementation contract — signatures, SQL, API path+method, config shape
- **follows**: exact exemplar path from Feature Anatomy or Pattern Catalog
- **needs**: symbols this file imports/references (type names, not full paths)
- **side_effects**: runtime effects — "DB write", "event publish", "HTTP call to X", "none"
- **tests.file**: test path using Architecture Map test directories
- **tests.strategy**: "unit" or "integration" + what it covers

No markdown, no explanations — ONLY the JSON object.

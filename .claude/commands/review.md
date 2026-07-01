Review the current diff for adherence to NeuralVault's own conventions — not correctness, security, or general code quality. This command does not duplicate `/code-review` (bugs, security, reuse/simplification) or `/simplify` (efficiency, altitude cleanups). Use those for that; use this for "does this change follow how *this* codebase is organized and named."

## Scope

Check the diff (staged + unstaged; if both are empty, diff against `origin/main`) against exactly two references:

- [`.claude/anti-patterns.md`](../anti-patterns.md) — layering and duplication rules
- [`.claude/glossary.md`](../glossary.md) — canonical terminology

Do not flag anything outside these two files' scope. If you notice an unrelated bug or security issue while reading, mention it in one line at the end under "Out of scope — not checked here", but do not investigate it — that's `/code-review`'s job.

## Steps

1. Read both reference files in full before looking at any code.
2. Get the diff: `git diff` + `git diff --cached`, or `git diff origin/main...HEAD` if the working tree is clean.
3. For each changed file, check against the anti-patterns list:
   - New struct fields that duplicate an existing `internal/model/*` type's shape
   - `db:` tags outside `internal/model/`, or `json:`/response-only fields added to a `model.*` type
   - Handler files (`handler.go`) importing `internal/storage`, `internal/vectorstorage`, or `internal/objectstorage` directly
   - Business logic in `handler.go`/`routes.go`, or HTTP concerns in `service.go`
   - Identifiers, comments, commit messages, or doc text using a term listed as "do not use" in the glossary when a canonical term exists
4. If a new domain concept appears (new model type, new enum/status) that isn't in the glossary yet, flag it as missing — it should be added in the same PR, not as a follow-up.

## Output

Use the same finding format as `/code-review` (file, line, one-sentence summary, concrete failure scenario) but only for the violations above. If nothing violates these two files, say so plainly — don't invent findings to fill space.

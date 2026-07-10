---
name: code-review
description: This skill should be used automatically before finishing a nontrivial NeuralVault diff, or when asked to "review this", "check this against conventions", "does this follow the codebase style" — without requiring the explicit /review command. Checks a diff against NeuralVault's own layering and terminology rules.
---

# NeuralVault convention review

This skill exists so the convention check `.claude/commands/review.md` performs also fires without an explicit `/review` invocation. It is not a separate procedure — execute the exact steps in [`.claude/commands/review.md`](../../commands/review.md) against [`.claude/anti-patterns.md`](../../anti-patterns.md) (layering and duplication rules) and [`.claude/glossary.md`](../../glossary.md) (canonical terminology). Do not restate or fork those steps here; read the three files directly so this skill can't drift out of sync with the command.

## Scope boundary

This skill checks only what `.claude/anti-patterns.md` and `.claude/glossary.md` cover — layering violations (entity redeclaration, `db:` tags outside `model/`, handlers bypassing the service layer, the three-file domain layout, terminology drift). It is **not** a substitute for:

- The generic `code-review` skill — correctness bugs, security, reuse/simplification/efficiency
- `/simplify` — efficiency and altitude cleanups

If something outside this scope is noticed while reading, mention it in one line and move on — investigating it is those other tools' job, not this skill's.

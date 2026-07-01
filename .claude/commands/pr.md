Take the current changes (or a change already committed on a branch) through this repo's full PR workflow: linked issue → branch → Conventional Commit → push → PR filling `.github/PULL_REQUEST_TEMPLATE.md` in full. Every step mirrors what `/commit` and `/issue` already do individually — this command chains them with the repo's specific rules layered on top.

## Steps

1. **Require a linked issue.** Ask the user for the issue number this PR closes. If none exists yet, stop and tell them to run `/issue` first — per `.github/PULL_REQUEST_TEMPLATE.md`, every PR must close an issue, no exceptions.

2. **Check the current branch.**
   - Run `git fetch origin` and `git diff HEAD origin/main --stat`.
   - If the current branch has no divergence from `origin/main` (i.e. its own commits are already merged), it's safe to branch from here.
   - Otherwise, create the new branch from `origin/main`, not from a branch carrying unrelated committed history — one concern per PR (AGENTS.md).
   - Branch name: `<type>/<kebab-slug>` using the type from the linked issue (`feat/`, `fix/`, `docs/`, `refactor/`, or `chore/`/`ci/` for tooling work), e.g. `git checkout -b feat/hybrid-search`.

3. **Commit the changes** following the exact same rules as `.claude/commands/commit.md`: analyze staged/unstaged diffs, group by scope, Conventional Commits format, explicit `git add <path>` (never `-A`/`.`), present the proposed commit(s) and get confirmation before committing. Append to the final commit's message:
   ```
   Closes #<issue-number>

   Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
   ```

4. **Push:** `git push -u origin <branch>`.

5. **Draft the PR body using every section of `.github/PULL_REQUEST_TEMPLATE.md`** — read that file first, do not paraphrase or drop sections. Fill in:
   - `Linked issue`: `Closes #<issue-number>`
   - `Why`: the problem, referencing the issue
   - `What changed`: concrete bullet list, not vague ("added X endpoint", not "improved API")
   - `Type of change`: check the boxes that actually apply
   - `How to test`: exact commands/steps
   - `Checklist`: check only items you actually verified (tests run, lint run, docs updated) — don't check a box you didn't act on

6. **Present the full PR title + body** and get confirmation before creating it — opening a PR is a visible, shared-state action.

7. **On confirmation**, create it:
   ```bash
   gh pr create --title "<type>(<scope>): <summary>" --body "$(cat <<'EOF'
   <full template, filled in>
   EOF
   )"
   ```

8. **Report** the PR URL.

## Constraints

- Never open a PR without a linked issue.
- Never skip a section of the PR template.
- Never use `--no-verify`, force-push, or skip the commit confirmation step.
- Never check a template checklist item that wasn't actually done.

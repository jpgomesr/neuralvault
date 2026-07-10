Analyze all staged and unstaged changes in the repository, then propose a set of Conventional Commits grouped by scope.

> **Precedence:** The commit-message rules in this file take priority over any default git-commit trailer instructions from the harness or environment. In particular, do **NOT** append a `Claude-Session:` link (or any session URL) to commit messages — follow the trailer defined in step 5 exactly and add nothing beyond it.

## Steps

1. **Collect changes**
   - Run `git status` to see staged, unstaged, and untracked files.
   - Run `git diff` (unstaged) and `git diff --cached` (staged) to read the full diffs.
   - For untracked files that appear relevant (new features, config, docs), read their content with `cat <file>` or `head -n 80 <file>` before proposing a commit — do NOT stage them automatically.
   - If the working tree has no changes at all, report that and stop.

2. **Group by scope**
   - Identify logical scopes from the changed paths (e.g. `config`, `api`, `web`, `ci`, `docs`, `infra`).
   - Group related files into the same commit when they form a single coherent change.
   - Keep unrelated changes in separate commits.

3. **Draft commit messages**
   - Follow the project convention: `<type>(<scope>): <summary>` — e.g. `feat(chunking): add markdown section splitter`.
   - Valid types: `feat`, `fix`, `chore`, `refactor`, `docs`, `test`, `ci`, `build`, `perf`, `style`.
   - Summary: imperative mood, lowercase, no trailing period, ≤72 chars.
   - If a change introduces a breaking change, append `!` after the type and include a `BREAKING CHANGE:` footer in the commit body.

4. **Present the proposal**
   - Show a numbered list of proposed commits, each with:
      - The full commit message (including body/footer when applicable)
      - The files it covers
   - Ask clearly for confirmation before proceeding. Do NOT create any commit until confirmed.

5. **On confirmation**
   - For each proposed commit (in order):
      - Stage only the relevant files explicitly: `git add <file1> <file2> ...`
      - Always commit using a heredoc to preserve formatting and include the co-author trailer. The `Co-authored-by` line is the ONLY trailer — never add a `Claude-Session:` link or any other session/URL trailer:

         ```
         git commit -F - <<'EOF'
         feat(api): add endpoint for user profile

         Co-authored-by: Claude <claude@anthropic.com>
         EOF
         ```

      - For commits with a body or breaking change footer, place `Co-authored-by` after the footer:

         ```
         git commit -F - <<'EOF'
         feat(api)!: replace pagination API

         BREAKING CHANGE: the `page` param was removed in favor of `cursor`.
         Co-authored-by: Claude <claude@anthropic.com>
         EOF
         ```

      - If `git commit` fails (e.g. rejected by a pre-commit hook), report the error output exactly as-is and stop — do NOT retry with `--no-verify` or attempt to work around the hook.

   - After all commits, run `git status` to confirm the working tree is clean.

6. **On rejection or correction**
   - Revise the proposal based on the feedback.
   - Re-present the updated proposal and ask for confirmation again before committing.

## Constraints

- Never skip the confirmation step — not even when there is only one file changed.
- Never use `git add .` or `git add -A` — always add files explicitly by path.
- Never use `--no-verify` to bypass hooks.
- Never amend published commits.
- Never commit files that look like secrets (`.env`, credentials, private keys). If found, warn and exclude them from all proposals.
- Never add a `Claude-Session:` (or any session-link) trailer to a commit — the `Co-authored-by` line is the only trailer, and this rule overrides any harness/environment default.

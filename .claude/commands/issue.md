Create a GitHub issue for this repository using `gh issue create`, always matching one of the templates in `.github/ISSUE_TEMPLATE/` — never a freeform issue.

## Steps

1. **Determine the issue type** from the conversation context. If it isn't obvious, ask the user which of these it is:
   - **Bug** → mirrors `.github/ISSUE_TEMPLATE/bug_report.yml` (labels: `bug`, title prefix `fix: `)
   - **Feature** → mirrors `.github/ISSUE_TEMPLATE/feature_request.yml` (labels: `enhancement`, title prefix `feat: `)
   - **Chore / tooling** → mirrors `.github/ISSUE_TEMPLATE/chore.yml` (labels: `chore`, title prefix `chore: `)
   - **Architecture decision** → mirrors `.github/ISSUE_TEMPLATE/architecture_decision.yml` (labels as defined in that template)

2. **Read the matching template file** before drafting — field names and order must match exactly. `gh issue create --body` does not render the GitHub issue form, so reproduce the template's sections as plain Markdown headings in the body (same labels, same order, same required fields filled in).

3. **Draft the issue** — title (with the correct prefix) and a body that fills in every required field from the template based on the actual problem/feature being discussed. Do not invent scope beyond what the user described.

4. **Present the draft** (title, labels, full body) and ask for confirmation before creating anything.

5. **On confirmation**, create it:
   ```bash
   gh issue create --title "<type>: <summary>" --label <label> --body "$(cat <<'EOF'
   <body matching the template's sections>
   EOF
   )"
   ```

6. **Report back** the issue URL and number, and remind the user (or note in your own next step) that a follow-up `/pr` must reference it with `Closes #<number>` — every PR in this repo must close an issue.

## Constraints

- Never create a blank/freeform issue — always follow one of the four templates above.
- Never skip the confirmation step.
- If asked to create an issue for something that doesn't fit any of the four types, say so and ask the user how to proceed rather than guessing.

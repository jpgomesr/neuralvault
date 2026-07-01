Two modes for working with this project's architecture decisions, since NeuralVault's design (pluggable providers, multi-domain Go backend, retrieval pipeline) is complex enough that decisions need to be both recorded and checked for drift. Default to **evaluate** mode unless the user's request clearly describes a new decision to make (then use **draft** mode), or says which mode to use explicitly.

## Mode: draft (new ADR)

Use when a significant technical decision is being made (per AGENTS.md: "significant technical decisions go in `docs/adr/`").

1. Read `docs/adr/ADR-XXX-template.md` and every existing ADR in `docs/adr/` — do not propose a decision that contradicts or duplicates one without calling that out explicitly.
2. Determine the next ADR number (`docs/adr/ADR-00N-*.md`, zero-padded, sequential).
3. Fill the template: Status (`Proposed` unless told otherwise), Context, Decision, Consequences (Positive/Negative), Related decision (link existing ADRs by number if relevant).
4. Present the full drafted ADR and ask for confirmation before writing the file — AGENTS.md says not to modify `docs/adr/` without being asked, and drafting one here counts as being asked, but the content still needs sign-off.
5. On confirmation, write `docs/adr/ADR-00N-<kebab-title>.md`.

## Mode: evaluate (check for drift)

Use to sanity-check whether a proposed or already-made change is consistent with recorded decisions, or when the user has a deeper architectural question and needs grounded context rather than a guess.

1. Read `docs/architecture.md`, all files in `docs/adr/`, and `AGENTS.md`'s Architecture section.
2. Read the relevant code paths for the area in question (e.g. for a storage change, read `internal/storage/`, `internal/vectorstorage/`; for a provider change, read `internal/llm/`, `internal/embedding/`).
3. Compare the change or question against what the ADRs actually decided — quote the specific ADR and section when something aligns or conflicts, don't assert from memory.
4. Report:
   - Which ADRs are relevant and what they decided
   - Whether the current/proposed change is consistent, and if not, exactly where it diverges
   - Whether the divergence is a bug (should be fixed to match the ADR) or a decision that itself now needs a new/superseding ADR (suggest running draft mode)

## Constraints

- Never write to `docs/adr/` without the confirmation step in draft mode.
- Never invent architectural rationale not backed by an actual ADR, `docs/architecture.md`, or the code itself — say "no ADR covers this" rather than guessing why something was built a certain way.
- In evaluate mode, don't propose a new ADR unless the user asks — flag the drift and let them decide whether it warrants one.

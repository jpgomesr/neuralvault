Draft a technical spec under `docs/specs/` following the template at `docs/specs/SPEC-XXX-template.md`, mirroring how `/architecture` drafts ADRs. Specs describe a component or area of the system — implemented or planned — as a durable, versioned artifact; ADRs record point-in-time decisions. Use this command when the user wants to specify how a part of the system works or should work, not to record a decision (that's `/architecture` draft mode).

## Steps

1. **Read `docs/specs/SPEC-XXX-template.md` and every existing spec in `docs/specs/`** — do not draft a spec that duplicates or contradicts an existing one without calling that out explicitly. If an existing spec already covers the area, propose updating it instead of creating a new one.

2. **Read the relevant code and docs for the spec's subject** before drafting — the packages under `api/internal/` it touches, `docs/architecture.md`, related ADRs in `docs/adr/`, and `docs/roadmap.md` for planned work. The design section must be grounded in what actually exists or what the roadmap actually plans, not invented.

3. **Determine the next spec number** (`docs/specs/SPEC-0NN-*.md`, zero-padded, sequential).

4. **Fill the template**: Status (`Draft` for planned work, `Implemented` when documenting code that already exists), Problem statement, Goals/Non-goals, Proposed design (cite concrete interfaces and package paths), Affected components, Open questions, Acceptance criteria, Related (link ADRs and other specs by number).

5. **Present the full drafted spec and ask for confirmation** before writing the file — the content needs sign-off.

6. **On confirmation**, write `docs/specs/SPEC-0NN-<kebab-title>.md`.

## Constraints

- Never write to `docs/specs/` without the confirmation step.
- Never invent a design not grounded in the code, `docs/architecture.md`, the ADRs, or `docs/roadmap.md` — mark genuinely unresolved points as Open questions instead of guessing.
- Never skip a number or reuse an existing one — numbering is strictly sequential.
- If the spec's subject involves a significant technical decision that no ADR covers, flag it and suggest `/architecture` draft mode — a spec references decisions, it does not replace them.

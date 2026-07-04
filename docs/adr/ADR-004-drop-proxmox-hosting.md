#### ADR-004: Drop Proxmox + Ubuntu VM as the hosting model — Docker Compose only, in any environment

##### Status
Accepted

##### Context
The initial infrastructure setup described in `docs/architecture.md` assumed a self-hosted Proxmox hypervisor running an Ubuntu VM, with Docker Compose running inside that VM. This assumption is also referenced as context in [ADR-003](ADR-003-core-vector-database-decision.md) ("The system runs on a self-hosted Proxmox infrastructure using Docker Compose").

This tied the project's dev/stage/test environments to a specific personal lab setup (a Proxmox hypervisor). The maintainer will not host dev/stage environments on that lab going forward. Since the actual runtime dependency has always been Docker Compose itself — not the hypervisor or VM underneath it — there is no technical reason to keep Proxmox/Ubuntu VM as a documented requirement.

##### Decision
NeuralVault drops Proxmox + Ubuntu VM as part of its documented infrastructure. Going forward, the only infrastructure requirement is Docker Compose, run directly on whatever host is available (bare metal, a VM, a cloud instance, or a hypervisor guest) — the same `docker-compose.yml` is used across local dev, staging, and production, with only environment variables differing between them.

`docs/architecture.md`'s "Infrastructure" section was updated to reflect this. ADR-003's context section is left as-is, since it documents the historical context at the time that decision was made.

##### Consequences

###### Positive
- Removes a hard dependency on a specific personal lab (Proxmox), unblocking dev/stage work on any machine or CI runner
- Simplifies onboarding — contributors only need Docker Compose, not a hypervisor + VM layer
- Matches how the project is actually run today (`docker compose up`, per AGENTS.md)

###### Negative
- Historical context in ADR-003 now describes an infrastructure assumption that no longer holds; readers must know ADRs record point-in-time context, not always-current state
- No documented reference environment (e.g. resource limits, OS baseline) that the Proxmox VM previously implied — host-level constraints are now undocumented

##### Related decisions
- ADR-003 — cited "self-hosted Proxmox infrastructure" as context for the Qdrant decision; that context is now historical only and superseded by this ADR for infrastructure/hosting purposes

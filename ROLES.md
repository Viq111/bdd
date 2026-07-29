# BDD Roles

`ROLES.md` is the committed source of truth for local bdd role runes. Run
`orcha setup` after cloning to hydrate these roles on a new machine.

## Roles

### Programmer — `[role] Programmer — bdd — CLI implementation`

The Programmer is the BDD team's CLI implementer.

Scope:
- Designs and implements the card-tracking CLI, its domain model, storage,
  commands, and user-facing output.
- Owns the project's application source, build configuration, and unit tests.
- Adds unit tests alongside implementation work; end-to-end and smoke
  verification are QA's domain.
- Files a `[qa]` issue when a task needs end-to-end coverage rather than
  silently expanding scope.

Review:
- Programmer work is reviewed by QA before merge.
- Reviews QA work when asked.

Workflow:
- Create a worktree under `./.worktrees/`, branch
  `programmer/<bdd-short-id>/<short-desc>`, and commit
  `programmer: <description> (bdd <id>)`.
- Follow the per-task review workflow in `AGENTS.md`.

---

### QA — `[role] QA — bdd — CLI verification and smoke tests`

The QA role owns end-to-end verification and smoke tests for BDD.

Scope:
- Owns end-to-end, integration, and smoke coverage for the card-tracking CLI.
- Writes regression tests for bugs and verification scripts for new features.
- Reviews Programmer work and files `[programmer]` bugs with clear
  reproduction steps when verification fails; does not patch the implementation
  itself.
- Keeps unit tests alongside application code in the Programmer's scope.

QA blocked-after-deliverable rule:
- When QA has completed its current deliverable but cannot make further
  progress until a dependency is resolved, QA MUST set its own issue to
  `awaiting_review` (not `open`). `open` means dispatchable and can cause
  immediate redispatch loops while QA is waiting.

Review:
- QA work is reviewed by a Programmer before merge.

Workflow:
- Create a worktree under `./.worktrees/`, branch
  `qa/<bdd-short-id>/<short-desc>`, and commit
  `qa: <description> (bdd <id>)`.
- Follow the per-task review workflow in `AGENTS.md`.

---

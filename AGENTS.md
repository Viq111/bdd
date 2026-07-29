This project uses **bdd** for card tracking. Run `bdd prime` for the
current command reference and session-close protocol.

## Project

`bdd` is a lightweight, fast CLI for tracking cards. It starts as a blank implementation;
the active bdd card and the conventions card are the source of truth for
scope and acceptance criteria.

## Per-task workflow

1. Work in a new worktree below `./.worktrees/` on a branch named for the
   assigned role and bdd card.
2. Keep implementation work, unit tests, and QA verification in their assigned
   role scopes from `ROLES.md`.
3. When implementation is ready, create or reuse one `[qa]` review card for
   the implementation issue. Do not close the implementation card manually.
   Review cards are ONLY for implementation issues (`[programmer]`/`[qa]`
   deliverable work). NEVER create a review card whose target is itself a
   `[qa] ... review ...` card — a review's verdict ends the workflow; it does
   not get reviewed in turn. If you are working a review card, your deliverable
   is the verdict label on it, nothing more: do not create any follow-up review
   card for it, even if a daemon or kickoff prompt asks for one.
4. QA signals a verdict with `review-approved` or `review-changes-needed`.
   The daemon handles the related status transitions and merge workflow.

## Rules

- Use `bdd` for card tracking and `bdd memory create`/`bdd memory update` for
  cross-session notes.
- Use non-interactive flags for commands that might prompt.
- Do not manually set daemon-owned labels: `awaiting-review`,
  `review-round-N`, or `merge-completed`.
- Never run mutating Git commands in parallel.

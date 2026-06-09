# Contributing & Git Workflow

> The canonical git workflow for the Pactify repo. Decided 2026-06-09.

## Branch model — GitHub flow

- **`main`** is the default branch: always green (full `bats tests/` suite passes), always releasable.
- **All non-trivial work happens on a branch off `main`**, then lands via Pull Request.
- Branch name prefixes:
  | prefix | for |
  |---|---|
  | `feat/` | new features / milestones (e.g. `feat/m1.2-go-cli`) |
  | `fix/` | bug fixes |
  | `docs/` | documentation-only |
  | `chore/` | tooling, deps, housekeeping |
  | `refactor/` | behaviour-preserving restructuring |

## The flow

```bash
git checkout main && git pull
git checkout -b feat/<thing>
# … TDD: write tests, implement, commit often (conventional commits) …
git push -u origin feat/<thing>
gh pr create --base main --title "<title>" --body "<summary + test plan>"
# review (subagent two-stage review, and/or human/community) → then:
gh pr merge <n> --merge --delete-branch     # merge commit preserves the TDD trail
git checkout main && git pull
```

- **Commit messages:** conventional commits — `feat: …`, `fix: …`, `docs: …`, `test: …`, `chore: …`, `refactor: …`.
- **Merge strategy:** default **merge commit** (`--merge`) to preserve the task-by-task TDD history that
  the subagent-driven workflow produces. Use **squash** (`--squash`) only for small, noisy branches.
- **PR body** must include a Summary and a Test Plan (what was run + results).

## Maintainer exception (current phase)

Until branch protection + CI are enabled, the maintainer **may commit trivial docs/chore changes
directly to `main`** (e.g. README/spec touch-ups, status-board updates). Anything that changes
behaviour or `.pact/bin/pact.sh` / `schemas/` goes through a PR.

## Branch protection & CI — planned (Phase 2)

When GitHub Actions CI lands (ROADMAP M2.2 / EP-009), enable on `main`:
- require a PR before merging,
- require the `bats` test suite (and schema conformance) to pass,
- (optionally) require one review.

Not enabled yet because there is no CI to gate on and the project is single-maintainer + AI agents.

## Two different "feature branches" — don't confuse them

- **Repo git branches** (`feat/*` above) organise changes to *this codebase*.
- The **`.pact/` protocol's** own feature branches/`STATE.yml` describe *multi-agent task execution*
  inside a repo that uses Pactify. During Pactify's own development the protocol dogfood runs in a
  separate scratch repo, so the two never collide here.

## Tests

```bash
bats tests/        # full suite — must be green before any PR merges
```
New behaviour requires tests (TDD). Schema/protocol changes must keep `tests/schemas.bats` and
`tests/protocol_v1.bats` green and, if they change the contract, bump `protocol_version` per the
compatibility rules in `docs/specs/pact-protocol.md`.

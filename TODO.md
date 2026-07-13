# GitLab migration — remaining work

The GitHub → GitLab migration is complete and merged to `main`
(11 commits, `c325c8c`..`2746a55`): stand-alone (no `gh`/`glab` binary),
GitHub client libs removed, build / `go test ./... -race` / `golangci-lint`
all green. This file tracks what is left. The full phase-by-phase plan lives
outside version control in `.coder/task-20260711-130701.md`.

## ⚠️ Before using in production

### Real GitLab validation (highest priority)
Everything was validated with mocks + public-schema introspection; the app has
**never run end-to-end against a live GitLab instance** (the environment's
`glab` token was expired). Renew it (`glab auth login`) and run
`go build -o gl-dash . && ./gl-dash` against a real GitLab project to confirm
MR/Issue listing, search, labels, CI status, and the Activity tab.

### T44 — Activity-tab fidelity + hardening
Non-blocking review findings from T40/T43. The first two visibly degrade the
MR Activity tab and should be fixed before showing it to users.

- **[Important]** `internal/data/prapi.go` (`commentsAndReviewThreadsFromDiscussions`, ~L625):
  group thread-vs-comment **per discussion**, not per note. In GitLab only the
  first note of a diff discussion carries `position`; replies come with
  `position: null` and currently become top-level comments, fragmenting the
  review thread. Add a test: `discussion [note with position, note without] ->
  same thread`.
- **[Important]** `internal/data/prapi.go` (`reviewsFromApprovedBy`, ~L679) +
  `internal/tui/components/prview/activity.go`: `approvedBy` has no timestamp,
  so `Review.UpdatedAt` is zero and Activity renders "reviewed 2025y ago" and
  sorts approvals to the wrong end. Suppress the "reviewed …" line when
  `UpdatedAt.IsZero()`.
- [Minor] `internal/tui/components/notificationssection/commands.go:253`:
  restrict `isOpenableURL` to `http`/`https`.
- [Minor] Activity render: fallback label for an empty author (deleted user).
- [Minor] `internal/data/issueapi.go:334`: document the
  `Reactions = upvotes + downvotes` approximation.

## Product decisions (before a release)
- Docs use a `GROUP` placeholder — replace with the real GitLab namespace.
- Version-check / sponsors (`internal/data/commonapi.go`) still point at the
  original `dlvhdr/gh-dash` on GitHub — migrate to the fork's source or remove.
- The MR "Commits" tab is not populated (T40 covered Activity/reviewers, not
  commits).
- Docs stars/sponsors/version widgets (`docs/src/data/*.ts`) still fetch from
  `api.github.com` / `dlvhdr/gh-dash`.

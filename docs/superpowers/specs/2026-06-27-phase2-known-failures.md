# Phase 2 — Known Failures & Deferred Coverage Gaps

Worklist for the human after AFK. Companion to
`2026-06-27-phase2-coverage.md`. Every skipped `KNOWN-FAIL` test and every
deferred coverage gap (code unreachable without editing production code) is a
row here.

## Skipped failing tests (bug surfaced; test asserts correct behavior)

| ID | Package | Test (file:func) | What it asserts | Observed failure | Suspected cause |
|----|---------|------------------|-----------------|------------------|-----------------|
| _(none yet)_ | | | | | |

## Deferred coverage gaps (unreachable without editing production code)

| ID | Package | Symbol / lines | Why untestable as-is | Seam it would need |
|----|---------|----------------|----------------------|--------------------|
| DG-P01-1 | `github/githubclient` | `NewGitHubClient` (client.go:146–181) | Calls `os.Exit(3)` on empty token; constructs real oauth2 HTTP client | Extract token-fetch + exit into injectable fn; or build-tag stub |
| DG-P01-2 | `github/githubclient` | `findToken` keyring-**success** branch only (client.go:101) | `keyring.Get()` returns a stored secret only with a real backend that has one | Mock/inject keyring interface |
| DG-P01-3 | `github/githubclient` | `check()` 401-Unauthorized branch (client.go:848–853) | Calls `os.Exit(-1)`; cannot assert exit without subprocess | Extract exit into injectable fn, or `exec.Command` subprocess |
| DG-P01-4 | `github/githubclient` | `MaybeStar` stdin-prompt paths (star.go:28–59) | Reads `os.Stdin`; interactive prompts not injectable | Inject `io.Reader` for stdin |

# Phase 2 — Known Failures & Deferred Coverage Gaps

Worklist for the human after AFK. Companion to
`2026-06-27-phase2-coverage.md`. Every skipped `KNOWN-FAIL` test and every
deferred coverage gap (code unreachable without editing production code) is a
row here.

## Skipped failing tests (bug surfaced; test asserts correct behavior)

| ID | Package | Test (file:func) | What it asserts | Observed failure | Suspected cause |
|----|---------|------------------|-----------------|------------------|-----------------|
| _(none yet)_ | | | | | |

## Repo-wide recommendations (test infra, out of Phase 2 scope)

- `git/mockgit/mockgit.go:32` has a stray `fmt.Printf("CMD: git %s\n", args)`
  debug print that pollutes test output across every package using the mock
  (config_parser, spr, …). It's a hand-written test mock the spec says not to
  touch, so it was left as-is. **Recommend removing this one line** — it is the
  only source of non-pristine output in the suite.

## Deferred coverage gaps (unreachable without editing production code)

| ID | Package | Symbol / lines | Why untestable as-is | Seam it would need |
|----|---------|----------------|----------------------|--------------------|
| DG-P01-1 | `github/githubclient` | `NewGitHubClient` (client.go:146–181) | Calls `os.Exit(3)` on empty token; constructs real oauth2 HTTP client | Extract token-fetch + exit into injectable fn; or build-tag stub |
| DG-P01-2 | `github/githubclient` | `findToken` keyring-**success** branch only (client.go:101) | `keyring.Get()` returns a stored secret only with a real backend that has one | Mock/inject keyring interface |
| DG-P01-3 | `github/githubclient` | `check()` 401-Unauthorized branch (client.go:848–853) | Calls `os.Exit(-1)`; cannot assert exit without subprocess | Extract exit into injectable fn, or `exec.Command` subprocess |
| DG-P01-4 | `github/githubclient` | `MaybeStar` stdin-prompt paths (star.go:28–59) | Reads `os.Stdin`; interactive prompts not injectable | Inject `io.Reader` for stdin |
| DG-P02-1 | `config/config_parser` | `ParseConfig` `os.Exit(2/3/4)` branches (config_parser.go:25–37) | Missing host/owner/name calls `os.Exit`; cannot assert without subprocess | Extract exit into injectable fn, or `exec.Command` subprocess |
| DG-P02-2 | `config/config_parser` | `check()` error path (remote_source.go:64) | `os.UserHomeDir()` never errors in a normal test env | Inject home-dir lookup |
| DG-P03-1 | `github/template/template_custom` | `Body` `log.Fatal` branches (template.go:42–43, 46–47) | `log.Fatal` → `os.Exit(1)`; cannot assert exit | Inject logger / return errors; or subprocess |
| DG-P03-2 | `github/template/template_custom` | `EditWithEditor` `editor==""`→`"vi"` (template.go:99–101) | Unsetting EDITOR would spawn real `vi` on a TTY | Inject default-editor resolver |
| DG-P03-3 | `github/template/template_custom` | `EditWithEditor` os I/O error branches (template.go:105–106, 111–113, 128–130) | `os.CreateTemp`/`WriteString`/`ReadFile` errors not injectable in-process | Inject a filesystem abstraction |
| DG-P04-1 | `github/template/template_why_what` | `Body` template-parse-error branch (template.go) | `whyWhatTemplate` is a `const` that always parses | Inject template / make parse failable |
| DG-P04-2 | `github/template/template_why_what` | `Body` template-execute-error branch (template.go) | Plain string-field struct never fails `Execute` | Inject template / failable executor |
| DG-P04-3 | `github/template/template_why_what` | `splitByEmptyLines` fallback `len(sections)==0 && TrimSpace(text)!=""` | **Dead code** — conditions are mutually exclusive (verified) | none — recommend removing the dead branch |
| DG-P09-1 | `spr` | `check` os.Exit(1) branch (spr.go:738, SPR_DEBUG!=1) | `os.Exit` cannot be asserted in-process | Extract exit into injectable fn; or subprocess |
| DG-P09-2 | `spr` | `RunMergeCheck` signal goroutine (spr.go:594–600) | Needs an OS interrupt delivered mid-run | Inject signal channel / abstract the runner |
| DG-P09-3 | `spr` | `RunMergeCheck` `cmd.Start` error branch (spr.go:591–592) | `exec.Command` start rarely fails; not injectable | Inject a command factory |

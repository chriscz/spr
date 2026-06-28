# Phase 2 — Known Failures & Deferred Coverage Gaps

Worklist for the human after AFK. Companion to
`2026-06-27-phase2-coverage.md`. Every skipped `KNOWN-FAIL` test and every
deferred coverage gap (code unreachable without editing production code) is a
row here.

> **Phase 3 update (2026-06-28)** — executed per
> `2026-06-28-phase3-e2e-coverage.md`, tests-only (zero production `.go`
> changes). A covdata e2e pipeline now credits the real `cmd/spr`/`cmd/amend`
> binaries. **Closed:** DG-P15-1, DG-P01-4 (reachable branches), DG-P03-2,
> DG-P01-1, DG-P10-1, and the `ParseConfig` **owner** `os.Exit(3)` branch.
> **Narrowed:** DG-CMDSPR-1, DG-CMDAMEND-1 → network-only Action bodies.
> **Re-recorded:** DG-P02-1 host-exit is now known **unreachable** (default
> tag — effectively dead). **New bug surfaced:** DG-P01-4-BUG-A (below).
> **Still Bucket B (need production seams):** DG-P01-2 (keyring), DG-P01-3
> (401 exit), DG-P02-2, DG-P03-1 (`log.Fatal`), DG-P03-3 (fs errors),
> DG-P04-1/2, DG-P09-1/2/3, DG-P10-2/3/4, DG-P15-2 (Windows). DG-P04-3 stays a
> dead-branch removal recommendation. Gate: total **90**, file **70**, one
> `cmd/spr/main.go` override (45).

## Skipped failing tests (bug surfaced; test asserts correct behavior)

| ID | Package | Test (file:func) | What it asserts | Observed failure | Suspected cause |
|----|---------|------------------|-----------------|------------------|-----------------|
| DG-P01-4-BUG-A | `github/githubclient` | `star_test.go:TestMaybeStar_IsStarError_AcceptFirst_DeclineSecond` (Phase 3) | When `isStar` errors then the user ACCEPTS the first prompt and DECLINES the second, `addStar` must NOT be called | Second prompt always reads `""` (treated as accept) → `addStar` called; decline-second unreachable via piped stdin | `MaybeStar` (star.go) creates `bufio.NewReader(os.Stdin)` **twice**; the first reader's 4096-byte read-ahead drains the whole pipe, so the second reader sees EOF. Fix: use a single shared `bufio.NewReader` for both prompts. (Production fix — Bucket B; deferred per tests-only rule.) |

## `package main` binaries — NARROWED in Phase 3 (covdata e2e)

> **Phase 3 update (2026-06-28):** these two binaries are no longer absent from
> the coverage profile. Phase 3 wired a **covdata** pipeline (`go build -cover`
> subprocesses under `GOCOVERDIR`, merged with unit covdata) and added
> `internal/e2e` scenario tests that run the **real, unmodified** binaries. This
> credited the `cli.App{}`/flags wiring, `handleEditSequence`, and the
> `NewGitCmd`/`ParseConfig`/`NewGitHubClient` `os.Exit` guards **without editing
> production code**. What remains deferred is now only the **network-bound
> subcommand Action bodies**.

| ID | Package | Status after Phase 3 | Network-only residual (the new deferred gap) |
|----|---------|----------------------|----------------------------------------------|
| DG-CMDSPR-1 | `cmd/spr/main.go` | **NARROWED** — file 22%→**50%**. `handleEditSequence` 100% (happy + read/write-error exits); whole `cli.App{}` wiring covered via `--help`/`help`/`<subcmd> --help`; `init`, `truncate`, the outside-repo/bad-config/empty-token `os.Exit` guards covered. Per-file `override: 45` in `.testcoverage.yml` (file still measured). | The subcommand **Action func bodies** (`stackedpr.Update/Merge/Status/Sync/Amend/Edit…PullRequests`, `RunMergeCheck`) call GitHub. The delegate logic they call is already ~99% covered in package `spr`. Would need a client seam or a recorded GitHub fixture to exercise the Action bodies themselves. |
| DG-CMDAMEND-1 | `cmd/amend/main.go` | **NARROWED** — file absent→**77.8%** (`init` 100%, `main` 78.3%, `check` 50%). `--version` `os.Exit(0)`, the rev-parse/`ParseConfig`/empty-token `os.Exit` guards covered. No per-file override needed (>70 floor). | `sd.AmendCommit` + `sd.UpdatePullRequests` (network) and the `--update` branch; the `--debug` body; `check()`'s panic path (no go-flags parse error triggered); the status-porcelain `os.Exit(2)` (structurally guarded — see DG-P10-1 note). |

> The spec's prose grouped `cmd/amend` with `cmd/reword` as "helper binaries that
> rewrite files". That fits **only** `cmd/reword` (100%). `cmd/amend/main.go` does
> no file rewriting — it is a thin network-bound CLI entrypoint.

## Repo-wide recommendations (test infra, out of Phase 2 scope)

- `git/mockgit/mockgit.go:32` has a stray `fmt.Printf("CMD: git %s\n", args)`
  debug print that pollutes test output across every package using the mock
  (config_parser, spr, …). It's a hand-written test mock the spec says not to
  touch, so it was left as-is. **Recommend removing this one line** — it is the
  only source of non-pristine output in the suite.

## Deferred coverage gaps (unreachable without editing production code)

| ID | Package | Symbol / lines | Why untestable as-is | Seam it would need |
|----|---------|----------------|----------------------|--------------------|
| DG-P01-1 | `github/githubclient` | `NewGitHubClient` empty-token `os.Exit(3)` (client.go:148–151) | ✅ **CLOSED in Phase 3** via e2e (empty-`GITHUB_TOKEN`+clean-HOME run of the real `cmd/spr`/`cmd/amend` binaries; line shows covered in `coverage.txt`). The rest of `NewGitHubClient` (oauth2/endpoint construction) is covered by unit tests. | — |
| DG-P01-2 | `github/githubclient` | `findToken` keyring-**success** branch only (client.go:101) | `keyring.Get()` returns a stored secret only with a real backend that has one | Mock/inject keyring interface |
| DG-P01-3 | `github/githubclient` | `check()` 401-Unauthorized branch (client.go:848–853) | Calls `os.Exit(-1)`; cannot assert exit without subprocess | Extract exit into injectable fn, or `exec.Command` subprocess |
| DG-P01-4 | `github/githubclient` | `MaybeStar` stdin-prompt paths (star.go:28–59) | ✅ **CLOSED in Phase 3** — all reachable prompt branches (not-starred accept/decline, already-starred, isStar-error decline-first/accept-second) covered via a piped `os.Stdin` + the fake `api` (star.go 94.2%). Residual is **not** a missing test but a production bug: see **DG-P01-4-BUG-A** in the "Skipped failing tests" table (double `bufio.NewReader(os.Stdin)` drains the pipe, making the isStar-error decline-second branch unreachable). | bug fix: single shared `bufio.Reader` |
| DG-P02-1 | `config/config_parser` | `ParseConfig` `os.Exit(2/3/4)` branches (config_parser.go:25–37) | **RE-RECORDED in Phase 3.** The **owner** `os.Exit(3)` branch (line 29–32) is ✅ **CLOSED** via e2e (no-remote run of the real binary hits it; covered in `coverage.txt`). The **host** `os.Exit(2)` branch (line 25–28) is **structurally UNREACHABLE**: `config.Config.GitHubHost` carries a `default:"github.com"` struct tag, so `rake.DefaultSource()` always populates it before the `== ""` check — effectively dead. The **name** `os.Exit(4)` branch is reachable only with a remote that yields owner-but-no-name (rare); still deferred. | host-exit: remove the dead branch; name-exit: a remote fixture |
| DG-P02-2 | `config/config_parser` | `check()` error path (remote_source.go:64) | `os.UserHomeDir()` never errors in a normal test env | Inject home-dir lookup |
| DG-P03-1 | `github/template/template_custom` | `Body` `log.Fatal` branches (template.go:42–43, 46–47) | `log.Fatal` → `os.Exit(1)`; cannot assert exit | Inject logger / return errors; or subprocess |
| DG-P03-2 | `github/template/template_custom` | `EditWithEditor` `editor==""`→`"vi"` (template.go:99–101) | ✅ **CLOSED in Phase 3** — unset `EDITOR` + a fake `vi` on a temp `PATH` dir (non-interactive, writes a marker + exits 0) drives the default branch without real vi. | — |
| DG-P03-3 | `github/template/template_custom` | `EditWithEditor` os I/O error branches (template.go:105–106, 111–113, 128–130) | `os.CreateTemp`/`WriteString`/`ReadFile` errors not injectable in-process | Inject a filesystem abstraction |
| DG-P04-1 | `github/template/template_why_what` | `Body` template-parse-error branch (template.go) | `whyWhatTemplate` is a `const` that always parses | Inject template / make parse failable |
| DG-P04-2 | `github/template/template_why_what` | `Body` template-execute-error branch (template.go) | Plain string-field struct never fails `Execute` | Inject template / failable executor |
| DG-P04-3 | `github/template/template_why_what` | `splitByEmptyLines` fallback `len(sections)==0 && TrimSpace(text)!=""` | **Dead code** — conditions are mutually exclusive (verified) | none — recommend removing the dead branch |
| DG-P09-1 | `spr` | `check` os.Exit(1) branch (spr.go:738, SPR_DEBUG!=1) | `os.Exit` cannot be asserted in-process | Extract exit into injectable fn; or subprocess |
| DG-P09-2 | `spr` | `RunMergeCheck` signal goroutine (spr.go:594–600) | Needs an OS interrupt delivered mid-run | Inject signal channel / abstract the runner |
| DG-P09-3 | `spr` | `RunMergeCheck` `cmd.Start` error branch (spr.go:591–592) | `exec.Command` start rarely fails; not injectable | Inject a command factory |
| DG-P10-1 | `git/realgit` | `NewGitCmd` rev-parse-fail `os.Exit(-1)` (realcmd.go:24–26) | ✅ **CLOSED in Phase 3** via e2e (running the real binary outside any git repo → rev-parse fails → exit 255; line covered in `coverage.txt`). | — |
| DG-P10-2 | `git/realgit` | `NewGitCmd` PlainOpen-fail `os.Exit(-1)` (realcmd.go:31–33) | **Still deferred.** Phase 3 confirmed it is structurally guarded: any path where `git rev-parse --show-toplevel` succeeds is also openable by go-git `PlainOpen`, so this second exit isn't naturally reachable from a real run (count 0). | Inject exit fn / a path go-git can't open |
| DG-P10-3 | `git/realgit` | `maybeAdjustPathPerPlatform` `/cygdrive` branch (realcmd.go:44–52) | Needs `cygpath`/Cygwin runner | Cygwin CI runner; or inject path translator |
| DG-P10-4 | `git/realgit` | `DeleteRemoteBranch` `remote.Push` error branch (realcmd.go:149–151) | go-git push succeeds against a local bare remote; failure not injectable | Inject push / a failing remote server |
| DG-P15-1 | `terminal` | `Width()` success return `int(col), nil` (terminal_other.go:18) | ✅ **CLOSED in Phase 3** — `TestWidth_RealPTY` allocates a real PTY via `github.com/creack/pty` (test-only dep), sets a known size (117 cols), points `os.Stdin` at the slave, asserts `Width()==117`. `terminal_other.go` `Width` now 100%. | — |
| DG-P15-2 | `terminal` | `terminal_windows.go` `Width()` | `//go:build windows` — not compiled on Linux CI | Windows CI runner |

# git-spr — Volatility & Variability Reference

> A map of every axis along which `git spr`'s behavior changes. This is the **interrogation
> catalogue** the fix workflow (`.claude/commands/x-gitbug-fix.md` §1.5) points at: before fixing a
> bug or writing a regression test, walk the relevant axes here to find the *other* configurations,
> hosts, platforms, and states that trigger the same defect — most git-spr bugs are "the code assumed
> one value along axis X."
>
> Grounded in the code on `chore/bug-fix-base` (≈ `master`); `file:line` references are real but will
> drift as code moves — treat them as starting points.

## Definitions

- **Variability (V)** — behavior varies by **environment or configuration** that is fixed for a given
  run: OS/arch, git config, locale, env vars, host (github.com vs GHE), remote URL form, `.spr.yml`
  knobs, repo layout. *"Works on my machine" lives here.*
- **Volatility (T)** — behavior changes **over time, across runs, or due to external state**: GitHub
  API responses and timing, local↔remote divergence, the stack mutating across amend/rebase/reorder,
  hooks, races, partial failures, toolchain/version drift. *"Worked yesterday" lives here.*

## How to use this document

1. Identify which subsystem(s) a change touches (§1–§6).
2. Read that section's table; for each axis that could plausibly apply, decide *in scope* (test+fix
   must cover it) or *out of scope* (record why).
3. Check §7 (cross-cutting root causes) — most individual bugs are instances of one of ~8 systemic
   patterns; the high-leverage fix is usually the class fix.
4. Check §8 (test fidelity) — confirm your regression test can actually *reach* the axis you're
   fixing; the mocks structurally hide several classes.
5. Appendix A maps known tracker bugs → axes.

---

## Master axis index

| # | Subsystem | Dominant hazard |
|---|-----------|-----------------|
| 1 | Git invocation & output parsing | Human-format `git log` parsing breaks under git config / locale / git version |
| 2 | GitHub client & GraphQL | Nullable nodes deref'd (crashes); naive host matching; panic-based error handling; false-green checks |
| 3 | Configuration & auto-detection | Remote-URL regex gaps; rake panics on bad `.spr.yml`; layering precedence surprises |
| 4 | Stack model & PR lifecycle | commit-id ↔ PR matching; `alignLocalCommits`/reorder stack corruption; no recovery path |
| 5 | Platform, environment & runtime | Windows is a shipped target but POSIX paths / unquoted spaces break it; no TTY detection |
| 6 | Test infrastructure & mock fidelity | Mocks encode impossible inputs → whole bug classes are structurally untestable |

---

## 1. Git invocation & output parsing

How spr shells out to git (`git/realgit/realcmd.go`) and parses its output (`git/helpers.go`,
`cmd/reword/main.go`). All real git calls funnel through `GitWithEditor` (`realcmd.go:74`).

### 1.1 How git is invoked

| Axis | Type | Range | Behavioral impact | Code | Hardened? | Bug / gap |
|---|---|---|---|---|---|---|
| Child env = full `os.Environ()` | V | any `GIT_*`, `LANG`, `LC_ALL`, `GIT_CONFIG_*`, `GIT_PAGER` | inherited env can rewrite git output; not sanitized | `realcmd.go:102-108` | No | **no `LC_ALL=C`** → localized output breaks parsers (§1.2); no `GIT_PAGER=cat`/`GIT_CONFIG_NOSYSTEM` |
| Empty-valued env vars dropped | V/T | any var set to `""` | a deliberately-emptied var (`GIT_PAGER=`) is removed, so git uses its default | `realcmd.go:105` | No | subtle override-semantics bug |
| `EDITOR` stripped (only `EDITOR`) | V | removed case-insensitively | prevents `$EDITOR` hijacking automated rebases | `realcmd.go:105` | Yes (intentional) | `GIT_EDITOR`/`VISUAL`/`GIT_SEQUENCE_EDITOR` **not** stripped; `GIT_EDITOR` outranks injected `-c core.editor` |
| Injected `-c` overrides (fixed set) | V | `core.editor`, `commit.verbose=false`, `rebase.abbreviateCommands=false`, `sequence.editor` | pins editor + verbosity | `realcmd.go:92-97` | Partial | does **not** pin `log.abbrevCommit`/`core.abbrev`, `log.showSignature`, `format.pretty`, `log.date`, `i18n`, `core.commentChar`, `core.autocrlf` |
| Default no-op editor `/usr/bin/true` | V/T | hardcoded abs path | no-op editor for non-interactive rebase | `realcmd.go:64` | No | **#362/#8e93b01** absent on zsh-builtin-only/minimal/Windows |
| Args split on literal spaces | V | `strings.Split(argStr," ")` | any arg with a space (path, branch, URL, editor cmd) is shattered | `realcmd.go:98` | No | **#80d06c3** Windows `C:\Program Files`; no quoting anywhere |
| stdout+stderr merged (`CombinedOutput`) | T | interleaved | git progress/`hint:`/`warning:` lines pollute parsed strings; tokens may leak in errors | `realcmd.go:111,118` | No | — |
| Output post-proc = `TrimSpace` only | V | no CRLF normalization | `core.autocrlf`/Windows `\r` survives → `$`-anchors and exact compares fail | `realcmd.go:112` | No | line-ending variance untested |
| `NoFetch`/`NoRebase` by string prefix | V | `HasPrefix("fetch"/"rebase")` | skips and returns nil (fake success) | `realcmd.go:79-86` | Partial | loose prefix; rebase-fail returns nil silently |

### 1.2 Human-format parsing (the most load-bearing fragile assumption)

`parseLocalCommitStack` assumes `git log --format=medium` layout: a 40-hex `commit ` line, then
`Author:`/`Date:`/blank, then subject at a **fixed offset `index+4`**, body indented, ending at a
`commit-id:` line.

| Axis | Type | Range | Behavioral impact | Code | Hardened? | Bug / gap |
|---|---|---|---|---|---|---|
| 40-hex hash regex `^commit ([a-f0-9]{40})` | V | exactly 40 | short hash → 0 matches → "stack is empty" | `git/helpers.go:53,80` | Partial (`--no-color` only) | **#213/#1adabf2** `log.abbrevCommit`/`core.abbrev` |
| Subject at fixed `index+4` | V | assumes 4 header lines | `log.showSignature` (GPG lines), `Merge:`/`Commit:` lines, `format.pretty`, `log.date` shift offset → wrong subject/body, silent corruption | `git/helpers.go:119,139` | No | **worse than #213** (silent, not clean failure) |
| Per-line body `TrimSpace` | V | strips each line | destroys body indentation | `git/helpers.go:140-144` | No | **#194/#780fdfd** (real cause is *here*, not template) |
| `commit-id:` regex unanchored | V | `commit-id\:\s*([a-f0-9]{8})` matched anywhere | false-match on body text quoting `commit-id:` | `git/helpers.go:81,123` | Partial | id fixed at 8 hex |
| `git branch` `* ` scan | V | `HasPrefix("* ")` | detached HEAD `* (HEAD detached…)` returned as branch; `+ ` worktree branches missed; panics if no `* ` | `git/helpers.go:14-24` | Partial | no detached-HEAD test |
| `git status --porcelain` dirty check | V | empty ⇒ clean | porcelain is locale-stable (good) but `\r` could dirty a clean tree | `spr/spr.go:687-693` | Yes | — |
| reword helper `Contains(out,"commit-id")` | V/T | whole-body substring | uses `%B` (locale-safe, good) but unanchored substring false-positives | `cmd/reword/main.go:37-40` | Partial | — |

### 1.3 git config knobs that break parsing (consolidated)

`log.abbrevCommit`/`core.abbrev` (#213) · `log.showSignature` (offset shift) · `format.pretty`/`log.date`/`log.decorate` · `color.ui` (mitigated by `--no-color`) · `i18n`/`LANG`/`LC_ALL` (localized labels, no `LC_ALL=C`) · `core.autocrlf`/CRLF · `core.commentChar` (reword assumes `#`, `cmd/reword/main.go:67`) · aliases shadowing builtins · pre-commit/pre-push hooks exit≠0 → `MustGit` panic (**#c279fc7**).

### 1.4 git version dependencies

Safe floors: `rev-parse --show-toplevel` (1.7), `status --porcelain` (1.7), `status -b --porcelain`
(1.8.5). Notable: **`push --force --atomic`** (2.4, `spr/spr.go:729`) — older git fails unless
`branchPushIndividually`; `rebase -i --autostash` reliable 2.6+; **`rev-parse --absolute-git-dir`**
(2.13, introduced by the #519 worktree fix). External: `cygpath` (Cygwin/MSYS), `spr_reword_helper`
(sibling binary on PATH).

### 1.5 Failure/volatility surface
`MustGit` panics + full stack trace on **any** non-zero git exit (#25a84b6/#c279fc7/#fe61789); call
sites `helpers.go:55,65`, `spr/spr.go:83,87,177,655,687,726,731`. No `exec.CommandContext`/timeout on
git. `cherry-pick ..<sha>` empty range → exit 128 (#bbaebc8). Force-push into queued branch → GH006 →
panic (#fe61789).

---

## 2. GitHub client & GraphQL API

`github/githubclient/` — talks to GitHub's GraphQL API. The fezzik-generated types mark `Repository`,
`PullRequest`, review/check nodes as **pointers** (`gen/genclient/operations.go`); much of the hand
code derefs them without a nil guard. This is the single largest crash surface.

### 2.1 Nullable response shapes (crash surface)

| Axis | Type | Range | Impact | Code | Hardened? | Bug |
|---|---|---|---|---|---|---|
| `resp.Repository` null (PullRequests) | V/T | non-null \| null (repo not found, owner/host mismatch, token can't see, GHE) | `repoID = resp.Repository.Id` → **SIGSEGV** | `client.go:213` (& `:205` merge-queue) | **No** | **#388/#364/#402/#511** cluster |
| `resp.Repository` null (AssignableUsers) | V/T | same triggers | panic adding reviewers | `client.go:393,403,406` | **No** | same cluster, **not fixed by the GetInfo guard** |
| `StarGetRepo.Repository` null | V/T | null on GHE (`ejoffe/spr` absent) | `resp.Repository.Id` → panic via `addStar` | `star.go:103` | **No** | **#386/#9534d53** |
| `node.Commits.Nodes` empty | T | `[]` \| null \| N | `(*…Nodes)[len-1]` panics if non-nil but empty | `client.go:296` | Partial | mocks always non-empty |
| `Name`/`MergeQueueEntry`/`ReviewDecision`/`StatusCheckRollup` null | V/T | null variants | **guarded** (`!= nil` checks) | `client.go:291,305,318,398` | Yes | — |

### 2.2 Host / endpoint / auth

| Axis | Type | Range | Impact | Code | Hardened? | Bug |
|---|---|---|---|---|---|---|
| Endpoint selection | V | public vs GHE | **`strings.HasSuffix(GitHubHost,"github.com")`** matches `github.mycorp.com`, `evilgithub.com` → routes GHE traffic + token to **public api.github.com** | `client.go:159` | **No** | latent token leak; same flaw mirrored in star guard |
| keyring service key | V | hardcoded `gh:github.com` even on GHE | wrong keyring entry on enterprise | `client.go:97` | No | GHE auth gap |
| Token source precedence | V | `GITHUB_TOKEN` → gh `hosts.yml`+keyring → hub → "" | **`GH_TOKEN`/`gh auth token` NOT supported** | `client.go:83-126` | Partial | #75966a4, #1b38ad8 |
| Missing token | V | "" → help + `os.Exit(3)` | clean exit | `client.go:148-151` | Yes | #0bd12bc claims regression |
| Non-401 API error in `check()` | T | network/403/5xx/partial | **`panic(err)`** full trace | `client.go:855` | **No** | MustGit-style UX; mutations using `log.Fatal` (`:514,538,554,587,607`) bypass even the friendly 401 msg |

### 2.3 Checks / status rollup volatility

| Axis | Type | Range | Impact | Code | Hardened? | Bug |
|---|---|---|---|---|---|---|
| Default checks path | T | SUCCESS/PENDING/ERROR/EXPECTED/FAILURE | maps only SUCCESS→Pass, PENDING→Pending, else→Fail; trusts rollup → can read green while contexts EXPECTED/NEUTRAL/SKIPPED → **false green** | `client.go:304-314` | Partial | **#3d8cb88** |
| Checks not started yet | T | null rollup | treated as **Pass** (optimistic) | `client.go:305,762-765` | No | merge may fire before CI exists |
| Required-checks path | V | active only if `requiredChecks` set | separate query, mitigates false-green | `client.go:225,663-843` | Yes | only helps when configured |
| Required-checks query failure | T | network error | logs warning, **silently falls back** to rollup | `client.go:711-738` | Partial | invisible degradation |
| `contexts(first:100)` | V/T | >100 truncated | required check beyond 100 → Pending forever | `client.go:688` | No | no pagination |
| EXPECTED mapping inconsistent | T | →Fail (rollup) vs →Pending (context) | two paths disagree | `client.go:307-314` vs `:817-826` | No | — |

### 2.4 PR lifecycle & schema drift
PR body regenerated each update → clobbers UI edits (**#6550e72**; mitigated by `<!-- SPR-STACK-* -->`
anchors `template.go:184` + `preserveTitleAndBody`). Force-push then body-edit → duplicate
`synchronize` webhooks (**#ee01181**). `pullRequests(first:100)` not paginated → silent truncation
on large accounts. No retry/backoff anywhere. Invalid base in stack → `panic` (`client.go:365`).
Enum-as-string literals (`"SUCCESS"`,`"MERGEABLE"`,`"APPROVED"`) and hand-rolled union query
(`client.go:620-646`) mis-bucket silently if GitHub adds enum values (`fezzik_types.go`).

---

## 3. Configuration & auto-detection

`config/config_parser/ParseConfig` layers three structs via the vendored `rake` lib (**last writer
wins** per field). Repo: defaults → `git remote -v` source → `.spr.yml` → `git status -b` source.
User: defaults → `~/.spr.yml`. State: defaults → `~/.spr.state`, `RunCount++`, **rewritten every
run**. Env vars do **not** participate (only `GITHUB_TOKEN` is read, separately).

### 3.1 Config file loading & write-back

| Axis | Type | Range | Impact | Code | Hardened? | Bug |
|---|---|---|---|---|---|---|
| `.spr.yml` empty / comments-only | V | empty vs valid | rake `Decode` → `io.EOF` → `check(err)` **panics** (`panic: EOF`), file unnamed | rake `yaml_source.go:31` via `config_parser.go:22` | **No** | **#5f96ff8** |
| `.spr.yml` malformed | V | unparseable | same panic | rake `yaml_source.go:31` | **No** | same root |
| Repo config write-back | T | written once when absent | persists detected values; later runs read file (detection no longer authoritative) | `config_parser.go:55-58` | Yes | #81e27cb (verify only-when-missing) |
| `~/.spr.state` write-back | T | every run | `RunCount++`, `MergeCheckCommit` map persisted → drives merge-check skip | `config_parser.go:49-52` | Yes | run-to-run coupling |
| `GitHubRemote` precedence | V/T | from `status -b` runs **last**, unconditional | **silently overrides a user's `githubRemote` in `.spr.yml`** | `remote_branch.go:34` | **No** | precedence bug |

### 3.2 Remote / owner / repo / host detection

Regex `remote_source.go:48`: `(?P<githubHost>[a-z0-9._\-]+)(/|:)(?P<repoOwner>[\w-]+)/(?P<repoName>[\w-]+)`,
wrapped `^origin\s+(https://|ssh://)?(git@)?<host><sep><owner>/<repo>(.git)? \(push\)`.

| Axis | Type | Range | Impact | Code | Hardened? | Bug |
|---|---|---|---|---|---|---|
| Only `origin` push line parsed | V | hardcoded `^origin` | fork users with only `upstream` → "unable to auto configure" | `remote_source.go:51` | Partial | #8a59ec7 |
| Repo name with `.` | V | `[\w-]+` excludes `.` | `my.repo` fails | `remote_source.go:48` | **No** | **#431/#9f83901** |
| Owner with `-` | V | `[\w-]` includes `-` | parses ✅ (already works) | `remote_source.go:48` | Yes | **#333/#77435b1(b)** — verify/close |
| `git://` scheme | V | not in protocol alts | fails | `remote_source.go:43` | **No** | undocumented |
| Host with port | V | host group excludes `:` | fails | `remote_source.go:48` | **No** | self-hosted non-standard port |
| Uppercase host | V | `[a-z0-9._\-]+` | fails | `remote_source.go:48` | **No** | rare |
| Repo name ending `.git` | V | `(.git)?` suffix | strips ✅ | `remote_source.go:50` | Yes | interacts with `.`-in-name fix |
| version/help before init | V | runs inside `cli.App` after `ParseConfig`+`NewGitHubClient` | `--version`/`--help` fail outside a repo / without token | `cmd/spr/main.go:350-353` | **No** | **#333/#77435b1(a)** |

### 3.3 Behavior-switching knobs (selected)
`githubHost`/`githubRemote`/`githubBranch` (host/remote/target), `requireChecks`/`requiredChecks`/`requireApproval`
(merge gates), `mergeQueue`/`mergeMethod` (merge path + SHA-rewrite assumption), `createDraftPRs`,
`preserveTitleAndBody`, `branchPrefix` (branch derivation + match), `noFetch`/`noRebase`/`forceFetchTags`,
`branchPushIndividually` (atomic vs per-branch), `deleteMergedBranches`, `mergeCheck`, `prTemplateType`/`prTemplatePath`.
Full list + defaults: README config tables + `config/config.go:18-69`. Internal state: `stargazer`,
`runcount` (both volatile, persisted).

---

## 4. Stack model & PR lifecycle

`spr/spr.go` (`stackediff`) — the most stateful, race-prone subsystem. Identity rests on the
**commit-id** (stable across amends, unlike the git hash), reconciled across three representations:
the local commit body, the PR head commit body, and the `spr/<target>/<id>` branch name.

### 4.1 commit-id & branch derivation

| Axis | Type | Range | Impact | Code | Hardened? | Bug |
|---|---|---|---|---|---|---|
| commit-id generation | T | absent → 8-char UUID prefix on first update | lazy via re-invoking `spr_reword_helper` as rebase editor | `cmd/reword/main.go:97,104`; trigger `git/helpers.go:57-72` | Partial | no collision check; helper must be on PATH (panic if missing) |
| commit-id → PR matching | T | matched / new / orphan | match by `CommitID` equality; no match → create; PR w/o local → **close** ("commit gone away", loses review comments) | `spr/spr.go:362,306-313` | No | duplicated/reordered/cherry-picked id mis-matches |
| branch name format | V | `<prefix>/<target>/<id>` | changing `branchPrefix`/`githubBranch` **orphans all PR branches** → mass close+recreate | `git/helpers.go:27-31` | n/a | feature #ef06b2b |
| `_edit-sequence` keyed on `hash[:7]` | V | volatile hash, not id | two commits sharing 7-char prefix both get `edit` | `cmd/spr/main.go:60`, `spr/spr.go:149` | No | — |
| target-branch chain walk | T | linear to target | **panics** if a base branch doesn't match regex | `client.go:357-366` | No | #3dd3ee8, #094963e (PRs → `develop`) |

### 4.2 Stack alignment & corruption

| Axis | Type | Range | Impact | Code | Hardened? | Bug |
|---|---|---|---|---|---|---|
| `alignLocalCommits` | T | drops non-head commits GitHub attributes to a PR | merge-queue support-commit tolerance; can drop **base** commits → unstack/mis-target/runaway PRs | `spr/spr.go:261-279` (commit `5f9733c`) | **No** | **#094963e/#3dd3ee8/#32e7152/#fe61789**; no direct test |
| `commitsReordered` | T | positional id compare | reparent-all-to-target then re-stack; **no length guard** → index panic if a commit removed | `spr/spr.go:316-338,629-636` | **No** | **#3dd3ee8** |
| `update --count N` | V | bottom-N | create/update stops at N but align/reorder/close run over **full** stack | `spr/spr.go:388-390` | **No** | **#094963e** — corrupts stack; only `merge --count` tested |
| WIP commit | V/T | `Subject` starts `WIP` | WIP + everything above cut from loops | `git/helpers.go:129-131` | Partial | mid-stack WIP truncates; #7be98a8 |

### 4.3 Local↔remote divergence & merge

| Axis | Type | Range | Impact | Code | Hardened? | Bug |
|---|---|---|---|---|---|---|
| `SyncStack` cherry-pick | T | in-sync / ahead / divergent | `cherry-pick ..<hash>`; empty range → exit 128 instead of "in sync" | `spr/spr.go:553-556` | No | **#bbaebc8** |
| force-push branches | T | atomic vs individual | pushes changed branches; **no InQueue check** → GH006 on queued branch → panic | `spr/spr.go:696-733` | Partial | **#fe61789** |
| re-push unchanged PRs | T | add commit `d` → re-push `a/b/c` | needless CI | `spr/spr.go:696-713` | Partial | **#110c6bc** (fixed 0.9.3, regressed); re-confirm |
| top-only merge + close-below | T | merge top, **close** rest | folds stack; review comments lost; SHA-rewrite assumption only holds for rebase | `spr/spr.go:457-509` | No | **#1c4e8b0** (CODEOWNERS), **#26cbae9** (Jira), **#32e7152** |
| post-merge no re-fetch | T | local `Merged=true` only | shows MERGED before queue actually merges | `spr/spr.go:505-509` | No | #32e7152 |
| partial failure mid-stack | T | parallel goroutines (prod) | `log.Fatal` leaves half-updated stack, **no rollback/recovery** | `spr/spr.go:329-336`, `client.go:514` | **No** | theme of #094963e |

### 4.4 `spr edit` / worktree state
`spr_edit_state` + `REBASE_HEAD` probe joined at `<RootDir>/.git/...` (`spr/spr.go:91,182`) — `.git`
is a **file** in a worktree → write fails (**#519/#68254ac**). Re-invoked sequence editor via
`os.Executable()` (`spr.go:147-149`) — unquoted-spaces hazard on Windows (#80d06c3).

---

## 5. Platform, environment & runtime

**Windows is a shipped goreleaser target** (`git-spr`, `git-amend`, `spr_reword_helper`) yet there
are **zero `runtime.GOOS` branches in production code** — OS variance flows only through build-tagged
`terminal/` files and a single `/cygdrive` prefix sniff. The result: every rebase/amend/edit flow is
effectively broken on native Windows.

### 5.1 OS / platform

| Axis | Type | Range | Impact | Code | Hardened? | Bug |
|---|---|---|---|---|---|---|
| `/usr/bin/true` git editor | V | POSIX abs path | absent on zsh-builtin-only/minimal/**Windows** → rebase aborts | `realcmd.go:64` | **No** | **#362/#8e93b01** (fix: `/usr/bin/env true` — still POSIX-only; bare `true` more robust) |
| editor/helper path with spaces, unquoted | V | `C:\Program Files\…` | interpolated into git `-c core.editor=%s`/`sequence.editor=%s` and `_edit-sequence` cmd; git word-splits → `C:Program: command not found` | `realcmd.go:93,96`; `spr/spr.go:149` | **No** | **#80d06c3** (explicit Windows gap) |
| terminal width | V | unix ioctl / **windows: always error** | error → width 1000 (no truncation); byte-slice `line[:width-3]` can split a UTF-8 rune | `terminal/terminal_*.go`; `pullrequest.go:213-229` | Caller defends | mojibake on narrow non-ASCII |
| cygpath adjust | V | only `/cygdrive` prefix | `cygpath -w`; **panics** if cygpath missing | `realcmd.go:43-55` | No | untested branch |
| XDG ignored | V | hardcoded `~/.config/gh`, `~/.config/hub`, `~/.spr.yml` | custom `XDG_CONFIG_HOME` → tokens/config not found | `client.go:39,44,65,70`; `config_parser.go:85` | Returns err | — |

### 5.2 Environment variables
`EDITOR` (PR-body editor, default `vi` — Windows-unfriendly; `template.go:98`), `GITHUB_TOKEN` (no
`GH_TOKEN`), `SPR_DEBUG=1` (panic+trace vs clean exit — only in `spr` pkg `check`, `spr.go:740`;
`cmd/reword`/`cmd/amend`/`star.go` always panic — inconsistent), `SPR_NOREBASE`/`SPR_FETCH`/`SPR_NOFETCH`,
`PATH` (resolves git/helper/cygpath/editor), `LANG`/`LC_ALL`/proxy **not read explicitly** (proxy only
via Go stdlib for HTTP; locale never consulted), `HOME`/`os.UserHomeDir()`.

### 5.3 Terminal / TTY — **no TTY/CI detection anywhere** (no `isatty`/`term.IsTerminal`)
Star prompt (`star.go:23-59`, every 25 runs) blocks on stdin; in non-TTY/CI, EOF reads empty →
treated as "yes" → auto-stars or hangs; on GHE → panic (#9534d53). OSC-8 hyperlinks and zerolog
`ConsoleWriter` color emit unconditionally (raw escapes in pipes/CI logs).

### 5.4 Toolchain volatility
Go pinned 1.21 (mise/CI) but nix flake `go` floats with nixpkgs-unstable → flake builds may use a
different Go than CI. goreleaser pinned 2.15.4 locally but release CI uses `latest` (can diverge on
the `brews` deprecation). `flake.nix` `vendorHash` breaks `nix build` on any dep change until
re-hashed. Coverage ratchet (`.testcoverage.yml` total=90/file=70, `cmd/spr/main.go`=45) is brittle:
moving covered code can drop a file below floor with no behavior change. **CI test job runs only on
Linux** → cygpath/width/`/usr/bin/true`/path-with-spaces never exercised. CRLF (`core.autocrlf`)
unhandled in todo/commit parsing.

---

## 6. Test infrastructure & mock fidelity

Two fidelity tiers, and conflating them is the central risk:
- **Real-behavior tier (good):** `git/realgit/realcmd_test.go` and `internal/e2e/` shell out to real
  `git`; `github/githubclient/api_test.go` drives real `client.go` parsing via a fake transport.
- **Scripted-mock tier (low fidelity):** the bulk of `spr/` tests drive `git/mockgit` + `mockclient`
  with hand-authored strings/structs that diverge from reality.

### 6.1 Where mocks encode impossible inputs

| Concern | Tests assume | Real git/GitHub | Bug class it CANNOT catch | Code |
|---|---|---|---|---|
| Body indentation | single **tab** indent | `--format=medium` uses **4 spaces**; varies w/ config/locale | indentation/whitespace bugs (#194); the `\t` branch of `stripLogIndent` is **dead in prod** | `mockgit.go:228,230` vs `helpers.go:138-145` |
| Commit headers | fixed `Author: Eitan Joffe`, fixed date, 4 lines | arbitrary author/date, `Merge:`/GPG lines shifting offset | the `index+4` assumption (#213-class) never stressed | `mockgit.go:225-226` |
| Hashes | author-chosen strings | always 40 hex | abbrev/short-hash bugs (#213) | `mockgit.go:224` |
| Status | always clean | dirty/untracked/conflicted | dirty-tree/autostash-conflict paths | `mockgit.go:137-139` |
| Branch/remote | hardcoded `origin/master`, `spr/master/<id>` | config-driven | non-`master`/non-`origin`/custom-prefix bugs | `mockgit.go:91-92,146-149` |
| GraphQL responses | always non-null, always green, `ReviewApproved:true` | nullable nodes, null Repository, conflicts, pending rollups | **null-Repository crash (#388), false-green (#3d8cb88), host detection, body-clobber** — structurally unreachable from `spr/` tests | `mockclient.go:36-42,69-82` |
| Host/endpoint | none | GHE vs github.com endpoints | GHE endpoint construction untested | `client.go:159-174` |
| Errors | never returned; `RoundTrip` always 200 | 429/5xx/partial/timeout | retry/backoff/panic-on-transient paths | `mockclient.go`; `api_test.go:69` |
| Execution model | `synchronized=true` (serial) | **goroutines** in prod | race/partial-failure/order bugs (#094963e/#3dd3ee8/#ee01181) | `spr/spr.go:49,329-333` |

### 6.2 Change-detector tests & coverage false-confidence
`spr/spr_test.go` has ~73 `assert.Equal` checks against literal rendered strings — they assert *the
current value*, reproduce no defect, and pass on un-fixed code. Mock command assertions are the same
at the call-sequence level (CLAUDE.md's "reordering git calls requires updating mocks"). Line coverage
(90% gate) measures *which lines ran under single-valued happy paths*, **not how many input shapes** —
the nil-guard "covered" by one SUCCESS + one PENDING case is the clearest example. **e2e never reaches
GitHub** (every scenario exits before the network).

### 6.3 Recommendations
Capture real `git log --format=medium` into fixtures (4-space indent, 40-hex, varied headers);
add `api_test` fixtures for `Repository: nil`, a GHE host asserting the endpoint, rollup states beyond
SUCCESS/PENDING, conflicts, and a `RoundTrip` returning 429/5xx; add one real end-to-end `update`
against an in-process fake GitHub; parametrize parsing/branch/host tests over locale, git config, URL
forms, prefixes, platform paths; carry Title/Body/BaseRefName into mockclient expectations so
body-clobber becomes assertable; track a "variability axes exercised" checklist alongside the ratchet.

---

## 7. Cross-cutting root causes (highest-leverage hardening)

Most individual bugs are instances of one of these ~8 systemic patterns. Fixing the **class** beats
patching instances.

1. **`git log` human-format parsing is config/locale fragile.** Centrally force `LC_ALL=C`,
   `-c log.showSignature=false`, `--no-abbrev-commit` in `GitWithEditor`, and/or read `--format=%B`
   for bodies. Kills #213, #194, the showSignature/locale silent-corruption class at once.
2. **GraphQL nodes deref'd without nil guards.** One response-validation helper at every node deref
   (not just `GetInfo`). Covers #388 cluster incl. `GetAssignableUsers`, `addStar`.
3. **Panic-based error handling.** `MustGit` (`realcmd.go:67`) and `check`/`log.Fatal`
   (`client.go:855` etc.) panic on *expected* failures. Return errors at the boundary; reserve
   panic+top-level `recover` for genuine bugs; one consistent fatal path + exit code. Covers
   #25a84b6/#c279fc7, the #386/#388 "clean error" goals, partial-failure recovery.
4. **Naive host matching.** `strings.HasSuffix(host,"github.com")` (`client.go:159`, star guard)
   misfires on `github.mycorp.com`. Use exact `host == "github.com"` / dot-boundary. Fixes #386 for
   real + the silent token-leak.
5. **Unquoted, space-split args.** Command builders (`fmt.Sprintf`) → `strings.Split(argStr," ")`
   (`realcmd.go:98`) + editor config strings shatter any value with a space. Root of #80d06c3; latent
   for every config-derived path.
6. **POSIX/Windows assumptions.** Hardcoded `/usr/bin/true`, `vi` default, no path quoting, no
   `runtime.GOOS` paths, Linux-only CI. Windows is shipped but broken (#362, #80d06c3).
7. **commit-id ↔ PR ↔ branch-name reconciliation.** Identity is never validated for uniqueness/order
   and the authoritative `spr/<target>/<id>` branch-name id isn't used for recovery. Root of the
   merge-queue/alignment corruption cluster (#094963e/#3dd3ee8/#32e7152) — and there is **no recovery
   path** after corruption.
8. **Mock infidelity hides whole classes.** Tests pass against impossible inputs (tab indents,
   always-non-null GraphQL, serial execution). High line coverage ≠ trigger-space coverage.

---

## Appendix A — Bug → axis cross-reference

| Bug (issue / git-bug) | Primary axis | Section |
|---|---|---|
| #388/#364/#402/#511 (a987ee5…) GetInfo SIGSEGV | null GraphQL `Repository` | §2.1, §7.2 |
| #386 (9534d53) star panic on GHE | null node + naive host match | §2.1/§2.2, §7.4 |
| #512/#389 (25a84b6/c279fc7) MustGit panic | panic-based errors | §1.5, §7.3 |
| #362 (8e93b01) `/usr/bin/true` | POSIX/platform | §1.1, §5.1, §7.6 |
| #213 (1adabf2) abbrev hash | git-config-fragile parsing | §1.2, §7.1 |
| #431 (9f83901) repo name with `.` | remote regex | §3.2 |
| #333 (77435b1) version/help + hyphen owner | init order + regex | §3.2 |
| #519 (68254ac) worktree `.git` | repo layout | §4.4, §5.1 |
| #194 (780fdfd) body whitespace | medium-format parsing | §1.2, §6.1, §7.1 |
| #80d06c3 Windows path-with-spaces | unquoted args / platform | §5.1, §7.5 |
| #094963e/#3dd3ee8/#32e7152/#fe61789 stack corruption | alignment/reorder/merge-queue | §4.2/§4.3, §7.7 |
| #bbaebc8 sync-in-sync | local↔remote divergence | §4.3 |
| #3d8cb88 false-green checks | rollup volatility | §2.3 |
| #ee01181 duplicate webhooks | PR lifecycle | §2.4, §4.3 |
| #6550e72 body clobber | PR lifecycle | §2.4 |
| #5f96ff8 empty `.spr.yml` panic | config loading | §3.1 |

*Generated by 6 parallel code-mapping passes; see `.gitbug-runs/current/` for the per-fix reviews that
seeded this. Update file:line refs when code moves.*

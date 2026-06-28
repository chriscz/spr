# git-bug Triage Index — Open Bugs

> Working index for deciding which bugs to fix. **Not committed.** Generated 2026-06-28 from the
> native `./bin/git-bug` tracker (60 open issues), with discussions read in full and key claims
> verified against the current `master` code.

## How to read this

- **IDs** are git-bug IDs — inspect with `./bin/git-bug bug show <ID>`.
- This index covers **bugs only**. Feature requests / docs / support are listed compactly at the end.
- **Reproducibility**: `CONFIRMED-LIVE` (repro clear *and* the defect verified in current code) ·
  `REPRODUCIBLE` (clear/multi-reporter repro, code not re-verified) · `LIKELY` · `NEEDS-INFO` ·
  `LIKELY-FIXED` (refactor appears to have resolved it — verify before closing).
- Several bugs cluster on a shared root cause — see **Clusters** to fix many at once.

---

## Tier A — Reproducible AND confirmed live in current code (best ROI)

### Crash cluster: `GetInfo` nil-pointer dereference
`github/githubclient/client.go:205` and `:213` do `repoID = resp.Repository.Id` with **no nil guard**.
Whenever GitHub returns a null `Repository` (auth/token not resolved, repo not found, host/owner
mismatch, GHE quirks), spr panics with SIGSEGV instead of a clean error. This single unguarded
deref is reported as **four** separate bugs:

| ID | Title | Repro trigger | Notes |
|----|-------|---------------|-------|
| `a987ee5` | SIGSEGV in `GetInfo` | `git spr update` with no `~/.config/gh/hosts.yml` | CONFIRMED-LIVE |
| `e5a018c` | "Can't rebase onto latest" | `git spr update` targeting `upstream/dev`; framed as rebase failure but is the same crash | CONFIRMED-LIVE; dup of a987ee5 |
| `86ec445` | runtime error with new `githubHost` (GHE) | `git spr status` against GHE 3.9.10 (worked on 3.8.13); null `Repository` node | CONFIRMED-LIVE |
| `61a4b4a` | Segfault in all commands 0.17.6 (not 0.17.1) | upgrade to 0.17.6 on macOS, `gh`-CLI auth (no keychain) | LIKELY (regression); same crash site `client.go:213` |

**Fix once:** guard `resp.Repository == nil` in `GetInfo`, emit a clear "couldn't resolve repo / not
authenticated" error. Closes/dedupes all four.

### Crash cluster: `MustGit` panics on ordinary git failures
`git/realgit/realcmd.go` `MustGit` panics (full Go stack trace) on *any* non-zero git exit. Normal,
expected failures become ugly crashes:

| ID | Title | Repro trigger | State |
|----|-------|---------------|-------|
| `25a84b6` | Stack trace on every "normal" error | `git spr amend` with nothing staged → `panic: exit status 1` + goroutine dump | CONFIRMED-LIVE; 2 reporters |
| `c279fc7` | Panic if husky pre-push hook exits 1 | pre-push hook `exit 1`; `git spr update` push fails → panic | REPRODUCIBLE |
| `fe61789` | Tries to push when merging | merge-queue repo; `git spr merge` force-pushes a queued `spr/*` branch → GH006 reject → panic | REPRODUCIBLE (also a logic bug, see Tier B) |

**Fix once:** convert `MustGit` panics on expected failures into returned errors + a clean top-level
handler; gate stack traces behind `--verbose`/env. Improves all three (and the UX of every crash).

### `8e93b01` — `git amend` fails: cannot run `/usr/bin/true`
- **CONFIRMED-LIVE.** `git/realgit/realcmd.go:64` hardcodes `/usr/bin/true` as the rebase editor; on
  systems where `true` is only a shell builtin (zsh, minimal images) the file is absent and the
  internal `git rebase -i --autosquash` aborts.
- **Repro:** run `git amend` where `/usr/bin/true` doesn't exist. 4 reporters.
- **Fix (maintainer-endorsed):** use `/usr/bin/env true`. Labeled *good first issue*.

### `1adabf2` — "pull request stack is empty" with `abbrevCommit=true`
- **CONFIRMED-LIVE.** `git/helpers.go:80` regex `^commit ([a-f0-9]{40})` requires exactly 40 hex
  chars; with git config `log.abbrevCommit=true` (or `core.abbrev`) `git log` prints short hashes
  and nothing matches → spr thinks there are no commits.
- **Repro:** set `log.abbrevCommit=true`, run `git spr up`. 2 reporters.
- **Fix:** relax to `{7,40}` **or** pass `--no-abbrev-commit` to `git log` (latter also immunizes
  against `core.abbrev`).

### `9f83901` — Owner/repo detection breaks for repo names containing `.`
- **CONFIRMED-LIVE.** `config/config_parser/remote_source.go:48` `repoName` group is `[\w-]+`, which
  excludes `.` (GitHub allows `.`). → "unable to auto configure repository owner".
- **Repro:** use spr in a repo named e.g. `my.repo`.
- **Fix:** allow `.` in `repoName` **while** keeping the optional `.git` suffix match intact
  (the one gotcha the reporter flagged). Same family as upstream PR #393.

### `77435b1` — `--version`/`--help` fail outside a repo + hyphenated owner not detected
- **CONFIRMED-LIVE (two parts).** (a) `cmd/spr/main.go` runs config/git init before handling
  `--version`/`--help`, so they fail outside a git repo / without `.spr.yml`. (b) The owner regex
  (`remote_source.go:48`, `repoOwner` uses `\w`) rejects hyphenated owners.
- **Fix:** short-circuit version/help before init (same early-handling spot as `_edit-sequence`);
  widen `repoOwner` to allow `-`. The (b) half pairs with `9f83901` (same regex).

### `68254ac` — `spr edit` fails in git worktrees
- **CONFIRMED-LIVE.** `spr/spr.go:91` joins `RootDir()/.git/spr_edit_state`, but in a worktree
  `.git` is a *file* (gitdir pointer), so the write fails: `.git/spr_edit_state: not a directory`.
- **Repro:** `git spr edit` from inside a worktree. v0.17.6.
- **Fix:** resolve the real git dir via `git rev-parse --git-dir`/`--git-common-dir` instead of
  assuming `<root>/.git` is a directory; audit other hardcoded `.git` paths.

### `780fdfd` — Leading/trailing whitespace stripped from commit bodies
- **CONFIRMED-LIVE.** `github/template/template_custom/template.go:138` (`formatBody`, single-PR
  case) does `strings.TrimSpace(commit.Body)`. Indented body lines lose indentation in the PR.
- **Caveat:** `TrimSpace` only trims the body's leading/trailing whitespace — the mid-line
  indentation shown in the original screenshots may *also* be GitHub markdown rendering (4-space
  indent → code block). Verify which path actually drops the spaces before fixing.

### `9534d53` — Star prompt panics on GitHub Enterprise
- **CONFIRMED-LIVE.** `github/githubclient/star.go:98-105` `addStar` resolves `ejoffe/spr` and calls
  `check(err)`; `MaybeStar` (star.go:23) has **no host guard**. On a GHE host the upstream repo
  doesn't exist → panic. A 0.14.8 star fix did *not* cover the enterprise case.
- **Fix:** skip the star feature (or swallow the error) when host ≠ public github.com or the target
  repo can't be resolved. Recent `MaybeStar` prompt changes don't address the host case.

---

## Tier B — Reproducible per reports; code path complex / needs deeper work

### Merge-queue / stack-alignment fragility (shared root cause)
The merge-queue support change (`alignLocalCommits` in `spr/spr.go`, commit
`5f9733c506c5bb71f688acd092739bf93e78b07a`) plus the post-merge re-fetch/re-match path appears to
drive several stack-corruption bugs:

| ID | Title | Symptom | Repro quality |
|----|-------|---------|---------------|
| `094963e` | `update --count` drops base commits / makes PRs higher up | wrong PR targets (`develop`), unstacking, runaway PR creation; no recovery | REPRODUCIBLE — maintainer-acknowledged, repro repo `chriscz/git-spr-reproductions`, upstream #385. **High priority — corrupts the stack.** |
| `32e7152` | spr didn't close PRs after merge | `mergeQueue:true`+`mergeMethod:rebase`: only top PR merges (`MERGED ⚠️`), then "stack is empty", rest left open | LIKELY — one detailed trace; possibly token-scope related |
| `fe61789` | force-push into a queued branch | merge re-pushes `spr/*` branches already in the merge queue → GH006 reject → panic | REPRODUCIBLE (also in Tier-A MustGit cluster) |
| `3dd3ee8` | Reordering commits confuses spr | reorder via `rebase -i` → either "PR already exists" crash *or* silent PR close+recreate (loses review comments) | LIKELY — intermittent; worse on the merge-target branch / when skipping intermediate `update`s |

**Investigation lead:** audit commit-id ↔ PR matching in `UpdatePullRequests`/`alignLocalCommits`
and the post-merge re-fetch; deriving commit-id from the `spr/<target>/<id>` branch name was
suggested as a recovery mechanism.

### `110c6bc` — Adding a new commit re-pushes earlier (unchanged) PRs
- **REPRODUCIBLE** (regression-prone). Adding commit `d` makes `spr update` force-push `a/b/c`
  branches, firing needless CI. Bisected to upstream `e7d2462` (reword-helper for commit-ids),
  fixed by `6f93850` in v0.9.3, **then regressed again** between 0.9.3 and 0.12.2 (reporter #8,
  2024). Full bash repro script in-thread.
- **Action:** re-confirm on current `master`, add a regression test from the script.

### `1c4e8b0` — Merging a stack with mixed CODEOWNERS fails
- **REPRODUCIBLE.** spr merges only the top PR (after repointing its base to master); that PR's
  range now includes another team's code → GitHub blocks on missing CODEOWNER review.
- Surfaces at `github/githubclient/client.go:292`. Reporter posted screenshot evidence that no-ff
  merge commits do **not** rewrite SHAs — challenging the stated rationale for the top-only-merge
  hack. Maintainer floated `git spr merge --upto <pr>` (merge in reviewer batches) — most concrete.

### `ee01181` — `spr up` triggers duplicate `synchronize` webhooks
- **REPRODUCIBLE** (multiple reporters; still hit in 2024). The force-push immediately followed by
  `UpdatePullRequest` (refreshes only the PR-body stack section, *not functionally required*) makes
  GitHub emit two `synchronize` events → duplicate/cancelled CI; spr then watches the cancelled run
  and mis-reports checks. Partly inherent GitHub behavior (push + PR edit).
- **Fix options:** config flag to suppress the post-push body update; document recommending `on:
  push` CI triggers (confirmed workaround).

### `3d8cb88` — PR shown ✅ while some checks haven't run
- **LIKELY-PARTIALLY-FIXED.** Default checks path (`client.go` ~304-312) maps only
  SUCCESS/PENDING/FAILURE; GitHub's rollup can read SUCCESS while individual contexts are
  EXPECTED/NEUTRAL/SKIPPED → false green (dangerous for merge decisions). A newer
  `requiredChecks`/`computeRequiredCheckStatus` path (~786-842) mitigates this **only when
  `requiredChecks` is configured**. Verify the default path still trusts the rollup.

### `bbaebc8` — `spr sync` errors when already in sync
- **REPRODUCIBLE** (2 reporters). Sync issues `git cherry-pick ..<sha>`; with nothing to pick git
  errors `empty commit set passed` / exit 128 instead of "in sync". Also fires when the remote has
  new commits in a stacked PR, so the guard must cover the "remote ahead" case too.
- **Fix:** guard the empty/in-sync case in `SyncStack` before cherry-picking.

### `6550e72` — PR title/body overwritten on update/merge
- **REPRODUCIBLE** (known upstream #236). Editing a PR body in the GitHub UI gets clobbered because
  spr regenerates the whole body on `update`.
- **Agreed fix in-thread:** wrap spr-managed content in HTML-comment sentinels and only replace
  between them. Coordinate with `f870488` (strip-stack-before-merge direction). Note: anchor-based
  insertion (`insertBodyIntoPRTemplate`, `template.go:184`) already exists — may be partly addressed.

### `5f96ff8` — Panic on empty (or comments-only) `.spr.yml`
- **REPRODUCIBLE.** Empty `.spr.yml` → `panic: EOF` from the vendored `rake` YAML loader
  (`rake@v0.2.7/yaml_source.go:31`), via `config/config_parser/config_parser.go`. Error also
  doesn't name the file.
- **Fix:** guard empty/whitespace-only config in `ParseConfig` before handing to rake; name the file.

---

## Tier C — Bug-ish but needs reporter info / environment-specific

| ID | Title | Why uncertain |
|----|-------|---------------|
| `0bd12bc` | panic "invalid memory address" with no `GITHUB_TOKEN` | Real regression (0.8.5 printed a friendly warning), but conflates two crashes: the missing-token panic (still plausible via the `GetInfo` cluster) and a now-refactored WIP-commit panic. Re-verify the missing-token path. |
| `26cbae9` | SPR + Jira integration | Intermittent/integration-specific; merge "roll-up" closes (not merges) intermediate PRs so Jira transitions never fire. Needs Jira to reproduce; overlaps `ec26587`. |
| `80d06c3` | Windows install + path-with-spaces | Real native-Windows bug (reword-helper editor path under `C:\Program Files` → `C:Program: command not found`), but Windows-specific; WSL works. Also a docs gap. |

## Tier D — Likely already fixed by refactor (verify, then close)

| ID | Title | Reason |
|----|-------|--------|
| `7be98a8` | Segfault in `update` with a top `WIP` commit | Old crash was in `formatStackMarkdown`/`formatBody`; that code moved to `github/template/template_custom/template.go` with `if pr != nil` guards. Likely fixed — reproduce a trailing-WIP stack to confirm. |
| `81e27cb` | `.spr.yml` overwritten every run | Current `config_parser.go:55-57` writes repo config **only** when it doesn't exist (`os.ErrNotExist`); the per-run rewrite path is gone. Likely fixed (was a 0.14.x rake-marshal issue). |
| `e47e0d8` | Default Reviewers (feature) | PR #433 reportedly merged; just needs close confirmation. |

---

## Excluded from bug work (feature / docs / support — for completeness)

- **Config auto-detection trio** (`config/config_parser/`, all by yogurtearl, chain on default-remote
  detection): `8a59ec7` (githubRemote), `83ffef3` (default branch), `8de929f` (githubHost).
- **Reviewers:** `44e2a18` (handle non-assignable reviewers gracefully — note: current half-applied
  failure *is* a real robustness bug worth fixing), `2709fad` (update should add reviewers to all
  PRs), `f17592e` (assign reviewers — effectively answered via `gh pr edit`).
- **Merge / auto-merge:** `47756a7` (enable auto-merge), `485cd99` (checks-gated manual-merge block),
  `ec26587` (merge bottom-to-top so all PRs merge), `09b5123` (PR number in commit message on merge).
- **Branch naming:** `ef06b2b` (customizable branch names — *highest-demand feature*, 8 participants,
  fork reference `3b7152c` in davinkevin/spr.go), `c0c3f82` (prefix with local branch name; `palves`
  has a `sprBranch` config prototype).
- **Auth/token:** `75966a4` (use `gh auth token`/`GH_TOKEN` — high-impact, 5 reporters), `1b38ad8`
  (read token from OS keyring), `383e845` (gh CLI extension). All overlap the `GetInfo` crash cluster.
- **PR body / display:** `d6823b2` (show review status when `requireApproval:false`), `7e6a2ee`
  (Sapling-style "you are here" arrow), `cd2362a` (retain merged PR links), `f870488` (disable/strip
  stack info in body — maintainer prefers strip-before-merge), `b6321a0` (correct the manual-merge
  warning text).
- **Misc features:** `ae03228` (`git rebase --update-refs`), `9153f72` (item 3 `noRebase` already
  done; items 1–2 WONTFIX), `266923c` & `ded27d4` (`--no-verify` on push / amend — maintainer welcomes
  PR), `b74b43b` (`git spr amend` + alias precedence), `313c487` (`createDraftPRs` → RepoConfig; also
  the underlying draft-PR API error panics), `c7d7c6a` (PR labels/milestone), `eac40ac` (list local
  branch / `git spr list`), `ae55173` (GitLab support — large in-progress `forge`-package refactor).
- **Docs:** `dc86dc7` (document branch-protection / "Dismiss stale approvals" settings).

---

## Recommended order of attack

1. **`GetInfo` nil-guard** — fixes the 4-bug crash cluster (`a987ee5`/`e5a018c`/`86ec445`/`61a4b4a`).
2. **`MustGit` → graceful errors** — fixes `25a84b6`/`c279fc7` and improves every crash's UX.
3. **Quick mechanical wins:** `8e93b01` (`/usr/bin/env true`), `1adabf2` (regex `{40}`),
   `9f83901`+`77435b1` (owner/repo regex), `68254ac` (worktree `.git`), `9534d53` (star host guard).
4. **`094963e` + merge-queue alignment** — high value, harder; deep audit of `alignLocalCommits`.
5. Tier-B reproducibles as capacity allows.

---
description: Fix a bug from the git-bug tracker (or a GitHub issue) — regression-first, single Fixes #xxx commit
argument-hint: <git-bug-short-id | #issue-number> [report=<path>] [extra context]
allowed-tools: Bash, Read, Edit, Write, Grep, Glob, Agent, Skill, TaskCreate, TaskUpdate
---

You are the orchestrator for fixing a single bug end-to-end. The target is: **$ARGUMENTS**

Follow the project `CLAUDE.md` conventions. Work on a branch, not directly on `master`. The
non-negotiables for this workflow:

- Commits reference the **GitHub issue number** (`#xxx`), never the git-bug id. No git-bug short id,
  no git-bug ref, and not the word "git-bug" anywhere in the commit subject or body.
- The regression test and the fix go in the **same single commit**.
- Regression first (red) → fix (green) → verify → commit. Don't guess-fix.

**Before doing anything else, invoke the `superpowers:systematic-debugging` skill and follow it
throughout this entire command** — it governs how you reproduce, isolate, and fix the bug. This is
mandatory, not just a fallback for when things go wrong.

## 0. Resolve the target

The argument is either a git-bug short id (e.g. `8e93b01`) or a GitHub issue number (e.g. `#362`).

- If it's already `#NNN` / a bare number, that is the GitHub issue number.
- If it's a git-bug short id, read the full discussion with `./bin/git-bug bug show <id>` AND resolve
  the **GitHub issue number** from the raw objects (the only reliable mapping — `git-bug bug show`
  does not print it):

  ```bash
  sid=<short-id>
  ref=$(git for-each-ref --format='%(refname)' | grep -i "bugs/$sid" | head -1)
  for c in $(git rev-list "$ref"); do git ls-tree -r "$c"; done 2>/dev/null \
    | awk '{print $3}' | sort -u \
    | while read b; do git cat-file -p "$b" 2>/dev/null \
        | grep -o 'github\.com/ejoffe/spr/issues/[0-9]\+'; done \
    | grep -o '[0-9]\+$' | sort -un | head -1
  ```

Record: the git-bug short id (for your own reference only — it must NOT appear in the commit), the
GitHub issue number `xxx`, the bug title, and the discussion's stated root cause / repro steps.
If `docs/bug-triage-index.md` exists, read its entry for this bug first — it has verified code
locations and a fix sketch.

## 1. Investigate (subagents welcome)

Confirm the bug and pin the root cause in the *current* code before writing anything. For non-trivial
bugs, dispatch one or more `Explore`/`general-purpose` subagents to investigate in parallel — scope
each to a subtree (e.g. `spr/`, `github/githubclient/`, `git/`, `config/`). If parallel edits or
build isolation are needed, run a subagent with `isolation: "worktree"` so it works on its own copy.
Each subagent should return: the exact file:line of the defect, why it triggers, and a concrete repro.

Apply `superpowers:systematic-debugging` (already loaded) to reproduce and isolate the defect.
Do not guess-fix. If it remains unreproducible, report failure (see §5).

## 1.5. Interrogate the bug's full scope — BEFORE writing any test

The most common failure mode of a "fix" is scoping it to the single reported reproduction and missing
the rest of the bug's trigger-space. **Before you write the regression test or touch the fix**, answer
the questions below in writing (they go in the report, §5). Don't hand-wave — back each answer with a
`grep`/read and a file:line.

**First, consult the volatility/variability report if one exists.** Look for
`docs/*volatility*variability*.md` (generate one with the `x-report-volatility` command if missing and
the bug is non-trivial). It catalogues, per subsystem, every axis along which this system's behaviour
changes — use the section(s) covering the buggy code as your concrete checklist for question 1, and its
"cross-cutting root causes" section for question 3.

1. **Other scenarios that trigger it (Variability & Volatility).** Under what *other* configs, inputs,
   hosts/endpoints, environments/locales, platforms, repo/workspace layouts, or run-time/external
   states could this same defect — or a sibling of it — fire? Walk the relevant V/V-report section (or,
   if none, reason from first principles about each of those dimensions) and say yes/no/maybe + evidence
   for each that could plausibly apply. Every scenario that *can* trigger the bug is **in scope** — the
   test and fix must cover it.
2. **Sibling call sites (same defect pattern elsewhere).** Grep for the *pattern*, not just this line
   (e.g. the same unguarded deref, hardcoded assumption, panic-on-expected-failure, or brittle parse
   elsewhere). List every hit `file:line` and decide: fix now (same root cause) or record an explicit
   out-of-scope follow-up with the reason.
3. **Instance vs class.** Is there a *single upstream* change that would kill the whole family at once
   rather than patching this one instance? If you deliberately choose the narrow point-fix, **state why**.
4. **Does the corrected path actually reach the user?** Confirm the fix's behaviour is what the user
   sees end-to-end (e.g. an error here isn't re-`panic`'d upstack; exit code / stderr / output gating is
   right; it composes with the top-level handler). A "clean error" that still dumps a stack trace is not fixed.
5. **Test-fidelity plan.** State how the regression test will be **red on current code for the bug's
   actual reason**, and confirm any mock/fixture output matches what the real system actually emits
   (mock infidelity produces tests that pass against impossible inputs). Avoid tautology/change-detector
   tests that assert a literal new value instead of exercising the defect.

Record the answers in the report. The scenarios you mark in-scope in (1) and the class decision in (3)
directly determine what the §2 tests must cover.

## 2. Regression first (red)

Invoke `superpowers:test-driven-development`. Write a test that surfaces the bug and **fails for the
right reason** on current code — run it and capture the red output (a test that would also pass on the
unfixed code proves nothing). Cover the reported reproduction **plus every additional in-scope
scenario** identified in §1.5; for class-level fixes, test a representative case per trigger. Match
the existing test conventions (table tests; scripted `git/mockgit` + `github/mockclient` sequences —
see CLAUDE.md "Architecture"), and make sure the mock output is faithful to real git/API output.

## 3. Fix (green)

Make the smallest correct change. Re-run the regression (now green) and the surrounding package
tests. Then verify broadly:

```bash
go build ./... && go test -race ./...
make check   # if the toolchain is provisioned (lint + tests + coverage gate)
```

Follow `superpowers:verification-before-completion` — do not claim success without the passing
output in hand. If you touched platform-split files (`*_windows.go` / `*_other.go`,
`maybeAdjustPathPerPlatform`), keep both sides in sync.

## 4. Commit (single commit)

Stage the regression test + fix and commit them together. Stage explicitly — never `git add -A`
(see CLAUDE.md). Commit format:

```
Fixes #xxx: <concise summary of what was wrong and is now fixed>

<2-6 lines: the root cause / trigger, then the fix. Reference real files where useful.>
```

Hard rules:
- `xxx` is the **GitHub issue number**, not the git-bug id.
- **No** git-bug short id, no git-bug ref, and not the word "git-bug" anywhere in subject or body.
- Regression test and fix live in the **same** commit.

## 5. Write the report (always, uncommitted)

Every run — success or failure — writes a standalone Markdown report. Pick the path:

- If the invoker passed `report=<path>` in $ARGUMENTS, write exactly there.
- Otherwise default to `.gitbug-runs/current/fix-issue-<xxx>.md`. Ensure the run dir is local-only:

  ```bash
  mkdir -p .gitbug-runs/current
  grep -qxF '/.gitbug-runs/' .git/info/exclude 2>/dev/null || echo '/.gitbug-runs/' >> .git/info/exclude
  ```

The report contains: GitHub issue `#xxx` + title; outcome (`FIXED` / `FAILED` / `ALREADY-FIXED` /
`NOT-REPRODUCIBLE`); root cause (file:line); the **§1.5 scope interrogation** answers (other
configuration scenarios considered + which are in/out of scope, sibling call sites found, the
instance-vs-class decision and its justification); what the regression test asserts and how it is
red-for-the-right-reason; the fix; verification output summary (`go test -race`, `make check`); the
**fix commit id** if any; and — on failure — what was tried, the evidence, and what input is needed. Do NOT put any git-bug id or the word "git-bug" in
the *commit*; the report itself may reference the source for traceability.

## 6. Return a structured response

End your final message with a compact block the orchestrator can parse, e.g.:

```
RESULT
issue: #xxx
outcome: FIXED            # or FAILED / ALREADY-FIXED / NOT-REPRODUCIBLE
commit: <sha or ->        # git rev-parse HEAD of the fix commit, or - if none
branch: <branch or ->
report: <path to the report written in §5>
summary: <one line>
```

On failure, do NOT commit speculative changes — `outcome: FAILED`, `commit: -`, and let the report
carry the detail and where `superpowers:systematic-debugging` got stuck.

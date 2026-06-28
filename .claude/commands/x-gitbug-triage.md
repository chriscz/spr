---
description: Triage all open git-bug issues — read discussions, classify, verify, regenerate the bug index
argument-hint: [optional: subset of git-bug ids, or "all" (default)]
allowed-tools: Bash, Read, Grep, Glob, Agent, TaskCreate, TaskUpdate
---

Produce a thorough, decision-ready triage index of the open issues in the native **git-bug** tracker
(`./bin/git-bug`). Scope: **$ARGUMENTS** (default: all open bugs). Output goes to
`docs/bug-triage-index.md`. **Do not commit it.**

## 1. Enumerate

```bash
./bin/git-bug bug --status open -f plain
```

Note the label split too (helps spot mislabeled bugs):

```bash
for id in $(./bin/git-bug bug --status open -f id); do ./bin/git-bug bug label $id; done | sort | uniq -c | sort -rn
```

Real bugs are frequently mislabeled `enhancement` or unlabeled (segfaults, detection failures), so
do not trust labels alone — read everything.

## 2. Fan out (subagents)

Partition the open ids into batches of ~12 and dispatch one `general-purpose` subagent per batch in a
single message (parallel). Each subagent runs `./bin/git-bug bug show <id>` for every assigned id,
reads the **entire** discussion, and returns a structured entry per bug:

- **ID** + **title**
- **Classification**: `BUG` / `FEATURE` / `QUESTION-SUPPORT` / `DOCS`
- **Reproducibility** (bugs only): `REPRODUCIBLE` (clear/deterministic repro, multi-reporter, or
  obvious from code) · `LIKELY` · `NEEDS-INFO` · `NOT-REPRODUCIBLE` · `FIXED` (discussion says so)
- **Description**: 1-3 sentence crisp summary of the actual problem (not the title verbatim)
- **Repro steps / trigger**
- **Root cause / code area**: any file/function/commit named in the discussion
- **Status notes**: fix/PR/workaround mentioned, # of affected users, recency
- **Discussion highlights**: what a maintainer must know

Subagents must NOT modify any bug (no edits/comments). They return raw structured text only.

## 3. Verify the high-value claims against current code (orchestrator)

Do not take the discussions at face value — several bugs have been partially fixed by refactors.
Yourself check the most important "needs verification" claims with `Grep`/`Read` against current
`master` (e.g. is the cited file:line still present? did the function move?). Downgrade to
`LIKELY-FIXED` anything the code shows is already resolved, and upgrade to `CONFIRMED-LIVE` anything
you can see is still broken. Cite the file:line you verified.

## 4. Cluster and write the index

Group bugs that share a root cause (e.g. one unguarded nil-deref reported as several crashes; one
`MustGit` panic behind several "ugly crash" reports) so they can be fixed together. Write
`docs/bug-triage-index.md` with:

- A short header (date, source, methodology, "not committed").
- Tiers: **A** reproducible AND confirmed live in code · **B** reproducible but complex · **C**
  likely / needs-info · **D** likely already fixed (verify-then-close).
- For each bug: id, title, reproducibility, repro trigger, verified code location, fix sketch.
- A **Clusters** section (shared root causes), an **Excluded** section (features/docs/support, listed
  compactly), and a **Recommended order of attack**.

## 5. Finish

Print a summary: counts per classification/tier, the top cluster opportunities, and the path to the
written index. Remind that the index is uncommitted. Do not run any `git add`/`git commit`.

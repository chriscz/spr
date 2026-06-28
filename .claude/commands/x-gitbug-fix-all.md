---
description: Orchestrate triage + fixing of multiple git-bug issues end-to-end, producing one consolidated report
argument-hint: [bug ids / #issues / "tier A" / "all reproducible"] — leave empty to be guided by triage
allowed-tools: Bash, Read, Write, Agent, AskUserQuestion, TaskCreate, TaskUpdate
---

You are the **orchestrator**. The work itself is **wholly subagent-driven**: triage runs in a
subagent, every bug fix runs in its own subagent. You personally do ONLY orchestration — run-folder
management, the triage question, dispatching subagents, and compiling the final consolidated report.
You do NOT investigate, write tests, edit code, or commit yourself.

Requested scope: **$ARGUMENTS**

## 1. Set up the run folder (local-only, ignored)

Make `.gitbug-runs/` a git-ignored working area, archive any prior run, and open a fresh one:

```bash
mkdir -p .gitbug-runs/archive
grep -qxF '/.gitbug-runs/' .git/info/exclude 2>/dev/null || echo '/.gitbug-runs/' >> .git/info/exclude
# Archive a leftover previous run, if present.
if [ -d .gitbug-runs/current ]; then
  ts=$(date +%Y%m%d-%H%M%S)
  mv .gitbug-runs/current ".gitbug-runs/archive/$ts"
fi
mkdir -p .gitbug-runs/current
RUN_ID=$(date +%Y%m%d-%H%M%S); echo "$RUN_ID" > .gitbug-runs/current/RUN_ID
echo ".gitbug-runs/current = active run; .gitbug-runs/archive/<ts> = past runs"
```

Write a manifest `.gitbug-runs/current/run.md` recording: run id, requested scope, and (filled in as
you go) the triage decision, the target bug list, and per-bug status.

## 2. Triage decision (search, then ask)

Search for an existing triage report at `docs/bug-triage-index.md` (note its mtime/age). Then use
**AskUserQuestion** to let the user choose:

- **Use existing** triage report (only offer if one exists; show its age) — recommended if fresh.
- **Run fresh triage** now (dispatch a triage subagent).
- **Skip triage** — go straight to the bugs named in $ARGUMENTS.

If the user chooses to run triage, **dispatch ONE subagent** that performs the full triage procedure
in `.claude/commands/x-gitbug-triage.md` (have it Read that file and follow it), writing/refreshing
`docs/bug-triage-index.md`. Wait for it, then Read the index yourself to pick targets.

## 3. Select the target bugs

Resolve the target list from $ARGUMENTS and/or the triage index:

- Explicit ids/issues in $ARGUMENTS → use them.
- A selector like "tier A" / "all reproducible" / "confirmed-live" → take the matching bugs from the
  triage index.
- Nothing specified → default to the index's **Tier A (reproducible AND confirmed live)** and
  confirm the list with AskUserQuestion before proceeding.

Record the final list in `run.md`. Skip anything marked likely-already-fixed unless asked.

## 4. Dispatch one fix subagent per bug

For each target, dispatch a subagent (the fix work happens entirely inside it). Give each its own
report path under the run folder. Each subagent must Read `.claude/commands/x-gitbug-fix.md` and
follow that procedure exactly, including its regression-first + single `Fixes #xxx` commit rules.

Prompt template per subagent:

> Execute the bug-fix workflow defined in `.claude/commands/x-gitbug-fix.md` for target `<id|#issue>`.
> Write your report to `.gitbug-runs/current/fix-issue-<xxx>.md` (pass it as `report=<that path>`).
> Return the structured `RESULT` block (issue, outcome, commit, branch, report, summary).

Dispatch independent fixes concurrently. If fixes may touch the same files or you want isolated
builds/commits, run each subagent with `isolation: "worktree"`. Collect every subagent's `RESULT`
block; if one dies or returns nothing, mark it `FAILED` and keep going. Update `run.md` as results
arrive. (For a long list, you may dispatch in waves rather than all at once.)

**Do not integrate the fixes.** Each fix lands as a commit on its own branch (in its own worktree).
Leave them there — do NOT merge, cherry-pick, rebase, or push anything into the working branch.
Fixes are pulled in **only at the end, on explicit user request** (see §5).

## 5. Compile the consolidated report (the only non-subagent work you do)

Read each per-bug report from `.gitbug-runs/current/` plus the returned `RESULT` blocks and write
`.gitbug-runs/current/consolidated-report.md`:

- Header: run id, date, triage decision (reused vs fresh), target count.
- A results table: issue `#xxx` · title · outcome · commit sha · branch · report path.
- Sections: **Fixed** (with commit ids + one-line summaries), **Failed / blocked** (why, and what's
  needed), **Skipped / already-fixed**.
- Aggregate verification status and any follow-ups.

Then print a short summary to the user: counts (fixed / failed / skipped), the list of fix commit
ids + their branches, and the path to the consolidated report. Note that everything under
`.gitbug-runs/` is uncommitted and local-only. Do not run `git add` / `git commit` for the run
folder or reports — the only commits are the per-bug fix commits made inside the subagents.

**Pull-in is the user's call.** The fixes stay on their own branches/worktrees, unintegrated. End by
telling the user the fixes are ready and listing how to pull them in (e.g. `git cherry-pick <sha>` or
`git merge <branch>` per fix), and **wait for their explicit request** before integrating anything.
Only if/when they ask do you merge/cherry-pick the selected fixes into the working branch.

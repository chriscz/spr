---
description: Generate a volatility & variability report for a codebase — fan out subagents to map every axis along which the system's behavior changes
argument-hint: [optional: target path · focus area · explicit subsystem/dimension list] — leave empty to auto-discover
allowed-tools: Bash, Read, Write, Grep, Glob, Agent, AskUserQuestion, TaskCreate, TaskUpdate
---

You are the **orchestrator**. The investigation is **wholly subagent-driven**: each subsystem/dimension
is mapped by its own read-only subagent. You personally do ONLY orchestration — scoping, dispatching,
the completeness check, and synthesising the final report. You do NOT investigate or edit code
yourself, and the mapping subagents are **read-only** (no edits, no commits).

Scope / focus: **$ARGUMENTS** (leave empty to auto-discover the whole repo).

## Goal & definitions

Produce one authoritative document mapping every axis along which the system's behaviour changes,
split into:

- **Variability (V)** — behaviour varies by **environment or configuration** fixed for a given run:
  OS/arch, config files, env vars, locale, hosts/endpoints, input formats, feature flags, deployment.
  *"Works on my machine" lives here.*
- **Volatility (T)** — behaviour changes **over time, across runs, or via external state**: external
  API responses & timing, concurrency/races, persisted state mutating, retries, partial failures,
  clock/ordering, dependency/toolchain drift. *"Worked yesterday" lives here.*

The report is meant to be reused as an **interrogation catalogue**: before fixing a bug or writing a
test, a reader walks the relevant axes to find the *other* configs/hosts/platforms/states that trigger
the same defect. Most bugs are "the code assumed one value along axis X."

## 1. Scope the fan-out units

Resolve the list of **mapping units** (each becomes one subagent). Two kinds, combined:

**(a) Subsystems — auto-discovered (unless $ARGUMENTS names them).** Get the lay of the land, then
group the codebase into ~4–8 cohesive subsystems (by directory, layer, or responsibility):

```bash
# language/build signals + top-level structure
ls -la; git ls-files | sed 's#/.*##' | sort | uniq -c | sort -rn | head -40
# entrypoints, interfaces, external boundaries
git grep -lE 'func main|http\.|exec\.Command|os\.Getenv|Open\(|graphql|sql\.' | head -50
```

If $ARGUMENTS gives a path, focus area, or explicit unit list, honour it (narrow scope = fewer units).
For a large repo, dispatch a single `Explore` subagent to propose the subsystem decomposition rather
than eyeballing it yourself.

**(b) Cross-cutting dimensions — include each only if it applies to this codebase.** Judge
applicability from the scout; add a unit for each relevant one (skip the rest, and note which you
skipped and why):

- Configuration & precedence (files, layering, defaults, write-back)
- Environment & locale (env vars, `LANG`/`LC_ALL`, time zone, `$HOME`/XDG)
- Platform & runtime (OS/arch splits, paths, line endings, TTY/interactivity)
- External services / APIs (nullable responses, host/endpoint selection, auth, rate limits, schema drift)
- Concurrency, state & time (races, ordering, persisted state, clocks, idempotency)
- Persistence & I/O encoding (DB/files, serialization, encoding, CRLF, truncation)
- Versioning & toolchain (language/dep/tool version assumptions, build/release pinning, CI matrix)
- Security & auth surface (token sources, host trust, secret handling)
- Test infrastructure & mock fidelity (where the test model diverges from reality)

Record the final unit list. If you auto-discovered, **confirm it with AskUserQuestion** before fanning
out (offer the proposed list; let the user trim/add). Skip the question if $ARGUMENTS already pinned scope.

## 2. Set up a local-only working area

```bash
mkdir -p .volatility-runs/current
grep -qxF '/.volatility-runs/' .git/info/exclude 2>/dev/null || echo '/.volatility-runs/' >> .git/info/exclude
```

## 3. Dispatch one mapping subagent per unit (concurrently)

Each subagent is **read-only** and returns one markdown section. Use this prompt template (fill in
`<UNIT>` and a focused list of what to cover for that unit):

> READ-ONLY task. Do NOT modify, stage, or commit any files. You are mapping ONE unit of the system
> `<repo/system name>` (root: `<abs path>`; read any `README`/`CLAUDE.md`/`AGENTS.md` for architecture).
>
> UNIT: **<UNIT — subsystem or cross-cutting dimension>**. Files/area: `<paths/globs>`.
>
> Produce an exhaustive, code-grounded catalogue of every axis of **Variability (V — varies by
> environment/config)** and **Volatility (T — changes over time / across runs / via external state)**
> for this unit. For each axis give concrete `file:line` evidence and say whether the code defends
> against it. Cover at least: <unit-specific checklist — e.g. for an API unit: nullable responses,
> host/endpoint selection, auth sources, rate limits/retries, pagination, timing/eventual-consistency,
> schema drift, error handling; for a parser unit: every format/locale/encoding assumption; for a
> config unit: layering/precedence/defaults/malformed-input>.
>
> Also flag, separately, any **LIKELY BUG / TEST GAP** you find (with evidence) vs. "probably fine".
>
> OUTPUT — return a markdown section titled `## <UNIT>` containing one or more tables with columns:
> **Axis | Type (V/T) | Range of values | Behavioral impact | Code (file:line) | Hardened? | Known
> bug / test gap**, plus short prose where a cell is insufficient. Cite real file:line for everything.
> Return the section in your final message (write no file).

Dispatch all units concurrently. If a subagent dies or returns nothing, mark that unit `INCOMPLETE`
and continue. Save each returned section to `.volatility-runs/current/unit-<slug>.md` as it arrives.
(For a large unit list, dispatch in waves.)

## 4. Completeness pass (one critic subagent)

Dispatch one more read-only subagent: give it the list of units covered and ask **"what axis,
dimension, boundary, or input shape is missing or under-covered?"** — e.g. an external boundary not
mapped, a config knob with no entry, a platform/locale/encoding axis skipped. If it surfaces real
gaps, dispatch a small second wave to fill them. (Scale this to the request: skip for a quick/narrow
run; do 1–2 rounds for "thorough"/"comprehensive".)

## 5. Synthesise the report (the only non-subagent writing you do)

Assemble all sections into one document. Default output path:
`docs/<repo-or-scope-slug>-volatility-and-variability.md`. Include:

- **Definitions** (V vs T) and a short **How to use this document** (the interrogation-catalogue framing).
- A **Master axis index** table: unit → dominant hazard.
- The per-unit sections (lightly normalised to the shared table columns).
- A **Cross-cutting root causes** section: the systemic patterns that recur across ≥3 units (where one
  class-level fix beats N instance patches). This is the highest-value section — derive it yourself
  from the unit findings.
- A **Test fidelity** section: where the test/mocks structurally cannot reach an axis.
- **Appendix: issue/bug → axis cross-reference** (if a tracker/triage index exists, map known issues
  to sections; otherwise list the LIKELY BUGs the subagents found).
- A footer noting it was generated by parallel mapping passes and that `file:line` refs drift as code moves.

## 6. Report to the user

Print: the unit list (with any INCOMPLETE), the output path, a 3–5 line digest of the biggest
cross-cutting hazards, and the top LIKELY BUGs found. Note that the doc and `.volatility-runs/` are
**uncommitted**; do not `git add`/`commit` unless the user asks. Committing the report is the user's call.

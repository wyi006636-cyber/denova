# Phase 0 Gate Split Route Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update every current planning source so Phase 1 engineering may proceed without treating missing P0 human reviews as a project-wide block, while preserving the factual quality gap, H v1 no-promotion decision, and unresolved dependency debt.

**Architecture:** This is one atomic documentation work package. The Phase 0 baseline report and decision register state the decision and evidence boundary; the master plan, detailed plan, validation gates, and traceability matrix consume the same decision. No runtime, API, schema, dependency, evaluation record, or private artifact changes.

**Tech Stack:** Markdown, JSON existence checks, Git, PowerShell, ripgrep

## Global Constraints

- Work only on `feat/quality-harness-foundation`; do not push, merge, rebase, tag, or publish.
- Modify only the six listed route documents plus `CHANGELOG.md`; do not change Go, TypeScript, JSON contracts, dependencies, or private evaluation evidence.
- Phase 1 P1-T01 through P1-T07 must be explicitly allowed to proceed under their own task gates.
- `p0-offline-harness-v1` remains evaluation-only evidence and must not be promoted into the product or enabled by default.
- Do not create `quality-gate-v1.json` or invent a win rate, sample size, non-inferiority tolerance, cost tolerance, fact-error tolerance, or author-edit tolerance.
- Preserve the historical facts: 0/24 formal human reviews, no adjudications, and no valid quality conclusion.
- Preserve the unresolved engineering facts: `go mod tidy -diff` classification failure and reachable `GO-2026-5970`; neither may be written as PASS.
- Do not commit private paths, prose, identities, credentials, or raw model-panel output. A safe aggregate may say only that the panel was non-human, position-sensitive, and insufficient for a quality claim.
- Update `CHANGELOG.md` before the implementation commit. Commit messages must be English.
- This documentation-only work runs consistency checks and `git diff --check`; it does not rerun or claim Go/web runtime gates.

---

## File map

- Modify: `docs/project-design/implementation/evaluation/PHASE_0_BASELINE_REPORT.md` — factual evidence, new entry decision, remaining release/quality limitations.
- Modify: `docs/project-design/implementation/planning/RISK_AND_DECISION_REGISTER.md` — frozen gate-split decision and new anti-blocking risk.
- Modify: `docs/project-design/implementation/planning/MASTER_DEVELOPMENT_PLAN.md` — current status, Phase 0 exit semantics, P1-T01 dependency, later quality boundary.
- Modify: `docs/project-design/implementation/planning/PHASE_0_DETAILED_PLAN.md` — P0-T09 closeout semantics, expected outputs, exit checklist, Phase 1 handoff.
- Modify: `docs/project-design/implementation/planning/VALIDATION_AND_RELEASE_GATES.md` — Phase 1 entry separated from G4/G5 quality and release claims.
- Modify: `docs/project-design/implementation/planning/REQUIREMENTS_TRACEABILITY_MATRIX.md` — current execution status and deferred quality-evidence ownership.
- Modify: `CHANGELOG.md` — bilingual record of the applied route change.

### Task 1: Apply the approved gate split across all route documents

**Files:**
- Modify: `docs/project-design/implementation/evaluation/PHASE_0_BASELINE_REPORT.md:3-17,28-39,57-74`
- Modify: `docs/project-design/implementation/planning/RISK_AND_DECISION_REGISTER.md:3-20,40-47,65-85`
- Modify: `docs/project-design/implementation/planning/MASTER_DEVELOPMENT_PLAN.md:3-9,87-135,219-227`
- Modify: `docs/project-design/implementation/planning/PHASE_0_DETAILED_PLAN.md:5-13,674-772`
- Modify: `docs/project-design/implementation/planning/VALIDATION_AND_RELEASE_GATES.md:7-34,111-144,287-334`
- Modify: `docs/project-design/implementation/planning/REQUIREMENTS_TRACEABILITY_MATRIX.md:1-3,23-60,79-96`
- Modify: `CHANGELOG.md:4-12`

**Interfaces:**
- Consumes: approved decision in `docs/superpowers/specs/2026-07-23-phase-zero-gate-split-route-design.md`.
- Produces: one consistent route in which Phase 1 engineering is allowed, H v1 is not promoted, quality evidence is inconclusive, and dependency debt remains release-blocking.

- [ ] **Step 1: Verify the exact starting state**

Run:

```powershell
git branch --show-current
git status --short
git log -1 --oneline
Test-Path 'docs/project-design/implementation/evaluation/quality-gate-v1.json'
```

Expected:

```text
feat/quality-harness-foundation
<no status output>
d20c776 docs: approve Phase 0 gate split
False
```

Stop if the worktree is not clean, the branch differs, or `quality-gate-v1.json` exists.

- [ ] **Step 2: Amend the Phase 0 baseline without rewriting historical evidence**

In `PHASE_0_BASELINE_REPORT.md`, replace the single overall `NOT-ENOUGH-DATA / BLOCKED` route with four explicit facts:

```markdown
> Phase 1 engineering / Phase 1 工程开发：may begin / 可以开始
>
> Quality conclusion / 质量结论：`INCONCLUSIVE`
>
> H v1 product use / H v1 产品使用：not promoted / 不推广
>
> Release readiness / 发布就绪：`NOT-READY`
```

Keep the existing 0/24 review, absent adjudication, absent `quality-gate-v1.json`, tidy failure, and `GO-2026-5970` rows unchanged as facts. Rewrite the conclusion so it states that missing human evidence prevents quality claims but no longer prevents Phase 1 engineering.

Rename “Exact unblock sequence” to “Remaining work before quality or release claims”. Replace its fixed 24-review-first sequence with this ordered boundary:

1. build and validate Phase 1 under its own engineering gates;
2. do not productize H v1 or invent thresholds;
3. design new quality evidence after a real writing vertical slice exists;
4. obtain separate authorization for the `go.mod` classification correction and vulnerable dependency upgrade;
5. rerun affected Windows/Linux engineering and security gates before release readiness.

Add one diagnostic-only sentence: the private model panel is non-human and position-sensitive, so it informed the no-promotion decision but did not create quality evidence.

- [ ] **Step 3: Freeze the route decision and risks**

In `RISK_AND_DECISION_REGISTER.md`:

- replace the top current-status paragraph with the four baseline facts from Step 2;
- add `FD-011` after `FD-010` with the decision “Phase 1 engineering may proceed without a P0 human-review quota; H v1 is not promoted and quality/release claims remain evidence-gated”;
- keep `ADR-EVAL-001` Proposed, but change its deadline from “P0-T09 完成前” to “首次 Harness 质量声明前”;
- add `QUAL-010` describing the risk that evaluation work becomes the project goal, with detection “evaluation activity grows while no real author workflow ships” and mitigation “quality evidence follows a real vertical slice; diagnostic evaluation cannot globally block unrelated engineering”.

Do not change accepted ADR decisions or existing risk evidence.

- [ ] **Step 4: Update the master and detailed phase plans**

In `MASTER_DEVELOPMENT_PLAN.md`:

- replace the current global Phase 0/Phase 1 block with the four baseline facts;
- revise the Phase 0 goal and deliverables so missing `quality-gate-v1.json` is a recorded quality gap, not a Phase 1 entry condition;
- remove “单人承担时不得省略人工评测” as a Phase 1 prerequisite;
- change the P1-T01 dependency from `P0-T04、P0-T09` to `P0-T04、FD-011`;
- preserve all other P1-T01–P1-T07 tasks and their own exit conditions;
- revise the dependency diagram/text so G4 quality evidence gates claims and promotion, not the start of all Phase 1 engineering.

In `PHASE_0_DETAILED_PLAN.md`:

- keep the evaluation-only runner boundary unchanged;
- state that P0-T09 produced valid engineering/evaluation mechanics but no valid quality conclusion;
- mark `quality-gate-v1.json` as deferred rather than a required Phase 0 artifact;
- preserve the 0/24 evidence and the prohibition on model votes as human evidence;
- revise the exit checklist so Phase 1 may start with the quality gap recorded and the two engineering failures tracked separately;
- replace “Phase 0 通过后” with “本路线决策生效后” for the P1-T01/P1-T02 handoff.

- [ ] **Step 5: Separate Phase 1 entry from quality and release gates**

In `VALIDATION_AND_RELEASE_GATES.md`:

- replace the current paragraph saying Phase 1 must not start;
- retain G4 as the gate for claims that Harness improves quality;
- retain G5 as the release gate, including dependency/security debt;
- add a Phase 1 entry paragraph requiring the approved route decision, accepted foundational ADRs, a clean scoped task, and each P1 task's targeted/repository tests;
- state explicitly that an absent `quality-gate-v1.json` is valid during Phase 1 engineering but forbids threshold-driven acceptance or quality claims;
- change Section 6 threshold ownership from “must be written by P0-T09” to “must be derived before the first quality claim from a preregistered real-product evaluation”.

Do not weaken any test, privacy, cross-mode, migration, or release requirement.

- [ ] **Step 6: Update traceability without deleting quality requirements**

In `REQUIREMENTS_TRACEABILITY_MATRIX.md`:

- replace the top global-block status with the four baseline facts;
- retain EVAL-001 and EVAL-002 as quality/release requirements;
- clarify that P0-T09 owns diagnostic paired evidence and the no-promotion decision, while P2-T09/P3-T08/P5-T04 own future real-product quality proof;
- state that missing P0 human reviews no longer blocks P1-T01–P1-T07;
- do not change DATA, SAFE, MODE, AUTH, or release requirement priorities.

- [ ] **Step 7: Record the applied route change in the changelog**

Under the first `[Unreleased]` section's `### Added` or a new `### Changed` section, add matching Chinese and English entries that say:

```markdown
- 路线门禁现将 Phase 1 工程开发与 Harness 质量声明分离：P1-T01–P1-T07 可按各自门禁推进；H v1 不进入产品，`quality-gate-v1.json` 仍不创建，tidy 与 govulncheck 失败继续独立追踪并在发布前解决。
- Route gates now separate Phase 1 engineering from Harness quality claims: P1-T01–P1-T07 may proceed under their own gates; H v1 is not productized, `quality-gate-v1.json` remains absent, and the tidy and govulncheck failures stay independently tracked for resolution before release.
```

- [ ] **Step 8: Run contradiction and privacy scans**

Run:

```powershell
$routeFiles = @(
  'docs/project-design/implementation/evaluation/PHASE_0_BASELINE_REPORT.md',
  'docs/project-design/implementation/planning/RISK_AND_DECISION_REGISTER.md',
  'docs/project-design/implementation/planning/MASTER_DEVELOPMENT_PLAN.md',
  'docs/project-design/implementation/planning/PHASE_0_DETAILED_PLAN.md',
  'docs/project-design/implementation/planning/VALIDATION_AND_RELEASE_GATES.md',
  'docs/project-design/implementation/planning/REQUIREMENTS_TRACEABILITY_MATRIX.md'
)
rg -n 'Phase 0 与 Phase 1 均为.*BLOCKED|Phase 1 不得开始|Phase 1 remains blocked|Until this sequence completes, Phase 1 remains blocked' $routeFiles
rg -n 'Phase 1 engineering|Phase 1 工程|H v1|quality-gate-v1.json|GO-2026-5970|go mod tidy' $routeFiles
rg -n 'AppData\\Local\\Denova|reviewer_id|api_key|Authorization|BEGIN PRIVATE' $routeFiles CHANGELOG.md
Test-Path 'docs/project-design/implementation/evaluation/quality-gate-v1.json'
git diff --check
git status --short
```

Expected:

- stale-current-state search: no matches except text explicitly labelled historical/superseded;
- positive route search: all six documents contain the relevant boundary where applicable;
- privacy scan: no matches;
- `quality-gate-v1.json`: `False`;
- `git diff --check`: exit 0 with no output;
- status: only the six route documents and `CHANGELOG.md` are modified.

- [ ] **Step 9: Review the complete diff against the approved design**

Run:

```powershell
git diff --stat
git diff -- docs/project-design/implementation/evaluation/PHASE_0_BASELINE_REPORT.md docs/project-design/implementation/planning/RISK_AND_DECISION_REGISTER.md docs/project-design/implementation/planning/MASTER_DEVELOPMENT_PLAN.md docs/project-design/implementation/planning/PHASE_0_DETAILED_PLAN.md docs/project-design/implementation/planning/VALIDATION_AND_RELEASE_GATES.md docs/project-design/implementation/planning/REQUIREMENTS_TRACEABILITY_MATRIX.md CHANGELOG.md
```

Confirm all of the following before staging:

- Phase 1 P1-T01–P1-T07 are allowed;
- H v1 is not promoted;
- no quality PASS or threshold is invented;
- 0/24 human evidence remains factual;
- tidy and govulncheck failures remain visible;
- no unrelated plan scope or historical evidence is rewritten.

- [ ] **Step 10: Commit the atomic route update**

Run:

```powershell
git add CHANGELOG.md docs/project-design/implementation/evaluation/PHASE_0_BASELINE_REPORT.md docs/project-design/implementation/planning/RISK_AND_DECISION_REGISTER.md docs/project-design/implementation/planning/MASTER_DEVELOPMENT_PLAN.md docs/project-design/implementation/planning/PHASE_0_DETAILED_PLAN.md docs/project-design/implementation/planning/VALIDATION_AND_RELEASE_GATES.md docs/project-design/implementation/planning/REQUIREMENTS_TRACEABILITY_MATRIX.md
git diff --cached --check
git commit -m "docs: allow Phase 1 engineering"
git status --short --branch
```

Expected: commit succeeds, the worktree is clean, and the branch remains `feat/quality-harness-foundation` without a push.

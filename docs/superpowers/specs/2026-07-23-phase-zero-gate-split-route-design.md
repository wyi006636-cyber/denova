# Phase 0 Gate Split and Phase 1 Entry Design

> Status: Approved
> Date: 2026-07-23
> Scope: Planning and gate semantics only; no product runtime or dependency change

## 1. Decision

Phase 0 no longer uses one indivisible gate that makes missing human blind-review data block all subsequent engineering. The route is split by what the evidence can actually prove:

1. Phase 1 engineering is allowed to start and all P1-T01 through P1-T07 tasks remain in scope.
2. The evaluation-only `p0-offline-harness-v1` is retained as historical engineering evidence but is not promoted into the product.
3. Current evidence cannot support a claim that Harness improves novel quality, so no `quality-gate-v1.json`, win-rate threshold, sample-size rule, or non-inferiority tolerance is invented.
4. Quality claims and promotion of an H workflow require new evidence from real product writing after the relevant workflow exists; they do not block Phase 1 domain, storage, API, SSE, or UI engineering.

This replaces the prior global statement that “Phase 1 must not begin until 24 human reviews and a Gate Manifest exist.” It does not rewrite or erase the factual P0-T09 evaluation result.

## 2. Why the route changes

The old gate combined three different questions:

- whether the Phase 0 contracts, ADRs, regression tests, and isolated evaluation mechanics exist;
- whether the whole repository currently passes every engineering/release check;
- whether H v1 improves prose quality.

Those questions now have different answers. Contracts, ADRs, bounded cohorts, and most engineering gates have auditable evidence. Quality improvement remains inconclusive. Two engineering failures are also still real: `go mod tidy -diff` reports a direct/indirect classification change, and `govulncheck` reports reachable `GO-2026-5970`. Treating all three facts as one `BLOCKED` state makes a missing reviewer pool prevent unrelated Phase 1 work while obscuring the separate dependency debt.

The route therefore records each fact independently instead of converting any failure or missing evidence into PASS.

## 3. Resulting route states

The planning documents use plain statements instead of relying on a new state-machine vocabulary:

| Area | Route decision |
| --- | --- |
| Phase 1 engineering | May begin with P1-T01/P1-T02 and proceed through P1-T07 under their own task gates |
| H v1 product integration | Do not implement or enable by default |
| Quality-improvement claim | Not established; cannot be used for MVP, Beta, release, or marketing claims |
| `quality-gate-v1.json` | Remains absent until defensible real-product evidence exists |
| Human P0 review quota | No longer a prerequisite for Phase 1 entry |
| Existing P0 private evidence | Preserved; not imported or relabelled as human evidence |
| `go.mod` classification failure | Tracked as explicit engineering debt; requires separate authorization and verification |
| `GO-2026-5970` | Tracked as explicit security/dependency debt; must be resolved before a release gate can pass |

“Phase 1 may begin” is not equivalent to “Phase 0 quality passed” or “the repository is release-ready.”

## 4. Phase 1 dependency changes

### P1-T01 through P1-T07

All planned Phase 1 engineering tasks are allowed. Their existing functional dependencies remain, except that P1-T01 no longer depends on a completed human P0-T09 pilot or `quality-gate-v1.json`. It depends on the accepted Profile and QualitySpec decisions and on this route decision.

Phase 1 may implement:

- Profile and QualitySpec contracts and versioning;
- Workspace Schema adapters, migration previews, backups, and rollback;
- rebuildable SQLite/FTS projection after its dependency work is authorized;
- CandidateSet, ReviewIssue, and PreferenceMemory persistence contracts;
- Quality API and bounded SSE payloads;
- the bilingual project/planning UI skeleton;
- Phase 1 integration and cross-mode regression gates.

Phase 1 must not encode an invented quality threshold, silently treat H v1 as the preferred workflow, or claim that a schema/API/UI implementation improves prose.

### Later phases

Phase 2 must reassess the generation workflow against real writing needs instead of automatically productizing `p0-offline-harness-v1`. A single-draft workflow remains valid. Multi-candidate or review/revision stages must earn their complexity with evidence from actual author use.

MVP, Beta, and release quality claims still require explicit evidence. The evidence design may be revised after a real vertical slice exists; no fixed requirement for 24 P0 reviews is carried forward automatically.

## 5. Evidence handling

- The current Phase 0 baseline report remains a factual record and is amended rather than replaced.
- The 0/24 human-review count remains true historical evidence.
- The private three-model advisory panel remains outside Git. Planning documents may state only its safe aggregate lesson: it is non-human, position-sensitive, and insufficient for a quality claim.
- No model vote is imported through `quality-eval record-review`.
- `quality-gate-v1.json` is not created as part of this route change.
- Existing release-holdout isolation remains unchanged.

## 6. Planning documents to update

The implementation updates existing documents in place:

1. `MASTER_DEVELOPMENT_PLAN.md`: replace the global Phase 0/1 block, revise Phase 0 exit semantics, and remove the human-pilot dependency from P1-T01.
2. `PHASE_0_DETAILED_PLAN.md`: distinguish preserved P0-T09 quality evidence from Phase 1 entry and revise the exit checklist.
3. `VALIDATION_AND_RELEASE_GATES.md`: separate Phase 1 entry from G4 quality/release claims and preserve the two engineering failures.
4. `REQUIREMENTS_TRACEABILITY_MATRIX.md`: update current execution status and trace quality evidence to later real-product gates.
5. `RISK_AND_DECISION_REGISTER.md`: record the gate split and the risks of both false quality claims and permanent evaluation blockage.
6. `PHASE_0_BASELINE_REPORT.md`: add the approved route decision without rewriting the original measurements.
7. `CHANGELOG.md`: record the route change in Chinese and English.

No new root document, source package, dependency, API, UI, configuration field, or runtime behavior is introduced.

## 7. Consistency rules

After the route change:

- no current-status paragraph may say that missing 24 human reviews alone prevents Phase 1 from beginning;
- no paragraph may say Phase 0 quality passed;
- no paragraph may say all engineering/release gates passed while tidy and govulncheck remain unresolved;
- all documents must agree that H v1 is evaluation evidence only and is not promoted;
- Phase 1 task gates remain strict for the files and behavior each task actually changes;
- G4/G5 quality and release claims remain unavailable without later evidence and cleared engineering debt.

Historical descriptions of the former plan may remain when clearly identified as superseded history.

## 8. Verification

The documentation change is accepted when:

1. focused searches find no contradictory current-state claim across the six planning/evaluation documents;
2. searches confirm Phase 1 engineering is explicitly allowed and H v1 is explicitly not promoted;
3. `quality-gate-v1.json` remains absent;
4. no private path, prose, reviewer identity, credential, or raw model-panel output enters Git;
5. `git diff --check` passes;
6. the final worktree contains only the intended planning/spec/CHANGELOG changes before commit.

No Go or web test is required for the design-only commit. The later route-document implementation commit runs documentation consistency checks and `git diff --check`; it does not claim runtime verification.

## 9. Alternatives considered

### Allow only P1-T02/P1-T03

Rejected because Profile/QualitySpec contracts, storage, API, and UI engineering do not logically require a completed P0 human pilot. It would preserve most of the accidental project-wide block.

### Treat the model panel as a human-quality replacement and mark Phase 0 PASS

Rejected because the panel is non-human and strongly position-sensitive. It cannot support formal thresholds or a quality-improvement claim.

### Keep the original global block

Rejected because the unavailable reviewer pool would continue to stop unrelated engineering and shift project effort away from real novel creation.

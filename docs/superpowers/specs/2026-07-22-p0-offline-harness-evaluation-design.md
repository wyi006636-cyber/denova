# P0 Offline Harness Evaluation Design

> Status: Approved
> Date: 2026-07-22
> Scope: Evaluation-only Quality Harness runner for P0-T09

## 1. Decision

Phase 0 may implement a versioned, offline Quality Harness runner solely for controlled evaluation. The earlier blanket statement that Phase 0 cannot implement any Harness behavior is replaced by a precise boundary: Phase 0 still cannot add product runtime integration, user-facing Harness workflows, formal workspace writes, automatic publication, or third-party script execution.

The offline runner exists because P0-T09 requires real paired S/H evidence, while P0-T07 intentionally created only the ordinary single-turn S arm. Reclassifying the capability-reference K experiment as H is forbidden: K isolates one bounded reference and has no candidate/review/revision workflow.

## 2. Goals

- Produce real, reproducible H outputs for the same evaluation tasks and model configuration used by S.
- Measure the effect and cost of a minimal Harness workflow without bringing Phase 2 product architecture into Phase 0.
- Preserve blinded human review, split discipline, traceable prompts, stable hashes, and explicit failure states.
- Keep credentials, generated prose, reviewer identity, and raw private review data out of committed public artifacts.
- Give P0-T09 enough evidence to freeze sample-size, cost, quality, and non-inferiority rules without inventing fixed thresholds.

## 3. Non-goals

- No product API, SSE event, React page, menu, setting, Automation, or workspace migration.
- No Author Finalization or write into formal novel Markdown.
- No production CandidateSet, ReviewIssue repository, PreferenceMemory, Capability Router, or Skill installer.
- No third-party package installation or script execution.
- No use of K, model self-scores, AI detectors, or deterministic fixtures as human quality evidence.
- No release-holdout generation, review, or threshold tuning in Phase 0.

## 4. Compared arms

### S: ordinary single turn

S remains the frozen `single-turn-v1` baseline: one model call with the task's allowed facts, task-level QualitySpec, and fixed template.

### H: `p0-offline-harness-v1`

H uses the same task, allowed facts, Provider, model, model profile, temperature, output-token ceiling, and task-level QualitySpec as S. Its only intended difference is the frozen workflow:

1. Generate candidate A.
2. Generate candidate B independently from the same input.
3. Review both candidates against the QualitySpec and return structured issues plus a preferred base candidate.
4. Revise the preferred candidate using only the two candidates, structured review, original allowed facts, and QualitySpec.

The fourth output is the H prose used for blind comparison. All four calls count toward token and cost totals. Candidate prompts may use deterministic candidate labels but cannot leak arm identity into prose.

### K: capability-reference isolation

K remains a separate tuning-only experiment. It adds exactly one bounded capability reference to S and does not become H. A future combined workflow may be evaluated only as a separately named cohort after S/H and S/K evidence can be distinguished.

## 5. Cohort discipline

The corpus currently contains 18 `tuning`, 12 `regression`, and 6 `release_holdout` tasks, balanced across the three Profiles.

- `tuning`: exercise the runner and revise templates before freezing `p0-offline-harness-v1`; results cannot support a release claim.
- `regression`: after template hashes are frozen, run the paired blind P0 pilot used to estimate variance and operational cost.
- `release_holdout`: retain only registered task metadata and hashes. Do not generate or review it in Phase 0.

Run identity must include the manifest hash, selected split/cohort, S template hash, H policy hash, and model-configuration hash. A template, policy, model, or cohort change creates a new run rather than mutating historical evidence.

## 6. Components and boundaries

### CLI orchestration

`cmd/quality-eval` gains explicit cohort-aware commands/options for creating an S run and executing H. Live execution is always explicit; validation, packaging, and summarization remain independently runnable offline.

### Evaluation package

`internal/quality/evaluation` owns:

- a versioned H policy contract;
- deterministic request assembly for candidate, review, and revision stages;
- structured review parsing and validation;
- call-level usage, cost, hash, and failure records;
- atomic stage persistence and safe resume;
- cohort-aware blind packaging and summaries.

The package depends on a small injected generation interface. Unit tests use fakes and never call a live Provider.

### Frozen templates and policy

Versioned candidate, review, and revision templates live beside the existing evaluation templates. A machine-readable policy records template hashes, stage order, candidate count, allowed inputs, output caps, selected split, and failure semantics.

### Provider adapter

The existing OpenAI-compatible Eino adapter is reused. The DeepSeek credential is loaded at runtime only. The current machine's credential may be sourced into the process from the existing Hermes environment file, but its value and source path are never written to run artifacts, logs, documentation, or Git.

## 7. Data flow

1. Load and strictly validate the corpus manifest and selected non-holdout cohort.
2. Resolve the frozen model configuration and runtime-only credential.
3. Create a stable run record before live calls.
4. Execute or resume S for the selected cohort.
5. Execute H stages in order, atomically persisting each successful stage.
6. Mark a task H-ready only when both candidates, structured review, and final revision are valid.
7. Package only complete S/H pairs into randomized A/B material; source labels and private maps remain separate.
8. Accept two independent human reviews and optional third-person adjudication.
9. Summarize paired outcomes, Profile strata, fact errors, author-edit observations, tokens, and cost.
10. P0-T09 consumes the regression pilot to freeze versioned gate rules; it does not claim that H already passes future Phase 2/3/5 quality gates.

## 8. Context and privacy limits

- Every stage receives only source-bounded task facts, QualitySpec, and the minimum prior H artifacts required by that stage.
- Candidate outputs, review JSON, and final revision have explicit byte and token ceilings recorded in policy.
- The revision call cannot receive provider reasoning, debug logs, unrelated Skills, prior tasks, reviewer feedback, or release-holdout material.
- Raw generated prose, blind maps, reviewer identities, and free-form reviewer evidence are private run data by default.
- Committed artifacts contain contracts, template/policy hashes, statuses, aggregate metrics, and reproducibility metadata, not credentials or private review content.

## 9. Failure and resume semantics

- Provider/configuration absence is `ENVIRONMENT-BLOCKED` before any call.
- A live call or parse failure records its exact stage and stable failure type; it is never converted to a partial H result.
- A task with any missing H stage is not packaged for blind review.
- Successful stages may be resumed without repeating paid calls when their input, policy, template, and model hashes still match.
- Hash mismatch creates a new run or invalidates the stage; it never silently reuses stale output.
- No fixed LLM timeout is introduced. Cancellation follows the caller context; retry policy, if enabled, is explicit and versioned.

## 10. Testing and validation

- Contract tests for policy, run identity, cohort selection, and release-holdout exclusion.
- Request-assembly tests proving S/H factual inputs and model configuration match.
- Fake-generator integration tests for four-stage H execution, atomic resume, failure recording, and no partial blind package.
- Parser tests for malformed or oversized review output.
- Privacy tests rejecting credentials, reasoning content, reviewer identity, and raw comments from committed artifacts.
- Deterministic blind-package and summary tests, including missing arms and adjudication.
- Fresh targeted/full Go tests, `go vet ./...`, frontend/i18n/build gates required by P0-T09, secret scan, and `git diff --check`.
- One explicit live smoke task precedes paid cohort execution; full execution proceeds only if the smoke record is valid and contains no secret leakage.

## 11. Plan correction

The Phase 0 detailed plan and related gates must replace the blanket prohibition with this evaluation-only allowance. P0-T09 must distinguish:

- runner and data readiness;
- tuning evidence;
- regression pilot evidence;
- untouched release holdout;
- thresholds frozen for future evaluation;
- actual future quality-gate PASS, which remains unclaimed until the relevant phase.

Phase 1 may begin only after P0-T09's engineering gates, approved ADRs, reproducible S/H pilot, and versioned gate manifest are complete. The offline runner does not become a product capability merely because it exists.

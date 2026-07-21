# ADR-RI-001: ReviewIssue v1

## Status

**Accepted v1** — 2026-07-21

This decision approves ReviewIssue contract `v1`. It is a controlled-evolution boundary, not a permanent freeze. Changes to issue identity, binding, status, severity, revision routing, evidence, or closure semantics require a superseding ADR, a new contract version, compatibility rules, and a migration path.

## Context

Review is useful only when an author can locate the problem, observe its reading effect, understand the cause, route a minimum-impact revision, and verify the outcome. A score or generic instruction to “polish” cannot support that loop. Reviewer output must contain findings and reader-observable evidence, never hidden writer chain-of-thought or self-review.

Review must also remain attached to the exact bytes it assessed. A later candidate, changed Profile, or edited artifact cannot inherit an issue silently. Reviewers advise; they do not acquire authority to mutate formal manuscript content or close an issue without re-verification evidence.

## Decision

### Stable issue and reviewed-byte binding

A ReviewIssue v1 record uses `contract.kind=denova.review-issue`, `contract.version=v1`, schema `review-issue-v1.schema.json`, and a stable `issue_id`. It records one stable revision-routing Capability ID from the closed v1 set and `unknown_capability_id=reject_explicitly`; unknown IDs never fall back to generic polishing.

Every issue binds the complete chain: workspace ID/revision/hash, run ID/version, stage ID/type/version, reviewed Artifact ID/type/version/hash, CandidateSet ID/hash, candidate ID/content hash, Source Manifest ID/version/hash, Profile ID/version/hash, QualitySpec ID/revision/version/hash, and reviewed-content hash. The attachment kind is exactly `candidate`, `candidate_set`, or `finalized_artifact`; a finalized attachment also names its Finalization receipt. Semantic validation recomputes all hashes and confirms references describe the same reviewed bytes.

### Required finding and evidence

Every issue contains:

- a precise artifact path and byte range, anchor hash, and quoted-text hash;
- a prose, reader-observable effect and at least one quoted excerpt with location and hash;
- an exhaustive cause category plus prose explanation;
- severity and revision layer from closed v1 vocabularies;
- a minimum-impact recommendation, exact affected range, and dimensions to recheck;
- reviewer provenance with stable source identity, version, and hash;
- status, complete lifecycle history, and current re-verification result/history.

An optional numeric score from 0–100 is only a summary. The evidence summary and excerpt remain mandatory at every score. Reviewer output policy is `evidence_and_findings_only`; writer chain-of-thought access is `forbidden`; formal mutation authority is `false`.

The exhaustive severity vocabulary is `blocking`, `major`, `moderate`, `minor`. Cause categories are `fact`, `story_structure`, `character`, `scene`, `information`, `causality`, `dialogue`, `pacing`, `language_style`, and `profile_fit`. Revision layers are `fact`, `structure`, `scene`, `character`, `dialogue`, `pacing`, and `language`. Unknown enum values fail explicitly.

The revision Capability IDs are exactly `revision.fact`, `revision.structure`, `revision.scene`, `revision.character`, `revision.dialogue`, `revision.pacing`, and `revision.language`. This v1 routing surface is controlled contract data; a future Capability Registry extension needs a new compatible contract/version rather than an unknown-ID fallback.

### Lifecycle and closure

The exhaustive status vocabulary is `open`, `revision_proposed`, `resolved`, `verified_closed`, `reopened`, and `dismissed`. The only transitions are:

- creation to `open`;
- `open -> revision_proposed | dismissed`;
- `revision_proposed -> resolved | open | dismissed`;
- `resolved -> verified_closed | reopened`;
- `verified_closed -> reopened`;
- `reopened -> revision_proposed | dismissed`;
- `dismissed -> reopened`.

No catch-all transition exists. Each history entry records actor, reason, time, from/to status, and reviewed-content hash. `resolved` means a revision claims to address the finding, not that review passed. Only a `passed` re-verification against the revised bytes may produce `verified_closed`; a failed check produces or supports `reopened`. Re-verification preserves old attempts, evidence, verifier provenance, and hashes rather than overwriting history. Dismissal requires an author or review-lead reason and may be reopened.

Reviewers may create or update review artifacts and revision recommendations. They cannot edit formal content, manufacture author approval, invoke Author Finalization, or make an issue disappear. A Revision tool produces another candidate/revision Artifact; formal content changes only through Author Finalization.

## Alternatives

### Score-only review

Rejected. Scores are not localizable, explainable, or independently verifiable.

### Free-form severity and routing labels

Rejected. Unknown values would drift across UI, router, analytics, and migrations.

### Reviewer closes an issue when it emits a revision

Rejected. Revision generation is not evidence that the reader-observable problem disappeared.

## Consequences

- Issues are actionable, byte-bound, routable, and reopenable.
- Review history can support precision/actionability evaluation without exposing hidden reasoning.
- Closed vocabularies require controlled version evolution as capabilities grow.
- Reference/hash equality and history continuity require semantic validation beyond JSON Schema.

## Migration

P0-T05 creates documentation and schemas only; it rewrites no review data. A v1 reader rejects malformed or unknown records for managed mutation while preserving bytes for safe inspection/export. A future version must publish a superseding ADR, explicit enum/capability mapping, schema compatibility, previewable provenance-preserving conversion, and rollback.

## Validation

`contracts/review-issue-v1.schema.json` is the normative shape and closed-vocabulary companion. Draft 2020-12 validation must accept an in-memory full binding chain and reject missing provenance, missing reader-observable evidence, unknown capability/enum values, illegal lifecycle transitions, and invalid closure payloads. A semantic validator must recompute reviewed-byte/evidence hashes, validate range and reference equality, ensure history is contiguous and ends at current status, and prove a closed issue has passed re-verification against the revised bytes. `git diff --check` and exact-scope review are required.

## Supersedes

None.

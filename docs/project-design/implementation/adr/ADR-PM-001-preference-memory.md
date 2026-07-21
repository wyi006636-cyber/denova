# ADR-PM-001: PreferenceMemory v1

## Status

**Accepted v1** — 2026-07-21

This ADR approves PreferenceMemory contract `v1` and its normative companion `contracts/preference-memory-v1.schema.json`. It is a controlled-evolution boundary: a change to signal kinds, scope precedence, provenance, confirmation, effective-state rules, or hash binding requires a superseding ADR, a new contract version, compatibility rules, a previewable migration, and rollback.

## Context

Creative preference is personal, contextual, and revisable. Treating model inference, passive UI activity, generated prose, hidden reasoning, automation completion, or reviewer scoring as memory would make the system silently teach itself beliefs the author did not express. It would also let a local task preference become false universal platform truth.

CandidateSet and ReviewIssue already preserve reviewable evidence, but neither may turn its own output into author preference. PreferenceMemory needs a narrow, auditable input boundary that later candidate generation or resolution can consult without mutating formal workspace content or bypassing Author Finalization.

## Decision

### Contract, authority, and explicit signal vocabulary

Each append-only record has `contract.kind=denova.preference-signal`, `contract.version=v1`, schema `preference-memory-v1.schema.json`, a stable `signal_id`, and a required explicit-author confirmation. The only v1 event kinds are `selection`, `mixed_selection`, `rejection`, `author_rewrite`, `rule_confirmation`, `revocation`, and `correction`; unknown kinds fail explicitly. `mixed_selection` is the lineage-preserving form of explicit selection, while `correction` and `revocation` are append-only maintenance events for those author-created signals.

Only an explicit author action may create one of those records: selecting a candidate or mixed segment composition, explicitly rejecting a candidate or issue recommendation, rewriting exact source bytes, confirming an authored rule, revoking a prior signal, or correcting a prior signal. The actor is always `author`; Agent, Harness, Skill, reviewer, and Automation may propose a signal preview but cannot persist it. Model inference, passive dwell time, generated text, hidden reasoning, workflow completion, telemetry, model/Agent self-rating, and reviewer score are never signals.

### Required provenance and binding

Every signal binds author, project, workspace ID and canonical path, current workspace revision and content hash; source operation; resolved Profile ID/version/hash; resolved QualitySpec ID/revision/version/hash; source content hash; schema/contract version; timestamp; explicit confirmation evidence hash; reason; strength; and confidence. Its event-specific reference is mandatory and closed: selection names a candidate set/candidate/hash; mixed selection names the composed artifact/hash, parent candidates, and segment-map hash; rejection names a candidate or issue and hash; author rewrite names the exact original and rewritten artifacts/hashes; rule confirmation names the rule/hash; correction names replacement evidence plus superseded signal IDs; revocation names its targets plus reason hash. Each event fixes the exact confirmation method and permitted source kind, and forbids irrelevant supersession/revocation fields. Unknown combinations fail schema validation.

Hash strings are SHA-256 lower-case hexadecimal values. Shape validation is insufficient: the PreferenceMemory service recomputes referenced canonical bytes and confirms the source is a real, bound CandidateSet/ReviewIssue/finalization record. A copied record, changed artifact, changed Profile/QualitySpec, unknown source reference, actor mismatch, or confirmation whose bound evidence hash does not match is rejected rather than refreshed.

### Scope, precedence, conflicts, and effective state

Signals are scoped to one author and additionally to `workspace`, `project`, or `author`. They are never platform-wide or shared as anonymous product truth. When resolving a specific author/workspace/project/dimension, precedence is: applicable `workspace` signals, then applicable `project` signals, then applicable `author` signals. At equal scope, later explicit correction/supersession wins; otherwise stronger explicit signal wins; if still tied, newer `recorded_at` wins, then lexical `signal_id` is the deterministic final tie-breaker. A revocation removes its named target from effective state but never deletes it.

A signal may supersede one or more prior signal IDs and `revocation` must name one or more revoked signal IDs. The append log is immutable: editing a preference emits `correction` or a replacement with `supersedes_signal_ids`; revoking emits another record with `provenance.source_kind=author_revocation`, never a fabricated rewrite or a copy of the target's original source kind. Semantic validation checks that targets exist, belong to the same author, scope is not broadened without a new explicit author confirmation, and the reference graph has no cycles. The effective resolver returns applicable records, suppressed/revoked records, and its deterministic reason; ambiguous input is an error, not a guess.

### Use boundary

PreferenceMemory may be supplied as bounded, attributed input to later candidate generation, comparison, or QualitySpec/Profile resolution. It may bias suggestions but cannot mutate formal content, alter a QualitySpec/contract automatically, change a CandidateSet decision, confirm a request, or invoke Author Finalization. It must show the author which signals were used and retain the original records for inspection/export.

### Worked decisions

- An author chooses candidate `cand-b` and explicitly confirms “shorter opening dialogue” as the reason. The system appends a `selection` signal bound to that candidate's exact bytes and current Profile/QualitySpec; it may later recommend the same preference only in the applicable scope.
- The author explicitly rejects a review issue recommendation because it removes intentional ambiguity. The system appends a `rejection` signal; a reviewer score alone would not qualify.
- The author directly rewrites an accepted opening and explicitly confirms the before/after bytes as a reusable preference. The system appends an `author_rewrite` signal bound to both artifact hashes; an Agent rewrite or a mere saved draft does not qualify.
- The author withdraws an earlier “always use first person” rule. A `revocation` appends the target ID; history remains visible and the deterministic resolver excludes the earlier signal.

## Alternatives

### Infer preferences from behavior or model output

Rejected. Inference is neither author authority nor reviewable consent.

### Overwrite a single mutable preference profile

Rejected. It loses why a preference existed, its source bytes, and the ability to revoke safely.

### Store one global platform preference

Rejected. Individual author intent must not become product folklore or another author's default.

## Consequences

- Preference influence is narrower and more verbose, but every effective preference is explainable and reversible.
- Consumers need bounded retrieval and semantic reference/hash checks; JSON Schema alone cannot prove those relations.
- This ADR creates no automatic formal write path and changes no existing workspace data.

## Migration

P0-T06 introduces documentation/schema only. A v1 reader rejects unknown authoritative records for managed use while preserving their bytes for inspection/export. Future conversion must retain event history, target references, confirmation evidence, and effective-state explainability; it must preview before write and be reversible.

## Validation

Draft 2020-12 validation accepts in-memory explicit selection, author-rewrite, rule-confirmation, and revocation records and rejects unknown event/scope/actor values, rating/self-assessment events, missing confirmation, and revocation without targets. Service validation additionally recomputes hashes, validates source references and confirmation binding, checks append/supersession/revocation semantics, and deterministically resolves conflicts. `git diff --check` and exact-scope review are required.

## Supersedes

None.

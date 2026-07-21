# ADR-PROFILE-001: Quality Profile v1

## Status

**Accepted v1** — 2026-07-21

This decision approves the Quality Profile v1 contract. Version `v1` is a controlled evolution boundary, not a permanent freeze: a change to identity, merge semantics, authority, or required fields needs a superseding ADR, a new contract version, compatibility behavior, and an explicit migration path.

## Context

Denova needs distinct quality defaults for long serial fiction, Fanqie-style short fiction, and Zhihu Salt-style short fiction without building three engines. Platform practice changes, authors intentionally depart from defaults, and model suggestions are not author decisions. A Profile therefore supplies versioned, sourced data to the shared Quality Harness; it does not encode a platform implementation branch or silently become creative truth.

Workspace Schema v1 places the authoritative selected profile at `.denova/profile-lock.json` and author-controlled QualitySpecs beneath `.denova/quality/specs/`. Both are version-protected formal records. A model, Skill, or Automation may create a candidate Artifact, but only an explicit author-facing write path may change the authoritative Profile or QualitySpec.

## Decision

### Identity and one-engine boundary

The complete Profile v1 ID set is exactly:

- `long_serial`
- `fanqie_short`
- `zhihu_salt_short`

An unknown ID is an explicit validation error. There is no default Profile and no silent fallback. All three IDs use engine `denova.shared-quality-engine` and contract `v1`. Profile differences are contract data consumed by that engine; application code must not create platform-specific orchestration, persistence, Agent, SSE, or finalization branches.

Every Profile record contains:

- `contract`, stable `profile_id`, bilingual `display_name`, and the shared `engine_contract`;
- Profile-level provenance and explicit identity/error policy;
- sourced settings for required Artifacts, required Capability IDs, candidate policy, review rubric, and export configuration;
- a concrete operation walkthrough and a bound QualitySpec v1 instance.

Capability IDs describe needed behavior, not a particular Skill name. Export configuration describes a requested output shape, not permission to overwrite the manuscript or publish.

### Mutable settings and provenance

No mutable platform rule is timeless hardcoded truth. Every mutable setting is a record with:

- stable `id` and `value`;
- `source_id`, `source_kind`, `source_ref`;
- `observed_at`, `effective_from`, and `recorded_at`;
- an `author_override_policy` stating allowed scopes, mandatory explicit confirmation, and explicit rejection of unsupported values.

The source may be an approved plan, a dated platform observation, a project contract, or an author decision. A platform observation is evidence for a default only as of its dates; a later observation is proposed as a versioned candidate and never overwrites an active Profile. Values such as length, pacing, number of candidates, or export packaging remain author-overridable when the setting policy permits it.

Profile identity, contract version, shared-engine identity, author-control rules, and the rule that model proposals are candidate-only are not mutable Profile defaults.

### Profile intent

`long_serial` supplies long-horizon continuity, chapter-progress, state-carrying, and chapter-end momentum defaults. It may require chapter plans, state evidence, and continuity review, but it does not impose short-story opening or ending rules on every chapter.

`fanqie_short` supplies short-form opening clarity, immediate premise/payoff visibility, escalation, and ending-delivery defaults. It does not inherit volume/chapter-group machinery merely because the shared engine also serves serial fiction.

`zhihu_salt_short` supplies narrative-voice, causal pressure, information control, reversal-evidence, and closure defaults. It does not use a reversal as an unsupported surprise: the final turn must be traceable to prior evidence under its QualitySpec.

### Resolution and author control

Profile defaults are the first layer of QualitySpec resolution. The deterministic order is:

1. Profile defaults
2. Project overrides
3. Task overrides
4. Explicit author confirmation for this operation

Later layers win only for a known goal, a supported value, and a scope allowed by the goal and setting policies. Overrides use `set`; they cannot delete a project red line or invent a scope. The resolver records every considered value, its provenance, the winning layer, and the operation confirmation ID. Unknown goals, Profile IDs, layer names, operations, values, or unauthorized changes fail explicitly.

A model, Skill, or Automation may add a `candidate_only` proposal with `applied: false`. It cannot mutate `.denova/profile-lock.json`, write an authoritative QualitySpec revision, create an author confirmation, or make its proposal win resolution.

## Walkthroughs

### Long serial, chapter 12

The `long_serial` example resolves operation `draft_chapter_12`. Its Profile defaults set continuity to `normal` and chapter-end momentum to `medium`. The project raises continuity to `strict`; the task raises chapter-end momentum to `high`; the operation-time author confirmation approves those resolved values without replacing either source layer. Continuity therefore records `project_overrides` as its winner and momentum records `task_overrides` as its winner. The fixture binds chapter 12 to `setting/chapter-12-plan.md` and `chapters/0012.md`. A model suggestion to relax continuity remains an unapplied candidate.

### Fanqie-style golden opening

The `fanqie_short` example resolves `evaluate_golden_opening` for the opening Artifact. The reusable opening-clarity goal stays at its Profile default `clear`; a second reusable goal sets hook intensity to Profile default `medium`. The task raises hook intensity to `high`, and the operation-time author confirmation approves the resolved values without replacing the task source. Hook intensity therefore records `task_overrides` as its winner. Evidence must quote the opening text rather than rely on a platform score.

### Zhihu Salt-style reversal ending

The `zhihu_salt_short` example resolves `evaluate_reversal_ending`. The reversal-evidence Profile default is `traceable`, and the causal-closure Profile default is `open`. The project raises the evidence chain to `strict`; the task raises causal closure to `closed`; the operation-time author confirmation approves both resolved values without replacing their source layers. Evidence therefore records `project_overrides` as its winner and closure records `task_overrides` as its winner. Review evidence must point from the ending reversal to earlier planted facts and unresolved causes; unsupported surprise is rejected rather than rewarded.

## Schema authority and compatibility

`contracts/profile-v1.schema.json` is the normative machine-readable companion for shape and closed-value validation. It references the sibling `contracts/quality-spec-v1.schema.json`; neither schema is a second implementation truth or a substitute for this ADR's authority and merge semantics. P1-T01 may choose Go field layout, but it must preserve these identities, error behavior, source fields, author boundary, and deterministic result.

A v1 reader rejects malformed authoritative records for managed operations while preserving the bytes and keeping safe workspace access available under Workspace Schema v1. A newer/unknown contract is read-only input until a compatible reader or controlled migration is available. A future Profile version may add Profiles or fields only through a superseding decision; extending the enum silently is not v1-compatible.

## Alternatives

### Separate engine per platform

Rejected. Three orchestration and persistence implementations would drift, duplicate safety boundaries, and turn platform labels into architecture.

### Timeless hardcoded platform rules

Rejected. Platform practice changes, so unsourced constants would become invisible stale truth and prevent informed author overrides.

### Model-managed authoritative Profiles

Rejected. A model may propose candidates, but silent authoritative mutation violates the author-control and workspace-truth boundaries.

## Consequences

- One engine can evolve independently of platform data while Profile behavior stays reviewable and versioned.
- Defaults can become stale without becoming invisible; dates and provenance make refresh decisions explicit.
- Author overrides remain possible but auditable and scoped.
- Examples are intentionally complete contract fixtures, not production defaults loaded at runtime.

## Migration

P0-T04 creates no production data and requires no current workspace migration. P1-T01 must read only exact v1 records, preserve unknown/newer records as read-only input, and reject unknown IDs rather than remap them. A future contract change must publish a superseding ADR, a new schema version, a previewable conversion, provenance preservation, explicit author confirmation, and rollback before rewriting `.denova/profile-lock.json` or bound QualitySpecs.

## Validation

- Both committed schemas must pass Draft 2020-12 meta-validation and JSON parsing.
- Each of the three complete Profile fixtures must pass the Profile schema and its embedded QualitySpec must pass the QualitySpec schema.
- Walkthrough inspection must confirm the exact reusable goals, default/project/task winners, provenance chains, and shared operation confirmation narrated above.
- In-memory negative cases must reject an unknown Profile ID, a goal missing evidence or provenance, an unauthorized override, malformed resolution order, and missing operation confirmation.
- `git diff --check` and exact-scope review must pass before acceptance.

## Supersedes

None.

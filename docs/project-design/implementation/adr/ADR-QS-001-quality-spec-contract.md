# ADR-QS-001: QualitySpec v1

## Status

**Accepted v1** — 2026-07-21

This decision approves the QualitySpec v1 contract. Version `v1` is a controlled evolution boundary, not a permanent freeze. Changes to goal identity, resolution order, author authority, provenance, or validation behavior require a superseding ADR, a new contract version, compatibility rules, and a migration path.

## Context

A QualitySpec states what “good” means for one work, task, and operation. It must be readable and author-controlled rather than a fixed platform scorecard. It also must not become a second workflow engine: the shared Quality Harness owns execution, while Profile and QualitySpec records provide validated inputs.

Quality goals need stable meaning and evidence. Profile defaults, project intent, task constraints, and an operation-time author decision have different authority and lifetimes; conflating them would let a transient model proposal silently rewrite durable creative direction.

## Decision

### Contract and reusable goals

A QualitySpec v1 record contains `contract.kind=denova.quality-spec`, `contract.version=v1`, a stable `spec_id`, monotonic `revision`, one exhaustive Profile ID, a reusable `goal_catalog`, four resolution layers, candidate changes, and a resolution receipt.

Every reusable quality goal contains all of the following:

- stable `id` and goal contract/version metadata;
- bilingual description;
- source provenance, including source reference and observed/effective/recorded dates;
- bilingual purpose;
- explicit Profile, operation, and Artifact scope;
- priority (`must`, `should`, or `could`);
- evidence requirement with kind, bilingual description, minimum count, and accepted sources;
- a value contract with supported values and `reject_explicitly` for unknown values;
- allowed override scopes.

The catalog defines reusable goal meaning. It is distinct from a Profile default binding, which selects a value for that goal; from project and task overrides, which set scoped values; and from operation-time author confirmation, which authorizes the final resolved contract for one operation.

### Deterministic layering

The only merge order is:

`profile_defaults -> project_overrides -> task_overrides -> operation_confirmation`

Resolution is deterministic:

1. Validate the Profile ID, contracts, goal catalog, values, scopes, provenance, and authorizations.
2. Start with validated Profile default bindings.
3. Apply confirmed project overrides in record order, only within goal-allowed project scope.
4. Apply confirmed task overrides in record order, only within goal-allowed task scope.
5. Apply confirmed operation overrides in record order, only within operation scope.
6. Require the operation-level author confirmation even when it does not change every value.
7. Emit one resolved value per applicable goal, its winning layer, the ordered provenance chain, and the author confirmation ID.

Later layers never gain new powers merely by being later. `delete` is forbidden in v1; a layer cannot remove a red line, change Profile identity, reference an unknown goal, supply a value outside the goal contract, or claim a disallowed scope. Duplicate conflicting writes within one layer, mismatched confirmation IDs, missing evidence/provenance, unknown enums, and unsupported values are explicit validation failures. Implementations must return a structured error identifying the offending goal/layer/value; they must not fall back or discard the record silently.

The committed schema validates structural and closed-enum constraints. P1-T01's semantic validator must additionally verify cross-record references, uniqueness of goal IDs, value membership/type against each goal's `value_contract`, allowed-scope membership, ordering/chain integrity, and equality between the Profile ID, operation confirmation, and resolved records. This validator implements this ADR; it does not invent a second set of defaults.

### Provenance and author control

Every default, override, resolution step, and mutable Profile setting carries provenance. The resolver preserves the full considered chain, not only the winner. This allows an author to see whether a value came from dated Profile evidence, the project contract, the current task, or the operation confirmation.

Project, task, and operation overrides require an authorization record with `actor=author`, `decision=confirmed`, a stable confirmation ID, and timestamp. A model, Skill, or Automation may only emit `candidate_changes` with `status=candidate_only` and `applied=false`. Turning a candidate into an override requires a separate explicit author action and a new authoritative revision.

QualitySpec records live under `.denova/quality/specs/`, remain author-controlled formal records, and enter workspace versions. Runs, scores, and projections may reference a spec revision and hash but cannot replace it or reverse-write it.

### One shared quality engine

All three Profiles resolve through the same QualitySpec v1 algorithm. The engine consumes stable goal IDs, Capability IDs, evidence requirements, and resolved values. It does not dispatch to three platform-specific engines. Schemas define the interchange boundary and closed values; they are not executable workflow definitions, scoring code, prompts, or a parallel source of business defaults.

## Walkthroughs

### Chapter 12 of a long serial

The example catalog defines continuity and chapter-end momentum. Profile defaults select `normal` and `medium`; a project author sets continuity to `strict`; the chapter task sets momentum to `high`; the operation-time author confirmation approves both resolved values without introducing another override. The receipt records project as the winner for continuity and task as the winner for momentum, while both resolved goals reference the operation confirmation.

### Fanqie-style golden-opening evaluation

The example catalog defines opening clarity and hook intensity for `opening_draft`. Profile defaults select `clear` and `medium`; the task sets hook intensity to `high`; the operation-time author confirmation approves the resolved values without introducing another override. The receipt records Profile default as the winner for clarity and task as the winner for hook intensity. Review evidence requires quoted opening spans and an author-readable judgment. A model proposal for `extreme` cannot apply because it is candidate-only and the value is unsupported.

### Zhihu Salt-style reversal-ending evaluation

The example catalog defines reversal evidence and causal closure with Profile defaults `traceable` and `open`. A project author raises reversal evidence to `strict`; the ending task sets closure to `closed`; the operation-time author confirmation approves both resolved values without introducing another override. The receipt records project as the winner for evidence and task as the winner for closure. A reversal with no earlier textual evidence fails the evidence requirement even if a model rates it highly.

## Validation

`contracts/quality-spec-v1.schema.json` is the normative machine-readable companion for JSON shape and closed values. `contracts/profile-v1.schema.json` binds each Profile example to it. The ADR is authoritative for semantics that JSON Schema cannot express across catalog references and provenance chains.

The three committed examples must parse and validate under JSON Schema Draft 2020-12. Negative validation must reject at least an unknown Profile ID, a goal missing evidence/provenance, and malformed or unauthorized override/resolution data. Negative cases remain temporary or in memory, not committed fixtures.

Walkthrough validation must additionally compare each fixture's goal catalog, layer bindings, resolved winners, provenance chains, and operation confirmation against the narrated deterministic chains above. P1-T01 semantic validation remains responsible for cross-record checks JSON Schema cannot express.

## Alternatives

### One fixed global scorecard

Rejected. It cannot express different work, task, and operation intent and would confuse a metric with author-defined quality.

### Last-write-wins without provenance

Rejected. It hides authority, makes stale platform defaults indistinguishable from author decisions, and prevents reviewable resolution.

### Let model proposals become active values

Rejected. Model output remains candidate evidence until a separate explicit author action creates an authoritative revision.

## Consequences

- Authors can inspect both the final value and why it won.
- Goal meaning is reusable without treating a default binding as universal truth.
- Model proposals stay reviewable but non-authoritative.
- The v1 schema is intentionally limited to fields consumed by the planned shared engine and semantic validator.

## Migration

P0-T04 introduces documentation and fixtures only, so no current workspace data is rewritten. P1-T01 must reject unknown/newer contracts for managed mutation while preserving bytes for safe read/export. A future QualitySpec version must provide a superseding ADR, schema/version compatibility, a preview of goal and layer transformations, provenance-preserving conversion, explicit author confirmation, and rollback before replacing records under `.denova/quality/specs/`.

## Supersedes

None.

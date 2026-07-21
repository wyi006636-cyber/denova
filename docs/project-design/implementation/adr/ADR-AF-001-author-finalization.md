# ADR-AF-001: Author Finalization v1

## Status

**Accepted v1** — 2026-07-21

This ADR approves Author Finalization contract `v1` and its normative companion `contracts/author-finalization-v1.schema.json`. It is a controlled-evolution boundary: changes to confirmation, request bindings, durable state, receipt, idempotency, recovery, or authority require a superseding ADR, new contract version, compatibility rules, previewable migration, and rollback.

## Context

Formal workspace files are the author's source of truth. Candidate, Agent, reviewer, and Automation output must remain pending until a human author approves exact bytes. The repository already provides a workspace-change lease/revision service with durable multi-path intent and roll-forward recovery, and a `go-git` version service. They are separate durable resources, so a finalization contract must coordinate them honestly rather than claim database-style cross-resource ACID.

## Decision

### Authority and the editor-save boundary

Author Finalization is the only Harness/Agent/Automation path permitted to move pending quality artifacts into formal workspace content. It requires explicit author confirmation and never writes formal content merely because an Agent, Skill, reviewer, or Automation proposed a change.

A direct editor save is a separate explicit author-originated formal mutation, not an AI bypass. It continues to use `workspacechange.Service.SaveFile` under the same workspace mutation lock and revision/CAS protection. It does not consume an Author Finalization nonce because the author writes their own editor bytes directly. Harness/Agent/Automation writes—including reviewed candidate application—must use Author Finalization, never loop over `os.WriteFile`, and must use the workspace-change named durable operation.

Automation modes are constrained further: an `auto_write` task may generate pending artifacts or pending change proposals only. It cannot self-confirm, mint confirmation evidence, call a direct formal write, or complete Author Finalization. `confirm_write` may request an author review/confirmation; `read_only` cannot write either area.

### Exact authorization binding and invalidation

A `request` record uses `contract.kind=denova.author-finalization`, `contract.version=v1`, an immutable request ID, and the following complete binding:

- canonical workspace ID, project ID, canonical root path, base revision, expected revision, and workspace hash;
- one tagged eligible CandidateSet output: either `selected` with state `author_selected` and exact candidate/artifact identity/hash, or `mixed` with state `mixed`, composed artifact/hash, parent candidate IDs, and segment-map hash;
- each canonical target ID/path, per-path base revision, artifact identity, and exact bytes SHA-256 hash;
- resolved Profile and QualitySpec identity, revision/version, and hash;
- explicit author identity, confirmation timestamp/evidence hash, authorization ID, single-use nonce, idempotency key, and canonical bound-payload hash.

The service canonicalizes the root and every target with the existing workspace-path protection and rejects out-of-root paths and symlink escape attempts. It rejects duplicate target IDs, duplicate canonical target paths, and duplicate artifact IDs; JSON Schema `uniqueItems` cannot prove member-field uniqueness. It also verifies that every target artifact/output hash matches the tagged eligible CandidateSet v1 output, recomputes canonical payload and artifact hashes immediately before write, and revalidates every base revision after the durable intent is prepared. A changed byte, hash, path, workspace identity, revision, Profile, QualitySpec, candidate identity, or request payload invalidates authorization. Missing confirmation, invalid candidate state, stale/mismatched hashes/revisions, wrong workspace/path, or a mismatched replay fails before a formal write.

### Durable operation, idempotency, and recovery

The authorization nonce is single-use for one canonical payload. The nonce/idempotency mapping and final receipt are durable. A duplicate SSE submit, reconnect, or process retry with exactly the same nonce and payload returns/replays the existing operation or receipt; it does not duplicate formal writes, workspace-change operation, version checkpoint/commit, receipt, or optional PreferenceSignal. A consumed nonce with a different payload is rejected explicitly.

The exhaustive durable operation states are `prepared`, `writing`, `workspace_committed`, `version_checkpointing`, `needs_recovery`, `succeeded`, `compensated`, and `failed`. Schema conditionals make their fields consistent: only `succeeded` may claim a successful non-null version checkpoint/commit and durable receipt, completed recovery, no failures, and no compensation; `needs_recovery` requires a workspace-change operation, failure evidence, and pending/manual recovery; `compensated` requires completed compensation; `failed` requires failure evidence and a settled or manual-intervention recovery. Pre-terminal states cannot carry a terminal receipt. Recovery is idempotent: it loads the durable request and workspace-change intent, then roll-forwards an unambiguous prepared operation, reconciles visible bytes against expected before/after hashes, or records compensation/manual intervention. Cancellation or SSE disconnection only disconnects the caller; it does not abandon a prepared durable operation.

The service first persists its authorization/operation intent, then lets the existing workspace-change service prepare and roll forward the multi-path formal bytes under the workspace lease, then records/creates the `go-git` checkpoint/commit, and finally persists a terminal receipt. Workspace-change provides all-old/all-new intent recovery for its paths. The Git checkpoint and receipt are separate resources, so this is not cross-resource ACID: if formal bytes are durable but Git/receipt persistence fails, recovery reconciles the same operation without reapplying bytes; it either records the missing checkpoint/receipt, uses a verified compensating restore under the same lease, or leaves a durable `manual_intervention` state with exact hashes and paths. No component may claim success until a durable terminal receipt exists.

### Receipt and audit record

Every attempt creates or updates one durable receipt with outcome `succeeded`, `needs_recovery`, `compensated`, `failed`, or `rejected`. Tagged outcome branches prevent contradictory claims. Success requires before/after workspaces, changed paths, workspace-change operation, successful non-null version checkpoint/commit, author confirmation, candidate lineage, authorization, no failures, and no compensation. Recovery/compensation outcomes require their actual writes, failures, and recovery state. Terminal failure represents a confirmed request that made no formal change. Pre-write rejection—including missing confirmation—requires no changed path, operation, version, confirmation, authorization, or lineage, and records its rejection failure. Every receipt must carry `preference_signal_id`; it is the separately confirmed signal ID when one was appended and explicit `null` otherwise. Finalization never infers or silently creates that signal.

### Required walkthroughs

| Situation | Required result |
| --- | --- |
| Unsaved editor draft | Finalization compares current persisted revisions. The UI must not overwrite an unsaved draft; it requires the author to save/rebase/discard before a new confirmation. |
| External file edit between review and confirm | Recomputed revision/hash differs, authorization is invalidated, no formal write occurs, and the author reviews a fresh candidate. |
| SSE reconnect or duplicate submit | Same nonce and exact payload replays the durable operation/receipt; different payload with a consumed nonce is rejected and never writes twice. |
| Crash after formal write before secondary records | Startup recovery reads the workspace-change prepared/committed intent and expected hashes, roll-forwards/reconciles once, then writes the missing version/receipt or records compensation/manual intervention. |
| Successful selected or mixed finalization | Validated selected/mixed lineage, hashes, confirmation, and revisions are bound; one durable workspace operation writes all paths, one version checkpoint is associated, and one receipt closes the operation. |

## Alternatives

### Let every writer call the editor save API

Rejected. A direct editor API is author-originated and lacks candidate/confirmation/receipt semantics required for Harness-mediated output.

### Make Automation auto-write formal content

Rejected. Scheduling is not author confirmation; automation stops at pending artifacts.

### Claim the workspace write, Git commit, and receipt are ACID

Rejected. They use distinct durable stores. Explicit durable recovery and reconciliation are safer and truthful.

## Consequences

- Every Harness-mediated formal mutation can answer who confirmed which bytes, paths, candidate lineage, revision, and recovery result.
- The implementation must extend the workspace-change service with a named public finalization operation rather than bypassing its ledger/lease.
- Callers need durable nonce lookup and receipt replay; apparent latency during recovery is preferable to duplicate writes.
- P0-T06 changes no production API or workspace data.

## Migration

P0-T06 adds no implementation or migration. A v1 consumer preserves unknown records for inspection but rejects them for managed mutation. A later version must publish compatibility/read behavior, a provenance-preserving preview conversion, rollback, and an explicit reauthorization rule; old author confirmations may never be silently broadened.

## Validation

Draft 2020-12 validation accepts selected and mixed requests, compatible durable states, success receipts, and clean pre-write rejection receipts. It rejects absent request confirmation, malformed CandidateSet output tags, and contradictory operation/receipt success claims. Service validation—not JSON Schema—must compare canonical bytes/hashes, current revisions, canonical path/workspace identity, candidate eligibility, target ID/path/artifact uniqueness, nonce payload reuse, symlink containment, durable recovery, and cross-record receipt consistency. Design negatives cover duplicate targets, missing confirmation, stale artifact hash, stale revision, wrong workspace/path, and reused nonce with different payload. Focused existing workspace-change/version/session/automation tests and `git diff --check` are required.

## Supersedes

None.

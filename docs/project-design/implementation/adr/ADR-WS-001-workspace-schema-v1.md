# ADR-WS-001: Workspace Schema v1

## Status

**Accepted v1** — 2026-07-21

This ADR establishes and approves the Workspace Schema v1 implementation baseline. It is a controlled-evolution baseline, not a permanently immutable format. A future change that alters a path meaning, truth boundary, or compatibility rule requires a superseding ADR, a new schema version, explicit reader/writer compatibility, and a migration path.

## Context

The final consolidated solution is authoritative. Denova remains the sole engineering base, and files remain the creative truth source. SQLite/FTS, model memory, sessions, runs, and tool traces must never become a second creative truth source.

The current source establishes these facts:

- `internal/book/state.go` creates or uses `ideas.md`, `setting/`, `chapters/`, the selected private data root, `lore/`, `sessions/`, and `backups/`; structured lore includes `lore/items.json`. Agent checkpoint storage is separately rooted beneath the selected data root's `checkpoints/` directory.
- `internal/workspacepath/workspacepath.go` chooses `.denova` or legacy `.nova` deterministically, including target-sensitive legacy preference when both directories coexist. It does not perform a migration.
- `internal/book/files.go` provides revision hashes and lexical workspace containment for ordinary visible files. It rejects hidden paths through that API, but it does not yet provide the canonical-filesystem-path and reparse-point migration validation required by this ADR.
- `internal/book/versions/files.go` currently includes workspace files by default and excludes `.git/` plus `runs/`, `changes/`, `reviews/`, and `interactive/` beneath both `.denova/` and `.nova/`. It does **not** yet implement all v1 exclusions below.
- The Chinese directory tree in the final solution section 7.1 is a logical sketch. It does not authorize renaming current Denova paths.

This ADR decides physical layout and protection semantics only. It does not implement a schema adapter, migrations, Quality Harness domain types, a projection database, or Author Finalization.

## Decision

### 1. Authority and physical mapping

Workspace Schema v1 keeps the current physical creative paths. The logical solution maps to physical paths as follows; no v1 initializer or migration may force a rename to `作品契约/`, `大纲/`, `资料/`, `正文/`, or `工作区/`.

| Logical concept | v1 physical path | Decision |
|---|---|---|
| Work metadata and creator direction | `book.json`, `CREATOR.md`, `ideas.md` | Preserve current root records. |
| Outline, progress, character state, chapter-group plans | `setting/` | Preserve current directory. |
| Final manuscript | `chapters/` | Preserve current directory. |
| Structured lore | `.denova/lore/`, including `.denova/lore/items.json` | Preserve current data and make it version-protected creative truth. The legacy active-root equivalent is readable under `.nova/lore/`. |
| Current Profile | `.denova/profile-lock.json` | Planned authoritative file contract. |
| QualitySpec | `.denova/quality/specs/` | Planned author-controlled authoritative file records. |
| PreferenceMemory | `.denova/quality/preferences.jsonl` | Planned author-controlled authoritative append journal. |
| Candidate, review, and synchronization evidence | `.denova/quality/artifacts/` | Planned pending-review file records. |
| Author decisions | `.denova/quality/decisions/` | Planned audit-relevant file records. |
| Finalization receipts | `.denova/quality/finalizations/` | Planned audit-relevant file records. |
| Migration receipts | `.denova/quality/migration-receipts/` | Planned audit-relevant file records. |
| Harness resumable state | `.denova/quality/runs/` | Planned runtime/recovery records, never creative truth. |
| Search projection and caches | `.denova/index.db`, `.denova/cache/`, `.denova/quality/projections/` | Planned rebuildable projections, never truth. |

`.denova/quality/` is the only canonical location for new Quality Harness contracts. The product may open and edit a legacy `.nova` project without moving it, but it must not create a parallel `.nova/quality/` Harness truth tree. Enabling a v1 Harness mutation on a legacy-only project requires the explicit controlled migration described below.

### 2. Exactly-one category classifier

Each path recognized by v1 has exactly one category. Classification uses the first matching row below; the rows are deliberately ordered from specific to fallback. `${dataRoot}` expands to `.denova` and `.nova` **only** for genuine current/legacy paths already selected through `workspacepath`; it is never an alias for new v1 paths. Every new v1 marker, Profile, Quality, Harness runtime, and projection path is the literal `.denova/...` path shown below.

| Priority and path | Category | Truth authority | Manual-edit policy | Rebuildability / recoverability | Workspace version | Invalidation behavior | Writer boundary |
|---|---|---|---|---|---|---|---|
| 1. `.denova/quality/artifacts/**` | Pending-review Artifact area | File is the truth for that candidate/review/sync artifact, but never formal creative truth | Allowed; a changed hash creates a new revision and invalidates dependent decisions/runs | Not assumed rebuildable; preserve for audit | **Include all records** | Invalidate dependent decisions, projections, context packs, and runs; keep the edited artifact | Harness/Skill/Agent may create; only Author Finalization may promote selected effects to formal files |
| 1. `.denova/quality/decisions/**` | Pending-review Artifact area (audit) | File is the audit source for an explicit author decision | Filesystem edits are preserved but mark the decision tampered/invalid until explicitly revalidated | Not rebuildable from model output | **Include all records** | Invalidate dependent finalization and preference signals; never block open/edit | Decision service writes only from explicit author action; Agent/Automation cannot author a decision |
| 1. `.denova/quality/finalizations/**` | Pending-review Artifact area (audit) | File is the finalization outcome/recovery receipt, not a substitute for formal files or Git history | Filesystem edits are preserved but invalidate receipt trust | Not rebuildable; recovery may roll forward or compensate according to ADR-AF-001 | **Include all records** | Mark transaction outcome `needs_recovery`; never infer success from a changed receipt | Finalization service only; no Agent/Automation direct write |
| 1. `.denova/quality/migration-receipts/**` | Pending-review Artifact area (audit) | File is the durable migration outcome record | Filesystem edits are preserved but invalidate receipt trust | Not rebuildable; backup and state record support recovery | **Include all records** | Mark migration audit untrusted and require explicit recovery review | Schema migration service only |
| 2. `${dataRoot}/runs/**`, `${dataRoot}/checkpoints/**`, `${dataRoot}/sessions/**` | Runtime/recovery area | Existing runtime metadata only | Not a supported authoring surface; external edits may make a run non-resumable | Recoverable from checkpoints or safely rerunnable; checkpoints are **not** rebuildable projections | Exclude | Invalidate only the affected run/resume chain; formal files remain openable | Agent/session runtime only |
| 2. `.denova/quality/runs/**` | Runtime/recovery area | New Harness runtime metadata only | Not a supported authoring surface | Recoverable from checkpoints or safely rerunnable; not a projection | Exclude | Invalidate only the affected Harness run/resume chain | Harness runtime only |
| 2. `${dataRoot}/changes/**`, `${dataRoot}/reviews/**`, `${dataRoot}/interactive/**` | Runtime/recovery area | Existing Change Review/game runtime records, not formal Harness review evidence | Existing subsystem policy; an edit never becomes a Harness decision | Recoverable according to the existing subsystem; not projection truth | Exclude, preserving current behavior for both roots | Affects only the owning legacy/current subsystem | Existing workspacechange/documentreview/interactive services only; Harness evidence must use `quality/artifacts/` |
| 2. `${dataRoot}/backups/**`, `${dataRoot}/messages/**`, `${dataRoot}/automations/inbox.json` | Runtime/recovery area | Recovery copy or UI/trigger state only | Not an authoring surface | Recoverable, replaceable, or safely discardable according to the owning subsystem | Exclude | Owning subsystem may reset/recover; never changes creative truth | Owning subsystem only |
| 2. `.denova-migration/**`, `.nova-migration/**`, `.denova-migrate-*.tmp`, `.nova-migrate-*.tmp` | Runtime/recovery area | Migration intent, stage, and backup data | Not an authoring surface | Used for idempotent resume or rollback; not a projection | Exclude | Incomplete state selects resume/rollback; never blocks safe read-only open | Schema migration service only |
| 2. `.git/**` | Runtime/recovery area | Version history managed by `book/versions`, not a workspace file contract | Only version service/product workflow | Recovery history, not derived from runtime DB | Exclude from snapshots of themselves | Git failure is handled as a separate durable outcome | Version service only |
| 3. `.denova/index.db`, `.denova/index.db-*`, `.denova/cache/**`, `.denova/quality/projections/**` | Rebuildable projection area | Never authoritative | No; quarantine or delete corrupted projection | Fully rebuildable from included authoritative and Artifact records | Exclude | Loss/corruption marks projection stale and triggers rebuild; must not block opening/editing | Projection service only; never write back into truth files |
| 4. Root visible files/directories, including `book.json`, `CREATOR.md`, `ideas.md`, `setting/**`, and `chapters/**` | Formal/authoritative area | Author-controlled creative or project truth | Allowed | Not rebuildable; protect with backup and versions | Include | Hash change invalidates projections and downstream artifacts/runs; preserve edit and keep project open | Author editor or explicit Author Finalization; Agent/Automation only propose Artifacts |
| 4. `.denova/workspace-schema.json`, `${dataRoot}/lore/**`, `.denova/profile-lock.json`, `.denova/quality/specs/**`, `.denova/quality/preferences.jsonl`, `${dataRoot}/chapter_statuses.json` | Formal/authoritative area | Author-controlled contract or structured creative truth | Allowed; managed records must still validate before an automated writer relies on them | Not rebuildable | Include | Preserve changed file; invalidate dependent projections/artifacts/runs and require explicit regeneration/author decision | Dedicated author-facing service; PreferenceMemory accepts only explicit author-confirmed signals; model changes remain Artifacts |
| 4. `${dataRoot}/skills/**`, `${dataRoot}/styles/**`, `${dataRoot}/image-presets/**`, `${dataRoot}/story-tellers/**`, `${dataRoot}/story-directors/**`, `${dataRoot}/story-director-modules/**`, `${dataRoot}/automations/tasks.json` | Formal/authoritative area | Author-controlled workspace configuration shared by writing/game where applicable; not manuscript fact | Allowed | Not assumed rebuildable | Include | Invalidate consumers that reference the changed hash/version | Existing dedicated configuration libraries; no projection reverse-write |
| 5. `.nova/workspace-schema.json`, `.nova/profile-lock.json`, `.nova/quality/**`, `.nova/index.db`, `.nova/index.db-*`, `.nova/cache/**` | Formal/authoritative area (protected read-only unknown input) | **Never v1 authority**; bytes are preserved only for reconciliation/migration | Read-only to the v1 adapter; preserve any manual bytes without trusting them | Unknown; never assume rebuildable | Include by default | Mark legacy v1-name conflict and prohibit v1 mutation until explicit reconciliation/migration | No v1 writer may create, update, promote, or project from these paths |
| 6. Any other workspace path not matched above | Formal/authoritative area (protected unknown) | Unknown file record is preserved but is not automatically injected or treated as creative fact | Preserve; do not silently rewrite | Unknown, therefore never assume rebuildable | Include by default | Mark unknown feature/path and deny automated mutation until classified | Existing owning feature only; v1 adapter must not claim ownership |

This default-protect rule prevents DATA-007 data loss. A new runtime or projection path must be added to an explicit exclusion before it is used; merely placing a file under `.denova/` does not make it disposable.

### 3. Version include/exclude contract

Workspace versions use a default-include policy and an exact exclusion list.

**Included:**

- all formal/authoritative rows above, including `.denova/lore/items.json`, Profile, QualitySpec, PreferenceMemory, and author-controlled writing/game configuration;
- every file under `.denova/quality/artifacts/`, `.denova/quality/decisions/`, `.denova/quality/finalizations/`, and `.denova/quality/migration-receipts/`;
- protected unknown files until a superseding ADR classifies them.

**Excluded under both `.denova/` and `.nova/` for genuine current/legacy paths:**

- `runs/**`, `checkpoints/**`, `sessions/**`, `backups/**`, `messages/**`;
- `changes/**`, `reviews/**`, and `interactive/**` (the current exact exclusions remain unchanged in meaning);
- `automations/inbox.json`;
- migration stage/state/backup paths listed in the classifier;
- `.git/**`.

**Excluded only under canonical `.denova/` for new v1 paths:**

- `quality/runs/**`;
- `index.db`, `index.db-*`, `cache/**`, and `quality/projections/**`.

The matching `.nova/workspace-schema.json`, `.nova/profile-lock.json`, `.nova/quality/**`, `.nova/index.db`, `.nova/index.db-*`, and `.nova/cache/**` names are not v1 contracts or disposable v1 data. They follow the protected read-only unknown-input row and remain included until reconciliation/migration.

An exclusion never matches a similarly named path elsewhere. In particular, excluding `reviews/` must not exclude `quality/artifacts/reviews/`, and excluding `quality/runs/` must not exclude `quality/artifacts/`. Restore must preserve excluded runtime/projection files already on disk and must restore/delete included files according to the selected version.

The current `versions/files.go` already implements only `.git`, `runs`, `changes`, `reviews`, and `interactive` exclusions. Adding the remaining exact exclusions and positive tests is a future P1-T02/P1-T07 implementation requirement, not current behavior.

### 4. Manual edits and invalidation

Manual edits are valid and must remain possible. On open, file watch, or before any automated mutation, the implementation must compare the recorded content hash/revision with current bytes.

1. Preserve the edited bytes as the current file truth; never overwrite them from an index, run, session, or stale Artifact.
2. Keep the project open and editable even if validation, indexing, or migration metadata is damaged.
3. Mark projections stale and invalidate every downstream Artifact, decision, context pack, run, or pending finalization that depends on the old hash.
4. Keep invalidated records for audit. Regeneration requires an explicit action, and any derived formal-file update still requires an explicit author decision/finalization.
5. Rebuild only projections. Runtime checkpoints are recovered or safely rerun; they must not be described as rebuildable from creative truth.

### 5. Schema and feature compatibility

The authoritative marker is `.denova/workspace-schema.json` and is included in workspace versions.

| Field | v1 value / rule |
|---|---|
| `schema_version` | Integer `1`. |
| `reader.min_schema_version` | `1`. |
| `reader.max_schema_version` | `1` for the first implementation. |
| `reader.min_denova_version` | `1.0.0`; older Denova versions are not schema-v1-aware readers. |
| `writer.schema_version` | `1`. |
| `writer.min_denova_version` | `1.0.0`; an older application must not mutate v1-managed paths. |
| `writer.compatibility_range` | `>=1.0.0 <2.0.0`; this is the allowed Denova application-version range for schema-v1 writers. |
| `writer.version` | Exact SemVer of the Denova application that last mutated a v1 contract. |
| `features` | Map from stable feature ID to `{version, required}`. Feature versions evolve independently from the workspace schema when their physical/truth boundary is unchanged. |
| `migration.state` | One of `not_required`, `previewed`, `validated`, `backed_up`, `staged`, `switch_pending`, `switched`, `verifying`, `completed`, `rollback_pending`, `rolled_back`, `needs_recovery`. |

Compatibility behavior is mandatory:

- Denova application versions are compared as SemVer 2.0.0 without a leading `v`; build metadata does not affect ordering. A missing or unparsable application version is unknown and therefore read-only for v1-managed paths.
- The writer range is a schema-v1 contract implemented by the local reader, not a writer's self-asserted permission. The local reader compares the recorded `writer.version` against its own supported range and treats an absent, widened, or conflicting marker range as unknown/read-only.
- A running Denova version below `reader.min_denova_version` is not a schema-aware reader. Version `1.0.0` and later may perform the guarded read/open behavior below, subject to schema/features.
- A running Denova version below `writer.min_denova_version` or outside `writer.compatibility_range` may read understood files but must not mutate v1-managed paths. A newer writer within the declared `1.x` range is compatible only when the schema and every required feature are also supported.
- Missing marker means an unversioned current workspace or a legacy workspace, not schema v1. Existing formal files remain safely openable/editable through current behavior; v1 Harness mutation requires explicit adoption/migration.
- Supported schema and supported required features allow read/write after validation.
- A supported schema with an unknown optional feature allows safe open; the unknown feature is preserved and not mutated.
- An unknown required feature, a schema newer than `reader.max_schema_version`, a recorded `writer.version` outside the local reader's supported writer range, or an otherwise unknown/newer writer allows best-effort safe read/open/export of understood files but prohibits mutation of v1-managed paths, migration, finalization, and projection write-back.
- An older supported schema may be opened read-only until a controlled migration completes. No writer may silently downgrade schema or feature versions.

The UI/API must report the blocking schema/feature/writer value in Chinese and English; compatibility failure must not be presented as project corruption.

### 6. Path contract and filesystem safety

- All paths stored in contracts are workspace-relative, use `/`, and never contain an absolute path, drive prefix, UNC prefix, empty segment, `.` segment, `..` segment, or NUL.
- Chinese names, spaces, and Unicode are supported. New stored path strings use NFC. Existing non-NFC names are not silently renamed; normalization-equivalent collisions are detected and require an explicit migration choice.
- Original case is preserved. Comparison follows the mounted filesystem for containment, while preflight also detects Unicode/case-fold collisions that would be ambiguous on Windows or during cross-platform migration. Case-only renames on insensitive filesystems use an explicit temporary name.
- `\` received from a Windows API is normalized to `/` only after rejecting drive/UNC/absolute forms. New Windows path segments reject trailing dots/spaces, alternate-data-stream syntax, and reserved device names such as `CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, and `LPT1`–`LPT9`, including names with extensions.
- Symlinks, junctions, mount points, and other reparse points are resolved before read/write/migration. The canonical target of every source, destination, stage, and backup must remain inside the canonical workspace boundary unless an explicit external backup destination was separately authorized. Escapes and loops are conflicts, not paths to follow.
- Long paths are supported to the extent of the host filesystem and Go runtime. The adapter must preflight every source/destination, use platform long-path support where available, never truncate a name, and fail safely before switching if the destination cannot represent the path.

The current `book.SafePath` lexical check and current version collector's file-symlink skip are useful existing guards but do not satisfy this full migration boundary. P1-T02 must add canonical containment and reparse-point tests without weakening normal visible-file access.

### 7. `.nova` legacy-read compatibility

Current `workspacepath` selection remains the compatibility authority:

- neither root: select `.denova`;
- only `.denova`: select `.denova`;
- only `.nova`: select `.nova`;
- both: prefer `.nova` when it has meaningful workspace data and `.denova` has no meaningful data after the current ephemeral checks; otherwise prefer `.denova`. For a target-specific lookup, an existing legacy target wins when the current target is absent; when both targets exist, the legacy target wins only when it has meaningful data and the current target does not.

The v1 adapter must evaluate these existing rules, record the resolved root and relevant target resolutions, and pin one writer root for the entire operation. When both roots coexist, only the pinned active copy is truth; the inactive copy is preserved recovery input, not a second truth source. The adapter must never read formal inputs from one root while writing their v1 successors to the other without a migration receipt. If target-specific resolutions disagree, the project opens in safe compatibility mode, existing formal files remain editable where current behavior permits, and v1-managed mutation is blocked until explicit reconciliation. This prevents split-brain without pretending the current target-sensitive behavior does not exist.

Names that resemble new v1 contracts beneath `.nova/` have no v1 authority. In particular, `.nova/workspace-schema.json`, `.nova/profile-lock.json`, `.nova/quality/**`, `.nova/index.db`, `.nova/index.db-*`, and `.nova/cache/**` are preserved and versioned as read-only unknown input. The adapter must not treat them as a schema marker, Profile, Quality record, Artifact, runtime record, or projection; it requires explicit reconciliation/migration before any corresponding `.denova/...` mutation.

Opening a `.nova` workspace does not move, copy, rename, or delete it. A migration to `.denova` happens only after preview, author confirmation, backup, staged switch, verification, and an available rollback. After a successful switch, `.denova` is the single writer root; retained `.nova` bytes are a backup/recovery source and are never dual-written.

### 8. Ownership derived for later tasks

This ADR fixes paths and ownership without implementing later tasks:

- **P1-T02** owns the Workspace Schema v1 adapter, exact path classifier, compatibility guard, canonical containment, version include/exclude implementation, dry-run migration, backup, switch, receipt, resume, and rollback.
- **P1-T03** owns `.denova/index.db` and projection invalidation/rebuild. It consumes included formal/Artifact file hashes and may write only projection paths; deletion/corruption must not prevent open/edit.
- **P2-T07** consumes selected `.denova/quality/artifacts/` and explicit `.denova/quality/decisions/`, then uses the future durable Author Finalization boundary to update formal files and write `.denova/quality/finalizations/` receipts. It may not use raw runs, current `changes/`/`reviews/`, or the projection as authority.

## Alternatives

### Rename immediately to the logical Chinese tree

Rejected. It would break existing workspaces and duplicate or move current truth without an implemented migration boundary.

### Store Harness contracts in SQLite

Rejected. It creates a second truth source and makes index loss block author-controlled records.

### Put all `.denova/` data in workspace versions

Rejected. Runs, traces, checkpoints, caches, and projections create noise and unsafe restore semantics.

### Exclude all `.denova/` data from workspace versions

Rejected. It loses lore, QualitySpec, PreferenceMemory, author configuration, and audit-relevant Artifacts/receipts (DATA-007).

### Auto-copy `.nova` on first open

Rejected. It can create split-brain and violates preview/backup/rollback requirements.

## Consequences

- Existing creative paths and manual-edit workflows remain intact; no schema-v1 decision itself changes user data.
- Version snapshots grow because pending-review and audit-relevant records are protected. Raw traces and large runtime payloads must remain in excluded run storage, not be disguised as Artifacts.
- Unknown data is protected by default, so every new disposable directory requires an explicit schema/version-policy change.
- Legacy projects can open without migration, while new Harness writes wait for explicit adoption.
- P1 implementation must close known gaps in version exclusions and canonical path safety before claiming v1 write support.

## Migration

### Required protocol

Every schema adoption or migration follows this protocol, including a no-rename adoption that creates only v1 metadata:

1. **Detect and preview:** resolve current roots with `workspacepath`; produce a dry-run manifest of source/destination paths, categories, sizes, hashes, version inclusion changes, schema/features, and planned operations. Do not mutate.
2. **Validate preconditions/conflicts:** acquire the workspace writer lease; recheck base hashes; validate permissions, free space, canonical containment, reparse points, Unicode/case/reserved-name collisions, long-path support, supported reader/writer/features, and same-filesystem switch capability.
3. **Back up:** copy all affected authoritative and Artifact records to `.denova-migration/<migration-id>/backup/` with a content-hash manifest. Flush files and record a durable `backed_up` state before staging.
4. **Stage:** build each complete target namespace under `.denova-migration/<migration-id>/stage/`. A legacy-root migration stages the complete future `.denova/`; a metadata-only adoption stages the complete `workspace-schema.json` file rather than copying the live data root. Write the future marker and migration receipt into the applicable stage and validate hashes/schema. Never edit an affected live file in place.
5. **Prepare switch:** persist `switch_pending` with source root, target root, backup manifest, stage hash, and deterministic next action. Revalidate source hashes immediately before switching.
6. **Switch:** use same-filesystem rename/replace to publish one prepared namespace entry: the new `.denova/` root when that target is absent, or a staged metadata file for an in-place adoption. Each supported rename/replace is atomic only at that filesystem namespace boundary. If a future migration requires multiple namespace entries or the host cannot atomically replace an existing directory, the persisted intent sequences individually atomic entry switches and exposes `needs_recovery` until all verify; it must not claim a globally atomic switch. An existing destination is backed up/moved only according to the persisted recovery intent.
7. **Verify and receipt:** reopen through the normal reader, validate category/version manifests, and write/finalize `.denova/quality/migration-receipts/<migration-id>.json`. Mark `completed` only after verification.
8. **Recover or roll back:** on restart, use the stable migration ID, persisted state, and hashes to repeat the next incomplete step safely. Before switch, discard/rebuild stage. After switch, either finish verification (roll forward) or restore the backed-up namespace and mark `rolled_back`/`needs_recovery`. Never delete the only verified backup automatically.

Resume is idempotent: a step whose output already has the expected hash is recorded complete rather than repeated; a different hash is a conflict. Cross-filesystem moves are copy-and-verify operations followed by a same-filesystem final switch, not atomic moves. The filesystem switch and workspace Git commit are separate durable media; this ADR makes no cross-filesystem or filesystem-plus-Git database-ACID claim.

## Validation

### Design walkthrough 1: new project

1. **Detection:** neither `.denova` nor `.nova` exists, so current `workspacepath` selects `.denova`.
2. **Category mapping:** initializer creates current formal paths (`ideas.md`, `setting/`, `chapters/`, lore) and v1 metadata; quality directories are created only when needed.
3. **Outcome:** `migration.state=not_required`; there is no source namespace to migrate.
4. **Backup/switch/rollback:** no switch or backup is needed. Failure before marker publication leaves an unversioned but openable new workspace; retry is idempotent.
5. **Open/edit:** formal files open and remain manually editable; absent projection is rebuilt lazily and cannot block editing.

### Design walkthrough 2: existing `.denova` project

1. **Detection:** meaningful `.denova` data selects `.denova`; existing `ideas.md`, `setting/`, `chapters/`, and `.denova/lore/items.json` keep their paths.
2. **Category mapping:** current `runs/changes/reviews/interactive` remain runtime/recovery, not Harness evidence; lore and existing author-controlled records remain formal and versioned.
3. **Outcome:** opening requires no migration. Enabling v1 writes on a missing-marker workspace runs a controlled in-place-schema **adoption** with no creative-path rename.
4. **Backup/switch/rollback:** adoption preview backs up affected metadata/formal records, stages the complete marker file, publishes that file by same-filesystem atomic replace, and can restore the prior marker/absence. It does not replace the existing `.denova` directory or rewrite live creative files.
5. **Open/edit:** ordinary formal editing remains available before adoption; unsupported/newer metadata restricts only unsafe managed mutation.

### Design walkthrough 3: legacy project with only `.nova`

1. **Detection:** only `.nova` exists, so current `workspacepath` selects `.nova`.
2. **Category mapping:** legacy lore is formal; legacy `runs/changes/reviews/interactive` are runtime/recovery and excluded; no `.nova/quality/` tree is created.
3. **Outcome:** first open performs no migration and legacy formal editing remains available. Enabling v1 Harness mutation requires explicit migration to `.denova`.
4. **Backup/switch/rollback:** preview shows every `.nova` source and `.denova` destination; validated bytes are backed up and staged; a same-filesystem namespace switch publishes `.denova`. If verification fails, the persisted intent restores/pins `.nova` and records rollback; neither root is dual-written.
5. **Open/edit:** before migration, current legacy behavior opens the project; after successful migration, `.denova` is the sole writer root and formal files remain editable.

### Mechanical acceptance checklist

- [x] Accepted-v1 authority and controlled supersession are explicit.
- [x] Current physical creative paths are preserved.
- [x] Every v1 path has one category, authority, edit, recovery, version, invalidation, and writer rule.
- [x] QualitySpec, PreferenceMemory, Artifacts, decisions, and receipts have exact file/version policies.
- [x] Runtime checkpoints and rebuildable projections are distinguished and excluded.
- [x] Existing current/legacy `runs`, `changes`, `reviews`, and `interactive` exclusions retain their meaning.
- [x] Reader/writer/schema/feature compatibility and unknown-newer behavior are explicit.
- [x] Minimum Denova reader/writer versions, writer compatibility range, and SemVer comparison behavior are explicit.
- [x] Preview, conflict validation, backup, stage, durable switch intent, receipt, recovery, rollback, and idempotent resume are explicit without a false ACID claim.
- [x] Manual edits preserve truth and invalidate only derived consumers.
- [x] `.nova` selection, first-open, coexistence, and migration boundaries are explicit.
- [x] Unicode, spaces, case, separators, reserved names, reparse points, containment, and long paths are explicit.
- [x] New, existing-current, and legacy-only walkthroughs cover open/edit and recovery outcomes.
- [x] P1-T02, P1-T03, and P2-T07 derive paths/ownership without implementation.

The JSON example beside this ADR is a parseable fixture illustrating this accepted contract. It is not loaded as runtime truth and does not replace `.denova/workspace-schema.json` in an implemented workspace.

## Supersedes

None. This ADR refines the final consolidated solution's logical workspace sketch and ADR-002 without superseding either. Future workspace schema decisions must identify this ADR as superseded and provide compatibility and migration details.

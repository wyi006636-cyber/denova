# P1-T04 Record Repositories Validation Record

## Scope and authority

This record covers only P1-T04 on `feat/quality-harness-foundation` from the
local P1-T03 baseline. Accepted v1 `ADR-CS-001`, `ADR-RI-001`, `ADR-PM-001`,
and `ADR-WS-001`, together with their three normative Draft 2020-12 schemas,
are the implementation authority.

- CandidateSet and ReviewIssue remain pending-review Artifact truth, never
  formal manuscript, setting, or lore truth.
- PreferenceMemory is an author-controlled structured formal record. It may be
  bounded input to later suggestions, but cannot mutate formal content,
  QualitySpec, CandidateSet decisions, or invoke Author Finalization.
- This task adds no API, UI, App service, Agent, Automation, Projection,
  product Harness runtime, Author Finalization, Tauri, vector search, P1-T05,
  P1-T06, or P1-T07 integration.

## Exact contracts and decoder boundary

`internal/quality/workspace.RecordDecoder` receives all three normative schema
byte slices explicitly. Production code never resolves `docs/` through the
runtime working directory. Each exact v1 record passes duplicate-key and
single-JSON-value checks, Draft 2020-12 schema validation with asserted
formats, `DisallowUnknownFields`, and domain semantic validation.

| Contract | ADR status | top-level required | properties | `$defs` |
|---|---|---:|---:|---:|
| CandidateSet v1 | Accepted v1, 2026-07-21 | 18 | 18 | 30 |
| ReviewIssue v1 | Accepted v1, 2026-07-21 | 16 | 17 | 27 |
| PreferenceSignal v1 | Accepted v1, 2026-07-21 | 11 | 13 | 19 |

Malformed exact-v1 input returns a located `domain.ContractError`. An unknown
or newer version retains exact raw bytes in read-only mode. It cannot be
created, updated, appended after, or silently interpreted as v1.

## Package and dependency direction

- `internal/quality/domain` owns record types, exhaustive lifecycle and event
  vocabularies, byte/hash/range/lineage validation, journal graph validation,
  and deterministic preference resolution. It imports no workspace, API, UI,
  Agent, App, Projection, or Harness package.
- `internal/quality/workspace` owns schema decoding, canonical/portable path
  policy, bounded strict reads, trusted authority and reference-provider
  interfaces, repositories, CAS, locks, durable publication, and record
  migration preview.
- `internal/book/versions` remains independent of `internal/quality`; existing
  Workspace Schema v1 include/exclude rules already cover the new persistent
  records, so only exact policy tests were expanded.

No base package imports `internal/quality`, no dependency was added, and
`go.mod` / `go.sum` are unchanged from the P1-T03 baseline.

## Stable storage layout

The implementation fixes these ID-derived paths beneath the Accepted
Workspace Schema v1 roots:

| Record | Stable path |
|---|---|
| CandidateSet | `.denova/quality/artifacts/candidate-sets/<candidate_set_id>.json` |
| ReviewIssue | `.denova/quality/artifacts/review-issues/<issue_id>.json` |
| PreferenceMemory | `.denova/quality/preferences.jsonl` |
| Cross-process repository lock | `.denova/quality/runs/record-repositories.lock` |

Callers provide stable IDs, never arbitrary record paths. IDs pass the existing
portable Windows-name contract and collision detector; all file operations are
root-contained and reject reparses, non-regular entries, identity changes, and
bounded-read overflow.

## Contract and authority matrix

| Record | Required validation | Trusted persistence authority | Forbidden effect |
|---|---|---|---|
| CandidateSet | complete canonical binding and Skill hashes; exact comparison coverage; closed lifecycle; author decision; mixed segment recomposition; receipt-bound finalization handoff | injected trusted authority verifier plus writer lease; author verifier is mandatory for decision-bearing mutations | write formal files, manufacture a Finalization receipt, infer author selection |
| ReviewIssue | exact reviewed/revised bytes per transition; portable location/ranges; anchor/quote/evidence hashes; closed severity/cause/layer/Capability/status vocabularies; ordered re-verification rounds | injected trusted authority verifier plus writer lease; dismissal cannot be self-attested by JSON actor text | edit formal content, close on revision generation, hide failed verification |
| PreferenceSignal | one of seven explicit events; exact source and confirmation bytes; author/scope identity; append-only correction/revocation graph; deterministic scope/strength/time/ID resolution | every append passes the injected trusted author-action verifier; persisted `actor_type` is evidence, not authority | telemetry/model/reviewer/Automation memory, physical history deletion, automatic formal mutation |

## Repository, CAS, and durability boundary

All managed writes first pass the Workspace Schema v1 compatibility guard with
the caller's real application version and supported capability ranges. Missing
markers, `dev`, pre-1.0 writers, unknown required features, legacy-only `.nova`,
and split-root workspaces remain read-only. The guard is repeated after the
writer lease and cross-process lock are acquired.

CandidateSet and ReviewIssue creation is create-if-absent. Updates bind the
expected raw SHA-256, workspace revision, strict current inode, immutable
identity, and exact prior history prefix. CandidateSet permits only the single
new `finalization_receipt` binding required by a valid selected/mixed to
finalized transition; finalized receipt identity is then immutable, and
archived sets are terminal.

Every record is written as a complete same-directory sibling, file-synced,
then published. Updates use an atomic exchange on Darwin/Linux and Windows
`ReplaceFileW` with a backup entry. Windows pins and identity-checks every
directory component from the already-open `os.Root` without delete sharing
before constructing the sibling paths required by that API; a junction or
renamed-directory substitution fails before publication. The displaced entry
is verified against the expected inode and raw hash after the atomic namespace
operation, closing the verify-then-replace race. A mismatch is atomically
restored. Successful updates sync the parent before deleting the displaced
bytes, then sync the deletion. Rollback first restores and syncs the complete
old target while the complete replacement still exists, then removes and syncs
the replacement sibling.

Create publication verifies the prepared inode/hash before and after its
create-if-absent link. If the prepared sibling is substituted, the new target
is withdrawn, both the intended and conflicting complete bytes are retained as
typed recovery siblings, and the create returns a conflict. `ListRecovery`
reports target path, recovery kind, exact raw hash, and size without choosing
which bytes are authoritative; ordinary record listing ignores only valid
repository-owned recovery names.

On Windows, post-call reconciliation recognizes success, no-op, partial-target
absence, concurrent target substitution, and staged-path substitution by file
identity. It restores the bytes displaced from the authoritative target,
moves conflicting published bytes into a typed sibling, and persists another
complete intended-byte sibling before returning `recovery required`; it never
selects a recovery sibling as truth automatically.

Preference append never uses bare `O_APPEND`: it bounded-reads the entire prior
journal, requires a complete trailing newline, verifies expected prior raw
hash, revalidates all managed records and the new reference set under the lock,
appends one complete line in memory, then applies the same sibling/CAS/durability
protocol. Exact old journal bytes are always the prefix of the new file.

## Migration and recovery matrix

| Input/failure | Read/preview result | Managed write/recovery behavior |
|---|---|---|
| exact v1 | managed; migration `no_op` / `exact_v1` | execution performs zero writes |
| unknown/newer version | exact raw bytes, read-only; `unavailable` / `no_accepted_migration_path` | execution rejects; no v2 or enum mapping is invented |
| malformed v1 or duplicate JSON key | located hard error | no target mutation |
| partial Preference JSONL tail | `ErrPreferencePartialTail` | preserve journal bytes; no append |
| duplicate signal ID, graph cycle, cross-author target, stale hash | hard semantic error | preserve journal bytes; no append |
| stale raw hash/revision or external inode substitution | CAS conflict | displaced external bytes are restored; repository bytes are not overwritten |
| create staged-inode substitution | conflict plus recovery-required error | target is withdrawn; complete intended and conflicting bytes remain inspectable |
| lock contention | non-blocking lock error | no record target mutation |
| write/file-sync/link/atomic-replace failure | operation error | incomplete sibling removed; existing target preserved |
| update parent-sync failure | durability error | old target restored, replacement removed, rollback parent synced when available |
| create parent-sync failure after visibility | durability error | only a complete file is visible; restart/read can inspect exact bytes |
| rollback itself cannot become durable | joined durability/recovery-required error | complete target/displaced bytes are retained and enumerated for explicit recovery; no guessed authority |

Recovery never promotes SQLite, Projection, run logs, model output, reviewer
scores, or reconstructed guesses into record or formal truth.

## Workspace version policy and write audit

Workspace Schema v1 version snapshots include
`.denova/quality/artifacts/**` and `.denova/quality/preferences.jsonl`. They
continue to exclude `.denova/quality/runs/**`, `.denova/index.db*`, cache, and
`.denova/quality/projections/**`; restore preserves every live excluded runtime
or Projection entry.

The repository write audit observes only the schema marker, the two fixed
Artifact subtrees, the Preference journal, and the excluded repository lock.
No repository path reaches `chapters/**`, setting/lore, quality decisions,
finalizations, runs other than the lock, Projection, SQLite, or Git.

## Review and final gate record

Independent final review on the stable implementation reported zero Critical
and zero Important findings. No API/UI/product integration was reviewed or
claimed.

| Gate | Result |
|---|---|
| `go test ./internal/quality/domain ./internal/quality/workspace ./internal/workspacepath ./internal/book/versions -count=1` | exit 0 |
| same package set under `go test -json` | exit 0; skipped 0; 462 leaf tests; slowest leaf 0.55s |
| same package set under `go test -race -count=1` | exit 0 |
| `CGO_ENABLED=0` related-package test compilation plus `cmd/denova` build: darwin/arm64 | exit 0 |
| same matrix: darwin/amd64 | exit 0 |
| same matrix: linux/arm64 | exit 0 |
| same matrix: linux/amd64 | exit 0 |
| same matrix: windows/amd64 | exit 0 |
| `go test ./... -count=1` | exit 0 |
| `go vet ./...` | exit 0 |
| `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` | shell exit 1; only existing reachable `GO-2026-5970` in `golang.org/x/text v0.38.0`; no new reachable vulnerability |
| Node 22 `pnpm --dir web test` | exit 0; 125 files / 651 tests passed |
| Node 22 `pnpm --dir web check:i18n` | exit 0; 2,987 keys aligned |
| Node 22 `pnpm --dir web build` | exit 0 |
| `./scripts/build.sh` | exit 0 |
| `go mod tidy -diff` | exit 0 |
| `git diff --check` | exit 0 |

`go.mod` and `go.sum` compare byte-for-byte equal to P1-T03 commit
`8e3293a4e3a367fa6db613094a3f9da8519f9dfe`. The accepted ADR/schema count
preflight remains unchanged. This validation record establishes a local
engineering boundary only; it does not imply push, merge, release, or product
acceptance.

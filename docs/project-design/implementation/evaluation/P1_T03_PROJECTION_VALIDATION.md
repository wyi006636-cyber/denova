# P1-T03 Projection v1 Validation Record

## Scope and authority

This record covers only P1-T03 on `feat/quality-harness-foundation`. Accepted
`ADR-PROJECTION-001` and `ADR-WS-001` are the implementation authority.

- Formal workspace files and explicitly approved Artifact paths are the only
  inputs. `.denova/index.db` is a disposable read model and never writes facts
  back to either source class.
- The implementation is isolated in `internal/quality/projection`, one minimal
  read-only source-snapshot boundary in `internal/quality/workspace`, and the
  offline `denova reindex` command.
- P1-T04, P1-T07, API, UI, App service, Agent, Automation, product Harness,
  Author Finalization, Tauri, vector search, custom tokenizers, and automatic
  product-startup rebuild are not included.

## Exact dependency decision

The dependency probe ran in the repository-external module
`/private/tmp/denova-projection-probe.lOihna` before `go.mod` or `go.sum` was
changed.

| Module | Exact version | Upstream tag commit | Module sum | License | Go directive |
|---|---|---|---|---|---|
| `modernc.org/sqlite` | `v1.54.0` | `693ff386c68d2964fe40d2f3c0b6cd06630660c4` | `h1:JCxR4qwkJvOaqAoYcgDoO25Nc+ROg6EJ2LfBVzdrgog=` | BSD-3-Clause | `go 1.25.0` |
| `modernc.org/libc` | `v1.74.1` | `7f6c23a10979fbb9e6f024c4735f51ead22f40b3` | `h1:bdR4VTKFMC4966QSNZ05XLGI/VwzVa2kTUX51Dm0riQ=` | BSD-3-Clause | `go 1.25.0` |

The selected sqlite tag's own `go.mod` directly requires
`modernc.org/libc v1.74.1` and the repository's existing
`golang.org/x/sys v0.46.0`. `go mod tidy` moved `x/sys v0.46.0` from indirect
to direct because existing Windows ACL code already imports it; no `x/sys`
version changed. `golang.org/x/text` remains exactly `v0.38.0`.

`CGO_ENABLED=0 go list -deps` reported no compiled package with `CgoFiles`.
The external dependency probe's `govulncheck` exited 0 with no vulnerability.

## Runtime SQLite and FTS5 evidence

The repository-external runtime probe was rerun with Go 1.26.5 and
`CGO_ENABLED=0`; it exited 0 and reported:

- `sqlite_version() = 3.53.3`;
- successful FTS5 external-content virtual-table and trigram creation;
- English trigram lookup `quick -> doc-en`;
- Chinese trigram lookup `小说创 -> doc-zh`;
- successful exact
  `INSERT INTO documents_fts(documents_fts, rank) VALUES ('integrity-check', 1)`.

Recorded `PRAGMA compile_options`:

```text
ATOMIC_INTRINSICS=1,COMPILER=clang-21.0.0,DEFAULT_AUTOVACUUM,DEFAULT_CACHE_SIZE=-2000,DEFAULT_FILE_FORMAT=4,DEFAULT_JOURNAL_SIZE_LIMIT=-1,DEFAULT_MEMSTATUS=0,DEFAULT_MMAP_SIZE=0,DEFAULT_PAGE_SIZE=4096,DEFAULT_PCACHE_INITSZ=20,DEFAULT_RECURSIVE_TRIGGERS,DEFAULT_SECTOR_SIZE=4096,DEFAULT_SYNCHRONOUS=2,DEFAULT_WAL_AUTOCHECKPOINT=1000,DEFAULT_WAL_SYNCHRONOUS=2,DEFAULT_WORKER_THREADS=0,DIRECT_OVERFLOW_READ,DISABLE_INTRINSIC,ENABLE_COLUMN_METADATA,ENABLE_DBPAGE_VTAB,ENABLE_DBSTAT_VTAB,ENABLE_FTS5,ENABLE_GEOPOLY,ENABLE_MATH_FUNCTIONS,ENABLE_MEMORY_MANAGEMENT,ENABLE_OFFSET_SQL_FUNC,ENABLE_PREUPDATE_HOOK,ENABLE_RBU,ENABLE_RTREE,ENABLE_SESSION,ENABLE_SNAPSHOT,ENABLE_STAT4,ENABLE_UNLOCK_NOTIFY,LIKE_DOESNT_MATCH_BLOBS,MALLOC_SOFT_LIMIT=1024,MAX_ATTACHED=10,MAX_COLUMN=2000,MAX_COMPOUND_SELECT=500,MAX_DEFAULT_PAGE_SIZE=8192,MAX_EXPR_DEPTH=1000,MAX_FUNCTION_ARG=1000,MAX_LENGTH=1000000000,MAX_LIKE_PATTERN_LENGTH=50000,MAX_MMAP_SIZE=0x7fff0000,MAX_PAGE_COUNT=0xfffffffe,MAX_PAGE_SIZE=65536,MAX_SQL_LENGTH=1000000000,MAX_TRIGGER_DEPTH=1000,MAX_VARIABLE_NUMBER=32766,MAX_VDBE_OP=250000000,MAX_WORKER_THREADS=8,MUTEX_NOOP,SOUNDEX,SYSTEM_MALLOC,TEMP_STORE=1,THREADSAFE=1
```

## Schema and query contract

Projection schema v1 records the schema version, Denova build identity,
sqlite/libc module identities, runtime SQLite version, source snapshot hash,
and document count. `source_documents` stores stable public document ID,
canonical workspace-relative path, byte revision hash, profile, kind, and
content. Its SQLite integer `rowid` is deliberately separate from the public
ID.

`source_documents_fts` is an FTS5 external-content table with
`content='source_documents'`, `content_rowid='rowid'`, and
`tokenize='trigram'`. Exact insert, update, and delete triggers keep the content
table and FTS index in one writer transaction. Rebuild completion, every new
sibling activation, and every corruption/inconsistency inspection execute the
exact external-content `rank=1` check. Closed siblings are reopened and the
schema SQL, metadata, quick check, row count, stable IDs, revisions, configured
storage bounds, snapshot fingerprint, complete source rows, and FTS integrity
are all revalidated before source CAS. Persisted file count, canonical-path
bytes, largest content, and total content are rechecked against the configured
source bounds before a database can be opened.

Queries of three or more Unicode characters use a quoted literal trigram
`MATCH`. Queries below three Unicode characters use deterministic exact
`instr()` scanning over path and content, so trigram's short-term limitation
cannot create false empty results. Both paths are bounded and deterministic.

## Source snapshot and rebuild protocol

The source reader canonicalizes and pins the workspace, walks through
`os.Root`, reads directory entries in fixed batches, rejects
reparses/non-regular nodes and identity changes, and accepts only bounded UTF-8
text without NUL. Defaults are 100,000 source files, 200,000 visited entries,
64 MiB of visited relative-path bytes, 16 MiB per source file, and 512 MiB of
source content; callers may configure lower finite limits. Runtime, migration,
projection, cache, protected unknown, and `.nova` v1-looking paths are
excluded. Review Artifacts require an exact caller-provided allowlist, empty by
default.

Each document ID is `doc-` plus SHA-256 of the canonical relative path; each
revision is SHA-256 of exact source bytes. The snapshot fingerprint is a
length-delimited SHA-256 over the complete sorted identity/path/revision/
profile/kind/size tuple.

Rebuild order is:

1. require Workspace Schema v1 managed-mutation compatibility using the real
   running Denova build version and the exact `quality_harness` /
   `fts_projection` `>=1.0.0 <2.0.0` feature ranges;
2. serialize rebuilds in-process and through the persistent, cross-process
   `.denova/index.db-rebuild.lock`, then take the bounded source snapshot;
3. classify any current Projection without blocking workspace read/edit;
4. discard only strictly owned orphan siblings and build a uniquely named
   fresh database beside `index.db` with one connection and one transaction;
5. close the builder, sync and independently reopen/revalidate the complete
   sibling, close validation, and sync the file again;
6. rebuild the source snapshot and require an exact fingerprint match;
7. when safe and required, quarantine an unsafe prior database and sidecars by
   same-directory rename, never by ordinary file copy;
8. atomically replace `index.db` (`os.Rename` on non-Windows,
   `MoveFileEx(REPLACE_EXISTING|WRITE_THROUGH)` on Windows), verify identity,
   recheck journal/WAL/SHM boundaries, and sync the `.denova` parent directory.

A diagnostic fault after a clean visible activation reports that boundary and
leaves a complete validated database recognizable on restart. If a sidecar or
identity hazard appears, the newly visible main is unpublished and durably
quarantined first; a non-regular sidecar or later partial diagnostic failure is
reported and retained without rolling the main back into visibility. Unix
quarantine renames are followed by directory sync. Windows quarantine is
root-handle-relative (`NtSetInformationFile`) and flushes the renamed file
handle because ordinary Windows directory sync is unavailable. Failure before
visibility removes the owned sibling; restart deterministically rebuilds from
source truth.

## Availability and recovery matrix

| Condition | State/reason | Database handling | Workspace authority/read-edit behavior |
|---|---|---|---|
| Missing | unavailable / `missing` | full rebuild | formal files remain readable/editable |
| SQLite exclusive lock | unavailable / `locked` | retain, do not replace | formal files remain readable/editable |
| Random corruption/open failure | unavailable / `corrupt` or `open_failed` | rename for diagnostics when safe, rebuild from files | no SQLite salvage into truth |
| Unknown newer schema | unavailable / `schema_newer` | rename for diagnostics, rebuild schema v1 | no reverse migration into truth |
| Metadata/schema/path/bounds/hash mismatch | unavailable / `identity_mismatch` or `integrity_failed` | isolate when safe, rebuild | no derived reverse write |
| External-content mismatch | unavailable / `integrity_failed` | exact `rank=1`, isolate, rebuild | source bytes unchanged |
| Author edits a source after indexing | stale / `source_changed` | valid stale DB may be replaced without diagnostic copy | new author bytes remain truth |
| Source changes during rebuild | rebuild fails with source-CAS error | prior valid DB retained; sibling discarded | author bytes preserved exactly |
| Crash/fault before visible activation | rebuild failure, not activated | sibling discarded on failure/restart | no partial authority |
| Fault after visible activation/before parent sync | rebuild reports activated with durability uncertainty | complete new DB remains recognizable | restart validates or rebuilds from truth |
| Journal/WAL/SHM appears at activation | unavailable / `integrity_failed` | main and regular sidecars are durably renamed; non-regular/partial failure retains a diagnostic error but never republishes the main | formal files remain the only truth |
| Missing, invalid, or pre-1.0 running version / required feature mismatch | unavailable / `workspace_incompatible` | no Projection mutation | safe workspace read/open behavior remains independent |

No recovery path copies a live SQLite file, imports `.recover` output into
formal files, or treats query results as an authoring source.

## Deterministic fault evidence

Tests inject failures without sleeps, permission damage, disk filling, or
daemons at: schema creation, data write, before/after integrity check,
connection close, source recheck, visible activation, and before parent sync.
They also mutate the closed sibling to prove schema/data/hash/FTS revalidation,
substitute the pinned reader path, create a sidecar immediately after namespace
replacement, and inject a partial quarantine rename failure. Every test
workspace is created with `t.TempDir()`.

## Five-platform CGO-free compile matrix

The formal compile-only gate uses `go test -c` for
`internal/quality/projection` and `go build` for `cmd/denova`. All commands set
`CGO_ENABLED=0`; artifacts were written outside the repository to
`/private/tmp/denova-p1t03-matrix.S5288V`.

| Target | Projection test compile | denova build |
|---|---:|---:|
| darwin/arm64 | 0 | 0 |
| darwin/amd64 | 0 | 0 |
| linux/arm64 | 0 | 0 |
| linux/amd64 | 0 | 0 |
| windows/amd64 | 0 | 0 |

## Known vulnerability baseline

Before this task, repository `govulncheck` exited 1 only for reachable
`GO-2026-5970` in the pinned `golang.org/x/text v0.38.0`, through
`internal/quality/workspace/path_contract.go`. The task explicitly forbids an
`x/text` upgrade. The external sqlite/libc probe exited 0. The final repository
scan again reports exactly that one reachable vulnerability and no new
sqlite/libc finding.

## Final gate record

The reviewed implementation had no remaining Critical or Important findings.
The final commands below ran from the repository root unless an evidence path
states otherwise.

| Gate | Exit | Result / evidence |
|---|---:|---|
| `go test ./internal/quality/projection ./internal/quality/workspace ./internal/workspacepath ./internal/book/versions -count=1` | 0 | all four package groups passed |
| requested-package `go test -json` plus offline CLI package | 0 | 79 new leaf tests checked; 0 over 1.0 s; maximum 0.49 s (`TestFaultMatrixNeverPublishesPartialAuthorityAndRestartIsDeterministic`); `/private/tmp/denova-p1t03-test-json.2RDXFY` |
| `go test -race ./internal/quality/projection ./internal/quality/workspace ./internal/workspacepath ./internal/book/versions -count=1` | 0 | `/private/tmp/denova-p1t03-race.tsncz2` |
| five-platform `CGO_ENABLED=0` Projection test compile / `denova` build | 0 / 0 each | darwin/arm64, darwin/amd64, linux/arm64, linux/amd64, windows/amd64; `/private/tmp/denova-p1t03-matrix.S5288V` |
| repository-external runtime FTS5 probe | 0 | SQLite 3.53.3; `ENABLE_FTS5`; virtual table, English trigram, Chinese trigram, and external-content `rank=1` all PASS; `/private/tmp/denova-p1t03-runtime-probe.eKyB7k` |
| actual schema-aware `denova reindex --workspace <temp-workspace>` | 0 | build version `1.6.2`; 2 documents; bilingual summary; `.denova/index.db` 880640 bytes; `/private/tmp/denova-p1t03-reindex.o4tJkd` |
| `go test ./... -count=1` | 0 | `/private/tmp/denova-p1t03-go-all.iYoySo` |
| `go vet ./...` | 0 | `/private/tmp/denova-p1t03-vet-final.Tmdu0x` |
| `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` | 1 | expected existing `GO-2026-5970` only; `/private/tmp/denova-p1t03-govulncheck.4MTm2k` |
| Node 22 `pnpm --dir web test` | 0 | Node v22.22.2; 125 files / 651 tests passed; `/private/tmp/denova-p1t03-web-test-final.nWQDqY` |
| Node 22 `pnpm --dir web check:i18n` | 0 | 2987 keys aligned; `/private/tmp/denova-p1t03-web-i18n.e2U0ek` |
| Node 22 `pnpm --dir web build` | 0 | TypeScript and Vite production build passed; `/private/tmp/denova-p1t03-web-build.BqcYPz` |
| `./scripts/build.sh` | 0 | frontend, embedded Go backend/updater, skills, and config assembled; `/private/tmp/denova-p1t03-build-sh.xaD914` |
| `go mod tidy -diff` | 0 | empty diff; `/private/tmp/denova-p1t03-tidy-diff.Lhzp5s` |
| `git diff --check` | 0 | no whitespace errors; `/private/tmp/denova-p1t03-git-diff-check.Dm3x53` |

The manual CLI proof deliberately injected a valid schema-aware 1.x
`buildinfo.Version`. A `dev`, missing, or current pre-1.0 product version is
read-only for Workspace Schema v1-managed paths and is tested to refuse
Projection mutation, as required by `ADR-WS-001`; no version is fabricated by
the Projection package.

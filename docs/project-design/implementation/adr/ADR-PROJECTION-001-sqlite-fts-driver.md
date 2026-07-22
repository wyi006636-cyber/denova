# ADR-PROJECTION-001: SQLite FTS Projection Driver

## Status

**Accepted** — 2026-07-22

**Approval source:** product owner P0-T09 `/goal` execution directive dated
2026-07-22. It authorizes this decision freeze only; it does not authorize a
dependency installation or any Projection, Phase 1, Tauri, or vector-search
implementation.

## Context

`ADR-WS-001` defines files as the creative truth and `.denova/index.db` as a
rebuildable projection. P1-T03 needs a database/sql driver family and a bounded
projection contract without making a database failure block a project from
opening or being edited. This ADR resolves TECH-005 (CGO, Windows/Tauri
packaging, license, and FTS capability) and DATA-004 (corruption or schema
drift blocking open).

The decision is based on the following primary sources, checked on 2026-07-22:

- [modernc.org/sqlite Go documentation](https://pkg.go.dev/modernc.org/sqlite):
  a CGO-free SQLite `database/sql` driver; documented Windows `386`, `amd64`,
  and `arm64` support, along with Linux and macOS support; tagged, stable
  BSD-3-Clause module metadata. The observed upstream line is `v1.54.0`; this
  is evidence only, not a permission to use an unpinned `latest` version.
- [modernc SQLite license](https://github.com/modernc-org/sqlite/blob/master/LICENSE):
  BSD-3-Clause redistribution conditions.
- [mattn/go-sqlite3 README](https://github.com/mattn/go-sqlite3): CGO/GCC
  requirements (including Windows GCC), the `sqlite_fts5` build tag, and MIT
  license.
- [SQLite FTS5](https://www.sqlite.org/fts5.html): FTS5 build availability,
  tokenizers including `trigram`, external-content consistency requirements,
  integrity checking, and rebuild behavior.
- [SQLite recovery](https://www.sqlite.org/recovery.html) and [how SQLite
  databases can be corrupted](https://www.sqlite.org/howtocorrupt.html):
  recovery limitations and unsafe live-file-copy failure modes.
- [SQLite backup API](https://www.sqlite.org/backup.html): consistent live
  copies through the backup API and `VACUUM INTO`.
- [Tauri sidecars](https://v2.tauri.app/develop/sidecar/): `externalBin`
  packaging and binaries named for each target triple.

## Decision

### 1. Driver and ownership

P1-T03 will use **`modernc.org/sqlite` through `database/sql`**. The
Projection remains a service owned and written by the standalone Go backend or
sidecar; readers do not mutate it. This is a driver-family decision only. P1
must select an exact reviewed version, pin it, and pin the exact matching
`modernc.org/libc` version required by that selected upstream `go.mod`.

`github.com/mattn/go-sqlite3` is rejected for this product baseline: its CGO
and GCC requirements would require target-specific C cross-toolchains,
including on Windows. A Rust/Tauri SQL plugin is not selected. Tauri remains a
future shell concern and any sidecar package must supply a separately built Go
binary for every required Tauri target triple. No vector extension, vector
search, or custom tokenizer is authorized by this ADR.

### 2. Authority and schema v1

Formal Markdown/workspace files and approved Artifact records are the only
Projection inputs and remain the creative truth. `.denova/index.db` is
disposable, excluded from workspace versions by ADR-WS-001, and must never
write facts back into formal files or Artifact records.

Projection schema v1 contains:

- schema metadata, including schema version and build/driver identity;
- a source-document table keyed by stable public document ID, canonical
  workspace-relative path, revision hash, profile, and kind;
- an FTS5 external-content index over that table, with an integer `rowid`
  boundary for FTS and the stable public document ID kept separate.

One Projection writer owns the content and FTS tables. Each content insert,
update, or delete and its FTS update must occur in one transaction using
explicit insert/update/delete triggers or equivalent single-writer code. P1
must include an implementation test that proves external-content consistency.

The v1 tokenizer is FTS5 `trigram` for Chinese/English substring retrieval.
It has a known limitation: terms shorter than three Unicode characters are not
served by trigram matching. Such queries must use an exact/ripgrep fallback.
No custom tokenizer dependency is added in v1.

### 3. Migration, rebuild, recovery, and rollback

New schema or driver builds create a fresh sibling database from a bounded
snapshot of authoritative source hashes. Before activation, the builder must
validate core integrity, row counts, and source hashes, and run the FTS5
external-content consistency form exactly:

```sql
INSERT INTO <fts_table>(<fts_table>, rank) VALUES ('integrity-check', 1);
```

The `rank = 1` argument is required because it checks the external-content
table against the content index; a plain FTS `integrity-check` is insufficient
for that comparison. The same command is required at rebuild completion and
whenever corruption or external-content inconsistency is checked. Activation
may occur only after closing connections and rechecking the source snapshot.

On a missing, stale, unknown-newer, integrity-failed, open-failed, or corrupt
database, the service must mark Projection unavailable or stale while keeping
project open/edit available. It must quarantine or retain the database for
diagnostics when safe, then rebuild from truth. It must never salvage SQLite
bytes into formal facts.

SQLite recovery APIs and `.recover` are diagnostic salvage only: upstream
warns that recovered output can be altered, can resurrect deleted content, or
can violate constraints. Normal recovery is a full rebuild. Rollback may keep
or restore a prior known-good Projection only when it still matches the source
snapshot and reader/schema contract; otherwise it must delete/quarantine and
rebuild. There is no reverse migration into truth.

Raw copying of a live database is not an approved backup. If an operational
snapshot is needed, it must use an SQLite-consistent mechanism such as the
backup API or `VACUUM INTO`; a Projection snapshot is never authoritative.

### 4. P1-T03 implementation gates

Before the dependency lands, P1-T03 must pass all of the following:

1. Select an exact version pin, pin its matching `modernc.org/libc`, complete
   license/notice review, and run `govulncheck`.
2. Build and test with `CGO_ENABLED=0` for the existing five-platform matrix,
   explicitly including Windows `amd64` and the future Tauri sidecar target
   triples.
3. At runtime, prove `sqlite_version()`, `PRAGMA compile_options`, actual
   `CREATE VIRTUAL TABLE ... USING fts5`, a trigram query, and
   `INSERT INTO <fts_table>(<fts_table>, rank) VALUES ('integrity-check', 1)`
   at fresh-db activation, rebuild completion, and corruption/inconsistency
   checks. `rank = 1` must prove the external-content table/content-index
   comparison; a plain `integrity-check` is insufficient. Importing the
   package alone is not proof that the final build contains FTS5.
4. Prove external-content consistency, corruption quarantine/rebuild, and
   atomic activation; prove project open/edit remains available with a missing,
   locked, or corrupt Projection.
5. Do not add a vector extension or product Tauri implementation in P1-T03.

## Alternatives

### `github.com/mattn/go-sqlite3`

Rejected for the baseline because CGO/GCC and target-specific C toolchains add
Windows and cross-target packaging risk. Its `sqlite_fts5` build tag also means
FTS5 must be explicitly enabled in that family.

### Rust/Tauri SQL plugin

Rejected. It would split Projection ownership away from the Go backend/sidecar
and prematurely couple P1-T03 to a Phase 4 shell decision.

### Vector search or a custom Chinese tokenizer

Deferred. FTS5 trigram plus exact/ripgrep fallback is the v1 bounded
retrieval contract; neither vector search nor a custom tokenizer is required
or authorized here.

## Consequences

The selected family minimizes the current CGO and Windows cross-build surface,
but it creates an explicit exact-version/matching-libc gate. P1 also carries a
runtime FTS5 probe because driver selection alone does not establish the final
binary's extension availability. The Projection can be deleted and rebuilt;
the file/Artifact authority boundary and open/edit availability take priority
over recovering derived bytes.

## Migration

This ADR changes no data and installs no dependency. P1-T03 will implement the
fresh-sibling-build, validate, connection-close, source-snapshot-recheck, and
atomic-activation protocol described above. Existing or failed projections are
not migrated into creative truth.

## Validation

P0 validation is documentation-only: the authoritative-source links and their
claims were checked, and the ADR freezes the P1 gates. No driver was installed,
no database was created, and no FTS, Tauri, vector, or Phase 1 implementation
was executed in this work package.

## Supersedes

None. A future change to the driver family, truth boundary, schema contract,
or recovery policy requires a superseding ADR.

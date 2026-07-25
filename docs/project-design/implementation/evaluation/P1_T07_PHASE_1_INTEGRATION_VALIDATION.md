# P1-T07 Phase 1 Integration Validation

## Scope and continuation

P1-T07 continued the pushed `feat/quality-harness-foundation` branch at exact
commit `30e4f553123af07bc3ae8c2b23221d11a10b0c34`, whose parent is
`98669428e52bdee1b1ba6a03e89486ad9a3eeb67`. Before any edit,
`origin` was exactly `https://github.com/wyi006636-cyber/denova.git`, the remote
branch resolved to the same commit, divergence was `0 0`, and the worktree was
clean. `git fetch --prune origin` completed successfully. No P1-T01–P1-T06
commit was reset, checked out, rebased, replayed, or replaced.

| Delivery | Required commit | Ancestor of continuation HEAD |
|---|---|---|
| P1-T02A | `6ed57b469b806d7e1924e6198774395a0628bd97` | yes |
| P1-T02B | `1760cdbcc8d1a141f60861333cef1886695d4310` | yes |
| P1-T03 | `8e3293a4e3a367fa6db613094a3f9da8519f9dfe` | yes |
| P1-T04 | `bf23a968f3b99f12ac4de12f7a971e45b3defc86` | yes |
| P1-T05 | `98669428e52bdee1b1ba6a03e89486ad9a3eeb67` | yes |
| P1-T06 | `30e4f553123af07bc3ae8c2b23221d11a10b0c34` | yes, exact starting HEAD |

The verified toolchain was Go 1.26.5 on darwin/arm64, Git 2.50.1 (Apple Git),
pnpm 10.33.0, and Node 22.22.2 from
`/opt/homebrew/opt/node@22/bin`. Web gates use that Node 22 path explicitly.

This task is the Phase 1 engineering integration and exit gate. Phase 1
explicitly does not run a complete generation loop and P1-T07 is not Author
Finalization. It adds no configuration because it introduces no runtime or
user-selectable behavior. It changes no production code, Accepted ADR,
normative schema, frozen API/SSE surface, dependency, or manifest.

## Phase 1 requirement-to-evidence matrix

| Phase 1 boundary | Integration evidence | Engineering conclusion only |
|---|---|---|
| Workspace Schema v1 and migration recovery | `phase1_workspace_test.go` composes inspection, preview, authorization, execution, backup, completed replay/resume, and rollback through public boundaries | fresh/current/legacy workspace contracts integrate without rewriting author bytes |
| Disposable SQLite/FTS Projection | `phase1_projection_test.go` deletes and corrupts only a `t.TempDir()` index, then reopens the workspace and rebuilds from formal files | `.denova/index.db` remains reconstructible state, never authority |
| External author modification | The same suite changes `chapters/0001.md`, observes `stale/source_changed`, and compares unrelated formal/audit bytes | author Markdown remains truth and is not silently overwritten |
| Authority and version policy | `phase1_authority_test.go` restores a positive/negative path matrix and checks legacy unknown protection | version capture and Projection sources respect Workspace Schema v1 authority |
| Quality API/UI integration | Existing P1-T05/P1-T06 tests plus `phase1-integration.test.tsx` cover four endpoints, bounded errors, navigation exclusivity, and mode preservation | the existing read-only Project Quality surface has not expanded |
| Phase 1 gate 4 event boundary | A valid `workflow.input.invalidated` event passes domain validation, App adaptation, and SSE encoding without author content | frozen transport contract is integrated; no persistent Run is created or mutated |
| Phase 1 gate 5 source isolation | Candidate and unconfirmed Preference proposal are absent from default source snapshot/query results | isolation is verified; no Context Pack Builder is implemented or implied |

## Workspace and migration matrix

All destructive-looking cases use `t.TempDir()` fixtures. Preview and inspection
are compared by complete regular-file SHA-256 maps before and after.

| Workspace kind | Open/inspection | Preview | Execute/recovery result |
|---|---|---|---|
| Fresh | opens with canonical active root `.denova` | `new`, empty source root, zero write | publishes the existing `not_required` adoption contract; no backup, receipt, rollback, or migration residue |
| Canonical `.denova` | opens at `.denova` | `current`, source `.denova`, zero write | verified backup manifest precedes completion; completed replay/resume is byte-idempotent; rollback restores the original non-evidence path set and bytes |
| Legacy-only `.nova` | opens at `.nova` | `legacy`, source `.nova`, zero write | verified backup, migration, idempotent completed replay/resume, and rollback preserve the original author path set and bytes |

The integration suite exercises the recovery boundary already supplied by
P1-T02B; it does not invent a second migration engine or production-only test
entry point. Leaf tests keep the existing fault-boundary/resume/rollback tests
as their detailed evidence rather than duplicating every injected leaf fault.

## Projection delete, rebuild, and query equivalence

The Projection integration builds an index from the marker, `ideas.md`, and two
chapters, records the source snapshot and multilingual query responses, deletes
only that fixture's `.denova/index.db`, and observes exact
`unavailable/missing`. Workspace inspection still opens and all selected
formal bytes remain equal. Rebuild then preserves:

- source snapshot hash and document count;
- complete query responses for `quick`, `小说`, and `删除重建`;
- stable document path/ID, revision, source hash, and snapshot structure; and
- byte equality of every selected formal source before and after rebuild.

A separately corrupted fixture returns exact `unavailable/corrupt` while the
workspace and authoritative chapter remain readable. These results establish
file-to-Projection reconstruction only; the index never writes back to formal
files or Artifacts.

## External Markdown, authority, and invalidation boundary

After a successful rebuild, the integration test replaces one chapter with new
external author bytes. Workspace inspection continues to open, Projection
inspection reports exact `stale/source_changed`, stale Projection open is
refused with that reason, and the new chapter bytes remain unchanged. A setting
file, Review Artifact, finalization version record, and QualitySpec reference
remain byte-equal and are neither overwritten nor deleted.

The test separately constructs the frozen P1 `workflow.input.invalidated`
event, validates it, adapts it through the App boundary, and encodes its SSE
frame with the original event ID, event type, and bounded `source_changed`
reason. The frame excludes author content. The synthetic Run ID exists only as
a required field of this transport fixture: there is no persistent Run
repository, state transition, workflow state machine, or automatic
invalidation orchestration.

CandidateSet and an unconfirmed Preference proposal are retained as audit
inputs but absent from the default Projection source snapshot and search
results. An explicit Candidate Artifact allowlist admits only that exact path;
it does not implicitly admit the Preference proposal. This proves source
isolation, not a P2 Context Pack selection policy.

## Version include/exclude matrix

Restore is tested after changing both included and excluded fixtures. Included
paths return to their captured bytes; excluded paths retain their live bytes.

| Included formal/audit/protected paths | Excluded disposable/runtime paths |
|---|---|
| `book.json`, `ideas.md`, `setting/**`, `chapters/**`, workspace marker | `.denova/index.db*`, `.denova/cache/**`, `.denova/quality/projections/**` |
| Profile lock, QualitySpec, confirmed Preference file | `.denova/quality/runs/**`, `.denova/runs/**`, checkpoints, sessions, backups |
| CandidateSet, ReviewIssue, explicitly allowed Artifact | messages, changes, legacy review/interactive/automation inbox runtime state |
| decisions, finalizations, migration receipts | legacy runs/checkpoints/sessions |
| legacy schema/profile and v1-looking unknown files, including legacy index/cache-shaped unknowns | `.denova-migration/**`, `.nova-migration/**`, and migration temporary siblings |

The dedicated legacy test verifies that
`.nova/quality/preferences.jsonl` remains byte-protected and version-included as
a v1-looking unknown, while it is not silently reinterpreted as a Projection
source.

## Quality API, UI, and mode regression

P1-T05 remains exactly four read-only/zero-write endpoints:

1. `GET /api/quality/profiles`
2. `GET /api/quality/profiles/:profile_id`
3. `GET /api/quality/project`
4. `POST /api/quality/project/migration-preview`

Existing client, handler, navigation, store, and view-state tests remain in the
targeted gate. The new narrow frontend integration test uses the production
QueryClient retry semantics and simultaneous catalog/project `500` responses;
it verifies that initial loading terminates, both localized bounded errors are
shown, raw backend detail/absolute paths are hidden, and no loading status is
left behind. No run/apply/confirm/start/decision/finalization behavior or API
client is added.

Existing navigation regression tests and live page evidence cover desktop and
mobile single-active primary navigation. Entering and leaving Quality from
both `ide` and `interactive` preserves `nova:content-mode`; only explicit user
mode actions change it.

## TDD and machine-readable audit

The pre-edit targeted baseline passed: Go exit 0 for the P1-T02–P1-T06 package
set, and frontend exit 0 for 6 files / 30 tests. No existing failing test was
edited around, skipped, deleted, or weakened.

The first Workspace JSON RED run exited 1 because the new fresh-workspace test
incorrectly expected migration preview `SourceRoot` to be `.denova`. The
existing contract correctly uses an empty source root for a new workspace
while inspection's active root is `.denova`; correcting only that test
assumption produced GREEN without production changes. Projection and authority
integration cases passed on first execution against the existing public
boundaries. The controlled live-page retry investigation led to the frontend
production-retry regression test, which passed without a production fallback
or enlarged mock.

The final standalone integration GREEN `go test -json` audit recorded 14
terminal test records, 12 leaf tests, passed 12, failed 0, skipped 0, leaves
over 1.0 seconds 0; the slowest leaf was the legacy migration case at 0.52
seconds. Its migration parent record was 1.03 seconds because it aggregates two
leaf cases and is not itself a leaf. A preceding audit launched concurrently
with three other gates observed resource-contended leaf durations above one
second and was rejected as final timing evidence; the standalone audit was run
unchanged after all competing commands had exited. Final Go and Vitest
machine-readable audit results are recorded in the verification table below.

## Ego lite live-page validation

Validation used the currently running Denova page at `http://localhost:5173`
in an isolated ego lite task space. No frontend/backend process was killed,
replaced, or started, and no real workspace was modified. Controlled states
intercepted only Quality/settings fetches in that page and were restored after
each observation.

| Matrix item | Live observation |
|---|---|
| zh-CN / light / 1440×900 | Project Quality rendered from the live read-only API; exactly one primary item active; body/document width 1440 |
| en-US / dark / 1440×900 | English navigation and content, black theme surface, one active Project Quality item, no raw i18n key |
| zh-CN / light / 390×844 | Adaptive content and mobile navigation fit exactly 390 pixels with one active item |
| en-US / dark / 390×844 | English mobile surface fit 390 pixels, one active item, no raw key |
| Long text | Controlled localized Profile/QualitySpec text wrapped at 390 pixels without horizontal overflow |
| Loading | Localized bounded loading state rendered |
| Empty | Localized empty catalog state rendered without fallback behavior |
| Error | Controlled production Query errors rendered two bounded localized alerts without exposing the injected internal detail/path |
| Mode preservation | `ide` → Quality → return remained `ide`; explicit game switch set `interactive`; `interactive` → Quality → return remained `interactive`; mobile explicit Writing switch returned to and preserved `ide` |

The long-text, loading, and empty recorders observed no new console error. The
normal page was finally restored to zh-CN, light, 1440×900, `ide`, with Quality
visible, loading complete, and one active primary item. The hidden automation
tab pauses TanStack Query retries by design; the error-state check temporarily
disabled retry on that isolated page's current QueryClient, remounted the same
production view, captured the bounded error UI, then restored all defaults and
responses. This was validation-environment control, not a product fix.

## Verification gates

| Gate | Result |
|---|---|
| Requested targeted Go package command | exit 0; all 9 requested packages passed |
| Go JSON audit over added/modified integration tests | exit 0; standalone audit: 12/12 leaves passed, failed 0, skipped 0, leaves over 1.0s 0; slowest leaf 0.52s |
| Requested race package command | exit 0; integration, workspace, projection, and versions passed under `-race` |
| Requested targeted Vitest command | exit 0; 7 files / 31 tests passed |
| Vitest JSON audit over added/modified target | exit 0; 31/31 passed, failed 0, pending/skipped 0; slowest leaf 488.56ms, new P1-T07 leaf 318.93ms |
| `PATH=/opt/homebrew/opt/node@22/bin:$PATH pnpm --dir web test` | final standalone exit 0; 131 files / 683 tests passed |
| `PATH=/opt/homebrew/opt/node@22/bin:$PATH pnpm --dir web check:i18n` | exit 0; 3,082 keys aligned across zh-CN/en-US |
| `PATH=/opt/homebrew/opt/node@22/bin:$PATH pnpm --dir web build` | exit 0; TypeScript and Vite production build passed; existing chunk-size warning only |
| `go test ./... -count=1` | exit 0 |
| `go vet ./...` | exit 0 |
| `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` | shell exit 1 (`govulncheck` exit status 3); only pre-existing reachable `GO-2026-5970` in `golang.org/x/text v0.38.0` |
| `./scripts/build.sh` | exit 0; frontend embed, `denova`, and `denova-updater` built |
| `go mod tidy -diff` | exit 0; no output |
| Protected manifest diff from `30e4f553123af07bc3ae8c2b23221d11a10b0c34` | exit 0; `go.mod`, `go.sum`, `web/package.json`, and `pnpm-lock.yaml` unchanged |
| `git diff --check` | exit 0 |

The first full Web run was mistakenly launched concurrently with the production
build, full Go suite, and i18n audit. It exited 1 with four unrelated existing
tests reaching their unchanged five-second timeouts. No test or timeout was
modified: those exact four files then passed 63/63 in isolation, and the
required full Web command passed 683/683 in a standalone rerun. Only the
standalone run is the final gate result; the earlier exit 1 remains recorded as
environmental contention evidence rather than being hidden.

## Known vulnerability

`govulncheck` reports exactly the already-known reachable `GO-2026-5970` in
`golang.org/x/text v0.38.0`, with traces through Workspace portable-path
normalization. It reports no second vulnerability ID and no new dependency.
The required scan therefore retains its real non-zero shell status. P1-T07
does not upgrade `x/text`, edit dependency manifests, or claim vulnerability
closure.

## Independent review

Minimax M3 was not exposed by the collaboration environment, so the
independent whole-diff reviewer was `gpt-5.5` with `xhigh` reasoning. Final
disposition after reviewing the complete uncommitted diff and spot-running the
Go integration package plus the new frontend regression test: Critical 0,
Important 0, Minor 1, scope-expansion/P2 findings 0. The sole Minor was this
section's outstanding review placeholder; replacing it with the actual result
completes that documentation cleanup. The reviewer found no code/test
correctness issue, unsafe non-fixture operation, contract expansion, skipped
test, or dependency/configuration change. The reviewer's frontend spot check
used its shell-default Node, while all authoritative frontend gates in the
table above used the required Node 22 path.

## Deferred boundary and engineering conclusion

P2-T01 Context Pack Builder and P2-T03 persistent Run/invalidation runtime do
not exist in this delivery. Persistent Run storage, automatic invalidation
orchestration, workflow state machines, Candidate/Review/Preference UI, Author
Finalization, formal-content write APIs, complete generation, Agent,
Automation, Tauri, and vector work remain deferred. Projection stale evidence
plus a frozen event envelope is not automatic Run invalidation; source
isolation is not a Context Pack Builder.

The only permitted positive conclusion after all final gates and independent
review pass is **Phase 1 engineering integration gates passed**. This is not an
H v1, Quality Harness product-quality, Beta, release, publication, or
publication-readiness conclusion.

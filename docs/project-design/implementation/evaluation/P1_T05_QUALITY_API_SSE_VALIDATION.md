# P1-T05 Quality API and SSE Validation Record

## Scope and authority

This is local engineering evidence for P1-T05 only. On 2026-07-24 the owner
accepted the following exact statement: `接受 ADR-SSE-001 v1，并接受 D01–D12 的 API/资产方案。`

P1-T05 depends on P1-T01 and P1-T04. `ADR-PROFILE-001`, `ADR-QS-001`, and
`ADR-SSE-001` are Accepted v1 (2026-07-21, 2026-07-21, and 2026-07-24
respectively). This task is display-only: P2-T03 alone owns the deterministic
runtime, checkpoints, and Run repository. It establishes neither P2 completion
nor Author Finalization, product-quality acceptance, push, merge, or release.

## Production assets and package boundary

`docs/project-design/implementation/contracts/assets.go` compile-time embeds the
single-source normative assets and returns cloned bytes. Production does not consult a
runtime working directory or copy normative JSON beneath `internal/`; the three
examples are a validated `read_only_catalog`, never installed as
`.denova/profile-lock.json`, a workspace default, or author truth.

| Normative asset | SHA-256 |
|---|---|
| `quality-event-v1.schema.json` | `39a4de536185a75ffbab421dd5c90e06f2590a61e6cf23bfb89239ebb3c78ff7` |
| `profile-v1.schema.json` | `8adaa49a9ccb50daaf717279f175959abf993cdf2c9ce63121619f6dbce229c8` |
| `quality-spec-v1.schema.json` | `e9b7e96a9d68755c7b36042a64759225b57bad143d362a929d07f08ad7a04b32` |
| `examples/long_serial.json` | `47761b8de3b05a29e67475b2eff0d611f50c1f9379c35be28570305cdbbe2b23` |
| `examples/fanqie_short.json` | `6db51f1971529d5b68246417aa4555b4ab1fa754113332923e883093a599208d` |
| `examples/zhihu_salt_short.json` | `ef780b82470d97d27675455f963e4a3ee4aa67b7c31aad3eca506d595bbc3663` |

`internal/quality/harness` owns the transport-neutral event contract, exact
schema/semantic admission, replay validation, and narrow durable-read port.
`internal/app` owns catalog and current-workspace read projections;
`internal/api` is HTTP/SSE/Agent display adaptation. The direction is
API -> App -> Quality packages; Harness imports no App, Agent, API, UI, or disk
implementation. No dependency direction change or `go.mod`/`go.sum` change is
included.

| Responsibility | Files |
|---|---|
| Normative embedded assets | `contracts/assets.go`, `quality-event-v1.schema.json`, existing Profile/QualitySpec schemas and three examples |
| Exact event contract and durable-read boundary | `internal/quality/harness/event.go`, `validation.go`, `schema_validation.go`, `replay.go`, `read_port.go` |
| Bundled catalog and zero-write project projection | `internal/app/quality_app_service.go`, `quality_profile_catalog.go`, `quality_workspace_service.go` |
| App/SSE/display adaptation | `internal/app/quality_event_adapter.go`, `internal/api/sse/quality_event.go`, `quality_reconnect.go`, `internal/api/agentui/quality_activity.go` |
| HTTP surface | `internal/api/routes.go`, `internal/api/handlers/handler_quality.go` |

## Read-only Quality API matrix

All public DTOs are stable and bounded. Catalog DTOs expose profile ID, contract
version, source hash, bound QualitySpec contract/version/hash, bilingual summary,
and `read_only_catalog`; detail input is capped at 1 MiB and public detail at
256 KiB. Project/preview projections use resource ID `current`, safe relative
paths, bounded strings/issues, deterministic page/digest collections, and omit
absolute roots, raw marker bytes, internal errors, and file content. Error text
is Chinese by default and English for an `en*` locale; only stable codes cross
the API boundary.

| Endpoint | Success / bounded DTO | Error behavior | Authority and forbidden effects |
|---|---|---|---|
| `GET /api/quality/profiles` | 200, exhaustive read-only bundled catalog | 500 `quality_assets_unavailable` | Embedded registry only; no Profile/QualitySpec install or write |
| `GET /api/quality/profiles/:profile_id` | 200, one bounded cloned Profile detail | 404 `quality_profile_not_found`; 500 assets unavailable | Bundled catalog only; no author decision or mutation |
| `GET /api/quality/project` | 200, current-workspace compatibility inspection | 409 `quality_no_workspace`; 500 `quality_workspace_inspection_failed` | Existing Workspace Schema v1 inspector only; no Run, projection, or workspace write |
| `POST /api/quality/project/migration-preview` | 200, deterministic complete-preview digest plus offset/limit page (default 0/100, max 500) | 400 `quality_invalid_request`; 409 no workspace; 500 inspection failure | Preview reads the current workspace only and writes no marker, backup, intent, receipt, run, projection, Git, or migration artifact |

No endpoint starts or mutates a Quality Run, creates a decision, selects a
Candidate, appends a PreferenceSignal, finalizes content, or writes a projection.

## Exact Quality event matrix

Every exact-v1 event has closed `contract.kind=denova.quality-event`,
`contract.version=v1`, `event_type`, `event_id`, `run_id`, UTC-Z `occurred_at`,
positive contiguous `sequence`, and `summary`. ID fields use the existing
3--128 ASCII opaque-ID grammar. The complete 18-event field matrix is:

| Event type | `stage_id` | `artifact_id` |
|---|---|---|
| `workflow.run.created` | forbidden | forbidden |
| `workflow.stage.started` | required | forbidden |
| `workflow.stage.completed` | required | forbidden |
| `workflow.stage.failed` | required | forbidden |
| `workflow.input.invalidated` | forbidden | forbidden |
| `workflow.decision.required` | required | forbidden |
| `artifact.created` | required | required |
| `candidate.created` | required | required |
| `candidate.compared` | required | required |
| `candidate.selected` | required | required |
| `review.issue.created` | required | required |
| `review.completed` | required | required |
| `revision.completed` | required | required |
| `preference.confirmed` | forbidden | forbidden |
| `preference.revoked` | forbidden | forbidden |
| `finalization.started` | required | required |
| `finalization.completed` | required | required |
| `finalization.rolled_back` | required | required |

`summary.code` is exactly `quality.event.<event_type>`. Its ordered arguments
contain at most eight unique pairs with names only from `profile_id`,
`stage_kind`, `artifact_kind`, `decision_kind`, `preference_kind`, `reason_code`,
`result_code`, and `item_count`. Values are non-nested strings, at most 128
bytes; `profile_id` is one of the three v1 IDs, token/count grammars are closed,
and canonical summary JSON is at most 1,024 bytes. Schema plus semantic
validation admit exact v1; unknown/newer bytes remain inspection-only and are
never adapted, replayed, emitted, renamed, or default-routed.

The prohibited payload set includes prompts, thinking, model messages,
manuscript/setting/candidate text, ReviewIssue quotes, Preference reasons,
tool arguments/results, paths, URLs, keys, headers, credentials, secrets,
telemetry blobs, maps, lists, free text, and other unbounded extensions.

## App, SSE, and Agent UI display contract

The App adapter accepts only a validated `harness.Event`, preserves the entire
closed envelope, maps the exact event type to `agent.Event.Type`, and maps the
envelope to `agent.Event.Data`. `DecodeQualityEvent` revalidates the JSON and
requires type equality. `WriteQualityEventFrame` emits exactly original
`id: <event_id>`, `event: <event_type>`, and closed-envelope `data: <JSON>`;
its stable error codes are `quality_sse_invalid_event`,
`quality_sse_type_mismatch`, and `quality_sse_write_failed`.

`agentui.StreamEncoder` identifies Quality-shaped data before legacy event
branches. It projects only bounded identity, time, sequence, and summary;
original `event_id` is the activity ID when valid, otherwise a bounded fallback
(`quality-event` where required). It never copies unsafe body, content, path,
message, error detail, secret, or nested values.

## Reconnect truth and ordering

The narrow state read takes `run_id` and optional `Last-Event-ID`, returning
only run ID, positive durable state revision, durable `last_sequence`, and
resolved `resume_after_sequence` (zero with no cursor). Reconnect validates the
request, reads durable state, then atomically obtains Task snapshot/live display
subscription. It cursor-filters snapshot events, validates original identity
and contiguous sequence through the durable upper bound, then emits replay
followed by live delivery. Completion events remain replayable.

Unknown cursors, sequence gaps, duplicate IDs, unavailable display, closed
delivery before the durable upper bound, or a slow-subscriber drop trigger a
durable reread and stable reconnect error/current receipt. They never execute a
stage or model call. Task snapshot/live is display recovery only: a slow
subscriber can lose display delivery, never durable state. P2-T03 must provide
the actual persistent state authority and checkpoint/recovery implementation.

## Privacy, errors, and write authority

| Boundary | Guaranteed behavior | Forbidden effect |
|---|---|---|
| Public errors | Localized bounded stable code/message only; no absolute paths, internal causes, raw files, credentials, or secrets | Error detail cannot become an information channel |
| Event/SSE/Agent display | Closed identity/summary projection; prohibited payload list is rejected or omitted | No body, prompt, thinking, content, path, tool result, secret, or unbounded map/list |
| Author authority | Display/read adapters do not manufacture authority | No author decision, PreferenceSignal, Candidate selection, or finalization receipt |
| Workspace and formal truth | Inspection/preview reads bounded workspace facts only | No chapters, setting/lore, Profile/QualitySpec, Git, Projection, runs, or formal-content write |
| Migration preview | Deterministic complete projection before paging; tree snapshots stay identical | No marker, backup, stage, intent, receipt, or migration artifact write |

## TDD and review record

Task 2 recorded RED-to-GREEN coverage for normative assets, exact schema,
semantic envelope, exhaustive scope matrix, summaries, inspection, replay, and
durable read port. A downstream Task 4 adapter run discovered the real
`summaryArgument` schema regression: parent `additionalProperties:false` did
not admit `name` and `value` declared solely under `oneOf`. The direct RED was
the valid summary-argument rejection; the minimal controller fix declared those
properties at the parent, followed by scoped schema/harness review and passing
adapter regression. No fallback, coercion, retry, sleep, network, model call,
daemon, or workspace was used.

Task reviews and the documented fix rounds have zero open Critical, Important,
or Minor findings. Minimax M3 was not available in the environment, so the
final whole-branch review used `gpt-5.5` at `xhigh` reasoning. Its first pass
reported zero Critical, two Important, and two Minor findings. The final fix
round made path sanitization host-independent, made a nil Task display adapter
return a bounded error after the durable read, and rejected explicit-null and
duplicate preview fields. The same reviewer then marked every finding
addressed, found no new Critical or Important issue, and returned PASS.

## Focused gate record (fresh Task 5 evidence)

| Command | Result |
|---|---|
| `go test ./internal/quality/domain ./internal/quality/profile ./internal/quality/workspace ./internal/quality/projection ./internal/quality/harness ./internal/app ./internal/api ./internal/api/sse ./internal/api/agentui -count=1` | exit 0; package times: domain 0.623s, profile 0.915s, workspace 21.749s, projection 6.250s, harness 1.438s, app 10.425s, api 9.785s, sse 1.597s, agentui 1.085s |
| `go test -json ./docs/project-design/implementation/contracts ./internal/quality/harness ./internal/app ./internal/api ./internal/api/sse ./internal/api/agentui -count=1` | exit 0; all 6 package terminal actions passed; JSON audit saw 450 leaf tests, 0 skips, 0 leaves over 1.0s; slowest leaf was `TestInteractiveStoryKeepsOpeningAndPresetWhenAsyncStateSchemaInitializationFails` at 0.51s |
| `go test -race ./internal/quality/domain ./internal/quality/profile ./internal/quality/workspace ./internal/quality/harness ./internal/app ./internal/api ./internal/api/sse ./internal/api/agentui -count=1` | exit 0; package times: domain 2.458s, profile 2.325s, workspace 23.153s, harness 2.273s, app 20.253s, api 16.520s, sse 3.025s, agentui 3.501s |
| `go vet ./docs/project-design/implementation/contracts ./internal/quality/harness ./internal/app ./internal/api ./internal/api/sse ./internal/api/agentui` | exit 0; no output |
| `go mod tidy -diff` | exit 0; no output |
| `git diff --check` | exit 0; no output |

Static audit of the P1-T05 production additions found no runtime-cwd normative
document reads and no filesystem/Git/repository/migration-executor/formal-write
primitive. The only workspace interaction is the existing read-only inspector
and `BuildMigrationPreview` call. `HEAD` remained
`bf23a968f3b99f12ac4de12f7a971e45b3defc86`; the index was clean; and
`git diff --no-ext-diff --exit-code bf23a968f3b99f12ac4de12f7a971e45b3defc86 -- go.mod go.sum`
exited 0.

## Final Task 6 gate record

All results below are fresh after the final fix round.

| Gate | Result |
|---|---|
| Exact requested package test command | exit 0 |
| Changed-package `go test -json` audit | exit 0; 480 passing leaf events, 0 failures, 0 skips, 0 leaves over 1.0s; slowest leaf 0.68s |
| Exact requested race command | exit 0 |
| CGO-disabled compile plus `cmd/denova` build matrix | darwin/arm64 0/0; darwin/amd64 0/0; linux/arm64 0/0; linux/amd64 0/0; windows/amd64 0/0 |
| `go test ./... -count=1` | exit 0 |
| `go vet ./...` | exit 0 |
| `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` | shell exit 1 (`govulncheck` exit 3); only the pre-existing `GO-2026-5970` reachable in `golang.org/x/text v0.38.0`; no new vulnerability ID |
| Node 22 `pnpm --dir web test` | exit 0; 125 files and 651 tests passed |
| Node 22 `pnpm --dir web check:i18n` | exit 0; 2,987 keys aligned |
| Node 22 `pnpm --dir web build` | exit 0; existing chunk-size warning only |
| `./scripts/build.sh` | exit 0 |
| `go mod tidy -diff` | exit 0; no output |
| Baseline byte comparison for `go.mod` and `go.sum` | exit 0; unchanged from `bf23a968f3b99f12ac4de12f7a971e45b3defc86` |
| `git diff --check` | exit 0; no output |
| Final whole-branch review and finding re-review | PASS; zero open Critical, Important, or Minor findings |

The controller commit and its hash are reported after this pre-commit record is
included in the single delivery commit. This record does not imply push, merge,
release, product-quality acceptance, or P2 completion; push remains forbidden.

# ADR-SSE-001: Quality Event Envelope v1

## Status

**Accepted v1** — 2026-07-24

The owner explicitly accepted ADR-SSE-001 v1 together with decisions D01-D12
for the P1-T05 Quality API and production-asset loading boundary on 2026-07-24.

## Context

P1-T05 depends on P1-T01 and P1-T04. The accepted Profile, QualitySpec,
Workspace Schema, CandidateSet, ReviewIssue, and PreferenceMemory v1 contracts
already provide stable record identities, author-control boundaries, and exact
v1 validation. P1-T05 must expose a minimal read-only-first Quality API and
freeze Quality event transport without starting the P2-T03 deterministic
Harness state machine, persistent checkpoints, or Run repository.

Denova already has one background display transport. `internal/app.Task` stores
an in-memory event snapshot, broadcasts live `agent.Event` values, and drops a
live delivery when a subscriber is slow. `internal/api/sse` replays the Task
snapshot before the live channel, and `internal/api/agentui` maps unknown events
to bounded activity data. These behaviors are useful display transport, but the
Task buffer is not durable state and cannot become Quality Run authority.

Quality events need an exhaustive, transport-neutral v1 contract. Replayed
events must retain their original identity, order, and time; reconnect must
read durable Run state before subscribing to display events and must never
restart execution. Payloads must remain safe for logs, SSE, and UI recovery and
must not carry creative content, model internals, credentials, or unbounded
maps.

The accepted Profile ADR also states that the three committed Profile examples
are complete contract fixtures, not active production defaults. P1-T05 therefore
needs a single-source asset-loading decision that can expose those validated
examples as a read-only bundled catalog without installing them into a
workspace or silently turning them into author truth.

## Decision

This section records the accepted v1 decision.

### Contract identity and closed event vocabulary

The normative machine-readable companion will be
`contracts/quality-event-v1.schema.json`. Its exact contract identity is:

- `contract.kind = denova.quality-event`
- `contract.version = v1`

The complete Quality event v1 vocabulary is exactly:

- `workflow.run.created`
- `workflow.stage.started`
- `workflow.stage.completed`
- `workflow.stage.failed`
- `workflow.input.invalidated`
- `workflow.decision.required`
- `artifact.created`
- `candidate.created`
- `candidate.compared`
- `candidate.selected`
- `review.issue.created`
- `review.completed`
- `revision.completed`
- `preference.confirmed`
- `preference.revoked`
- `finalization.started`
- `finalization.completed`
- `finalization.rolled_back`

Unknown event types and unknown contract versions are preserved for inspection
only and are rejected for v1 validation, adaptation, replay, or emission. They
are never downgraded, renamed, or routed through a default semantic branch.

### Envelope and stable identity

Every exact-v1 event requires these fields:

- `contract`
- `event_type`
- `event_id`
- `run_id`
- `occurred_at`
- `sequence`
- `summary`

`event_id`, `run_id`, `stage_id`, and `artifact_id` use the existing Quality v1
opaque-ID boundary: 3 to 128 ASCII characters matching
`^[a-z0-9][a-z0-9._:-]{2,127}$`. `event_id` is globally unique. IDs are assigned
once by the owning durable producer and stored with the event; replay never
generates or rewrites them.

`occurred_at` is an immutable RFC 3339 timestamp in UTC using the `Z` designator.
The exact stored timestamp is replayed; replay time is not an event time.

`sequence` is a positive integer allocated once and contiguously within one
Run, starting at 1. A replay validator requires the next event to be exactly
the preceding sequence plus one and rejects a gap, duplicate, or regression.
A partial replay must carry a durable preceding-sequence cursor from the Run
state read; it may not infer the cursor from buffer length or event time.

The envelope has `additionalProperties: false`. JSON Schema validation and a
Go semantic validator both participate in exact-v1 admission.

### Event scope and conditional fields

The v1 field matrix is exhaustive:

| Event type | `stage_id` | `artifact_id` |
|---|---|---|
| `workflow.run.created` | forbidden; this is Run-scoped | forbidden |
| `workflow.input.invalidated` | forbidden; one Run-level invalidation leads clients to reread state | forbidden |
| `preference.confirmed`, `preference.revoked` | forbidden; these are Run-scoped author-result notifications | forbidden |
| `workflow.stage.started`, `workflow.stage.completed`, `workflow.stage.failed` | required | forbidden |
| `workflow.decision.required` | required | forbidden |
| `artifact.created` | required | required |
| `candidate.created`, `candidate.compared`, `candidate.selected` | required | required |
| `review.issue.created`, `review.completed`, `revision.completed` | required | required |
| `finalization.started`, `finalization.completed`, `finalization.rolled_back` | required | required |

For candidate, review, revision, and finalization events, `artifact_id` names
the stable queryable Artifact involved in that event. A receipt, preference
signal, issue collection, or selected prose is not embedded into the envelope.
An inapplicable conditional field is omitted, never encoded as an empty string
or `null`.

### Bounded localizable summary

`summary` contains only:

- `code`: exactly `quality.event.<event_type>`;
- `arguments`: an ordered array of at most eight unique name/value pairs.

Argument names are closed in v1 to `profile_id`, `stage_kind`, `artifact_kind`,
`decision_kind`, `preference_kind`, `reason_code`, `result_code`, and
`item_count`. Values are strings, not nested JSON. ID/code arguments must match
the bounded lowercase token grammar; `profile_id` must be one of the three
Profile v1 IDs; `item_count` must be canonical unsigned decimal. Each value is
at most 128 bytes, and the canonical JSON encoding of the complete summary is
at most 1,024 bytes.

The summary carries localization inputs, not already translated prose. It has
no free-text, body, excerpt, message, prompt, path, URL, map, list, or opaque
extension argument. Details are read through an authorized ordinary API from
the durable state/Artifact source.

### Payload prohibition

A Quality event or its SSE representation must not contain prompts, thinking,
model messages, manuscript or setting text, candidate text, ReviewIssue quote
text, Preference content or reasons, tool arguments/results, absolute paths,
API keys, authorization headers, credentials, secrets, telemetry blobs, or any
other unbounded payload. A value that needs those bytes is represented by a
stable ID and read separately under the owning authority.

### Package and transport boundary

The Quality event type, vocabulary, semantic validation, sequence/replay
validation, and durable-state read port belong in
`internal/quality/harness`. This P1 package is contract-only: it contains no
Job/Run/Stage state machine, model invocation, checkpoint writer, Run
repository implementation, author decision creator, Preference writer, or
Finalization behavior. It depends only on the Go standard library, stable
Quality domain identifiers as needed, and the repository's existing
`jsonschema/v6` validator for the normative schema; it does not import Hertz,
Agent, App, React, or disk implementations and adds no dependency.

The App layer validates a Quality event and maps it without loss to the
existing `agent.Event`: `agent.Event.Type` is the exact Quality `event_type`,
and `agent.Event.Data` is the closed bounded envelope. The adapter cannot accept
an unknown event type/version or arbitrary `agent.Event.Data` as a Quality
event.

`internal/api/sse` adds only the Quality validation/output boundary and reuses
the existing Task/SSE writer and snapshot/live subscription. A Quality SSE
frame uses the original `event_id` as the SSE `id`, the exact `event_type` as
`event`, and the closed envelope as JSON `data`. P1-T05 does not add a Quality
Run/start/decision/finalization endpoint or a second SSE transport. The current
`agentui.StreamEncoder` default activity mapping remains the safe UI fallback;
P1-T05 may add contract coverage but does not add Quality workflow behavior to
Agent UI.

### Durable state read and reconnect order

P1-T05 defines a narrow read port and no repository implementation. The port
accepts `run_id` and an optional `Last-Event-ID`. Its read receipt contains only
`run_id`, a positive durable `state_revision`, durable `last_sequence`, and the
resolved `resume_after_sequence` (`0` when no prior event was supplied). An
unknown event ID is an error. This receipt is deliberately not a P2 Run state
model.

Reconnect order is fixed:

1. validate the requested Run ID and optional `Last-Event-ID`;
2. read durable Run state and resolve the stored event cursor;
3. only after the state read succeeds, atomically obtain the existing Task
   snapshot/live subscription;
4. validate replay identity and contiguous sequence against the durable cursor,
   then emit the original stored events followed by live events;
5. on an unknown cursor, gap, dropped live delivery, or unavailable Task,
   reread durable state and report a stable reconnect error or current state;
   never restart a stage or model call.

Task snapshot/live is a display optimization. A slow subscriber may miss a
live delivery without losing Run state. The future P2-T03 Run repository is the
only persistent state authority and owns checkpoint/recovery behavior. P1-T05
uses fakes and barriers to test this ordering and cannot create a placeholder
file, SQLite, memory, or run-log repository and call it durable.

### P1-T05 Quality API and asset boundary

The proposed minimal HTTP surface is exactly:

- `GET /api/quality/profiles`
- `GET /api/quality/profiles/:profile_id`
- `GET /api/quality/project`
- `POST /api/quality/project/migration-preview`

These endpoints are reads or a zero-write preview. They do not start or mutate
a Run, create a decision, select a Candidate, append a PreferenceSignal,
finalize content, write a projection, or change workspace files.

Profile/schema/example bytes are compiled from the single existing normative
files through a small Go `embed` package adjacent to
`docs/project-design/implementation/contracts`. No runtime working directory is
consulted, and no copied JSON mirror is created under `internal/`. Exact byte
hashes are exposed by the App DTO and verified in tests. The three examples are
served only as a validated read-only bundled catalog; they are never installed
as `.denova/profile-lock.json`, written as a QualitySpec, or treated as the
active author contract.

Handlers perform only HTTP decoding, locale selection, status/error mapping,
and DTO serialization. The App service owns the bundled Profile registry,
current-workspace inspection, zero-write migration preview, bounds, and mapping
from Quality errors to stable application errors. Workspace and Profile
packages do not import App or API.

List/detail DTOs expose stable IDs, contract version, exact source hash, the
bound QualitySpec ID/revision/hash, read-only catalog access mode, and a bounded
bilingual summary. A Profile detail response is rejected during asset loading
if its exact source exceeds 1 MiB; the serialized public detail is capped at
256 KiB. The project singleton uses resource ID `current` and never invents a
portable workspace identity.

Project and preview DTOs omit absolute workspace paths, internal errors, and
raw marker bytes. The preview request accepts only `offset` (default `0`) and
`limit` (default `100`, maximum `500`) and always targets the App's current
workspace and Workspace Schema v1; it cannot accept an arbitrary filesystem
path. The response includes the digest and total count of the complete
zero-write preview, pages each deterministically sorted entry/operation/conflict
collection with that offset/limit, and reports truncation for each collection.
Preview items contain only relative paths, categories, sizes, hashes,
operations, and structured conflicts; they never contain file bytes.

## Alternatives

### Use `agent.Event` and arbitrary `Data` as the Quality domain contract

Rejected. It would make transport shape the domain, permit unbounded maps and
future default routing, and make exact schema/semantic validation impossible.

### Treat the existing Task snapshot as Run state and replay authority

Rejected. Task state is process memory and slow live subscribers are dropped.
Calling it durable would lose restart recovery and conflate display transport
with workflow truth.

### Implement a P1 Run repository or Quality run/action APIs now

Rejected. Deterministic Run state, checkpoints, recovery, decisions, and the
Run repository belong to P2-T03 and later author-controlled tasks. A fake
repository in P1 would create a competing contract and false persistence.

### Copy normative JSON into an internal runtime asset directory

Rejected. A copied bundle can drift into a second normative truth. Compile-time
embedding from the existing files preserves one byte source and removes
runtime-working-directory dependence.

## Consequences

- Quality events are exact, exhaustive, bounded, localizable, and stable across
  replay, while remaining independent of SSE and Agent internals.
- Sequence gaps become visible contract failures rather than silent UI loss.
- SSE reconnect can restore display without acquiring authority to execute or
  persist a Run.
- The minimal API is safe to ship before the Harness runtime because every
  endpoint is read-only or zero-write preview.
- Compile-time embedded assets increase the binary by the exact schema/example
  bytes but do not add a second copy to the repository or activate examples as
  workspace defaults.
- P2-T03 must supply the real durable Run state and stored event cursor behind
  the read port. Until then, P1 tests prove contracts and ordering with fakes;
  they do not claim crash recovery.
- A future event, summary argument, field, or Run action requires a new
  compatible contract decision rather than a v1 default branch.

## Migration

P1-T05 adds no existing Run or workspace data migration. Exact v1 events are
new records produced only after a later accepted runtime contract supplies a
durable producer. Unknown/newer event bytes are preserved for inspection and
rejected for v1 adaptation or replay; there is no automatic downgrade.

The migration-preview API calls the accepted Workspace Schema preview path and
performs zero writes. It does not create a marker, backup, stage, intent,
receipt, Projection, run file, or author confirmation. Any future event or API
contract change must publish a superseding ADR/schema, explicit compatibility,
previewable conversion where persisted bytes exist, and rollback before
rewriting stored events.

## Validation

- Draft 2020-12 meta-validation and exact JSON decoding accept one valid fixture
  for every v1 event type and reject malformed JSON, missing fields, unknown
  fields, unknown event/version values, invalid IDs/timestamps, wrong
  stage/artifact combinations, and summary overflow.
- Semantic tests cover every event type without a default branch and reject
  body/secret/free-text argument names or values outside their closed grammar.
- Sequence/replay tests cover the first event, valid continuation, gap,
  duplicate, regression, duplicate event ID, cross-Run input, immutable
  ID/time/sequence replay, and retained completion events.
- App adapter tests compare the complete validated envelope before and after
  `agent.Event` mapping and reject arbitrary or unknown Quality data.
- Reconnect tests use fakes, hooks, channels, and barriers to prove state read
  precedes subscription, reconnect does not re-execute, completed events remain
  replayable, and a dropped live delivery is not treated as lost state.
- API tests cover the four exact routes, no-workspace behavior, the exhaustive
  Profile catalog, unknown Profile ID, malformed/unknown/newer contracts,
  legacy-only `.nova`, split roots, unknown required features, bounded DTOs,
  stable error codes, no absolute-path/secret leakage, and preview zero-write.
- Asset tests verify that embedded bytes are byte-for-byte identical to the
  normative repository files and that production code does not use the runtime
  working directory.
- Static write audits prove that API, App, SSE, and event code cannot write
  `chapters/**`, setting/lore, Profile/QualitySpec, decisions, preferences,
  finalizations, runs, Projection, Git, or formal content.
- Every new leaf test completes in at most one second with zero skips; race,
  five-platform CGO-free compile/build, repository Go/frontend gates,
  `go mod tidy -diff`, `govulncheck`, and `git diff --check` retain their exact
  recorded exit status.

## Supersedes

None.

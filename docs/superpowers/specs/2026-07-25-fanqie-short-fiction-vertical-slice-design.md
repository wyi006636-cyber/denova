# Fanqie Short-Fiction Vertical Slice Design

Date: 2026-07-25

Status: Approved in conversation; awaiting review of this written specification

Branch: `feat/fanqie-short-vertical-slice`

Base capability: `ff12579` (`ReplaceFileWithConsistentSnapshot`)

## Outcome

Deliver one useful, end-to-end writing workflow before adding another platform layer:

1. the author enters a bounded brief and chooses a Markdown target;
2. Denova asks the currently configured Writing/IDE model for one complete Fanqie-style short story;
3. the author previews the complete candidate without changing workspace files;
4. only an explicit confirmation may replace or create the target Markdown;
5. the confirmed write and its version checkpoint run through the existing single-lease workspace seam;
6. Zhihu Salt later reuses the same workflow with a different bounded profile.

The historical golden-opening rules constrain the opening of the complete story. They do not reduce the first product slice to an opening-only generator.

## Product Surface

The persistent entry lives in the Writing Agent right panel and opens a single-column `Sheet`.

The Sheet contains three states:

- Brief: target path, current base revision, and the author brief.
- Preview: complete Markdown candidate, target metadata, model provenance, and an explicit confirm action.
- Result: exact write/checkpoint outcome with recovery guidance when only the checkpoint fails.

For an open Markdown file, that file is the default target. With no Markdown selection, the author must choose a visible workspace-relative `.md` path; Denova never silently chooses the first chapter. The author may override the default target.

The surface does not add a top-level menu and never writes `nova:content-mode`. Entering or leaving it therefore cannot switch Writing/Game mode. Desktop and mobile use the existing Agent panel and Sheet primitives rather than a third pane.

## Architecture

### Transport-neutral short-fiction core

Add a focused `internal/shortfiction` package for:

- the closed profile ID `fanqie_short`;
- bounded request, candidate, provenance, confirmation, and error types;
- deterministic candidate identity and validation;
- a small built-in Fanqie prompt seed;
- a `Generator` interface.

The package must not import `internal/quality`. It does not persist candidates, runs, receipts, or workflow state.

Candidate identity is a SHA-256 checksum over a fixed canonical structure containing profile, canonical workspace, normalized target path, base revision, Markdown, locale, profile version, model profile, and model. It detects accidental or client-side mutation; it is not an authorization token.

### Direct no-tool generation

Add one production adapter under `internal/agent` that reuses the current Writing/IDE model resolution and provider compatibility path. It makes one direct model `Generate` call with bounded messages.

It must not call the Agent builder, `/api/chat`, sessions, Skills, SubAgents, middleware, or any tool registry. The provider request contains neither `tools` nor `tool_choice`, so preview generation cannot mutate the workspace.

The prompt requests one complete Markdown story with no explanation or code fence. It requires a clear premise, protagonist desire, immediate pressure, an early understandable hook, and a payoff consistent with the brief.

### Application service

Add a focused short-fiction application service with two operations:

- generate a stateless, client-held candidate after validating the active canonical workspace, visible `.md` target, exact base revision, and source bounds;
- confirm the complete candidate after recomputing its identity and revalidating the active workspace and revision.

Generation performs no file write, ChangeSet creation, version creation, chat message write, or candidate persistence.

Confirmation calls `ReplaceFileWithConsistentSnapshot` exactly once. The replacement uses `OriginAgent` and `AutoAccept:true` because the author explicitly accepted the preview. The callback creates a manual version checkpoint while the same workspace lease remains held.

### HTTP boundary

Register only these endpoints:

- `POST /api/short-fiction/candidates`
- `POST /api/short-fiction/candidates/confirm`

Generation returns a full client-held candidate. Confirmation returns one of:

- `written`: Markdown and exact version checkpoint committed;
- `written_checkpoint_failed`: Markdown and ChangeSet committed, checkpoint failed.

The partial outcome is HTTP 200 because the request was understood and the content mutation did occur. It must include the committed `write_revision`, `workspace_mutated:true`, `checkpoint_status:"failed"`, and `retryable:false`. It must not offer a retry token or imply an idempotent durable receipt.

Validation, workspace identity, source revision, or upstream model failures return stable non-2xx errors with `workspace_mutated:false`. Provider secrets, base URLs, full prompts, and raw upstream errors are never returned.

### Frontend boundary

Keep the Agent panel integration thin and put the workflow in a focused feature component plus an explicit API client. Component state owns the brief and returned candidate only for the current page lifetime.

The UI must distinguish:

- preview loading and generation failure;
- empty or oversized model output;
- revision conflict before confirmation;
- confirmed write plus checkpoint;
- confirmed write with checkpoint failure.

On either committed outcome, refresh the affected workspace path through the existing workspace-change refresh hook. On checkpoint failure, tell the author that the Markdown was written and must be inspected before manually saving a version; do not present a generic retry button.

All visible copy is paired in `zh-CN` and `en-US`. Existing theme variables and components cover light/dark. The Sheet stays single-column at 1440x900 and 390x844, wraps long target paths, and scrolls long previews without horizontal overflow.

## Bounds and Configuration

Use fixed safety bounds above 128 KiB for the first slice:

- brief: 256 KiB;
- existing target/source: 256 KiB;
- generated candidate: 1 MiB.

Oversized input or output is rejected explicitly, never silently truncated. This slice adds no configuration item, model slot, directory convention, timeout, dependency, or migration. The existing Writing/IDE model configuration remains the single model choice.

## Error and Recovery Rules

- Unknown profiles are rejected without falling back to Fanqie.
- Missing targets require the literal base revision `missing`.
- Existing targets require their exact byte revision before generation and again before confirmation.
- A stale or switched workspace cannot be written.
- Candidate mutation is rejected before any write.
- A pre-commit error reports no mutation.
- A post-commit checkpoint error reports truthful partial success; there is no rollback claim because the content write is already durable.
- External programs that ignore Denova's workspace lease remain outside the managed atomicity guarantee.

## TDD Seams

Tests exercise public or stable seams, one vertical cycle at a time:

1. `internal/shortfiction`: deterministic candidate identity, bound-field tampering, profile rejection, and size bounds.
2. Direct model adapter with `httptest.Server`: configured IDE model/provider is used and the complete request has no tools or tool choice.
3. Application service: generation is no-write, stale revisions prevent model calls, tampering cannot mutate, confirmation creates the exact checkpoint, and checkpoint failure returns truthful partial success.
4. Public HTTP: preview then confirm, locale-specific errors, revision conflict, unknown profile, and HTTP 200 partial success.
5. Frontend feature with MSW: target selection, preview/confirm, long/empty/error/partial states, bilingual copy, and no content-mode write.
6. Real page: current hot-loaded Denova in zh-CN/en-US, light/dark, 1440x900/390x844, long preview, empty/error states, and mode preservation.

Every new leaf test must complete within one second and none may be skipped.

## Independent Prerequisites

Two repository fixes remain separate from this product slice:

- a focused security PR upgrades `golang.org/x/text` from `v0.38.0` to `v0.39.0` to address reachable `GO-2026-5970`;
- a focused interactive-protocol PR ports the audited six-file `03d9122` first-candidate fix.

Neither fix changes the short-fiction contract. Existing Draft PRs and the archived quality-harness branch remain unmerged and undeleted.

## Explicitly Deferred

- Quality Harness runtime, Quality page integration, projection, evaluation runs, and SSE progress;
- persistent Run, candidate, confirmation receipt, or retry repositories;
- workflow state machines, automatic invalidation, and Context Pack Builder;
- Candidate/Review/Preference UI and Author Finalization;
- Agent, Skills, tool, Automation, Tauri, or vector integration for generation;
- Zhihu Salt behavior until the Fanqie vertical slice passes its focused PR gates.

## Acceptance Boundary

Completion means the Fanqie slice can generate a complete preview through the configured model, preserve the workspace before confirmation, commit only after explicit confirmation, create or truthfully fail its exact version checkpoint, and pass automated plus real-page verification.

It does not mean the archived Quality Harness is merged, Phase 2 exists, Denova is Beta/release ready, or the product has a general short-fiction platform.

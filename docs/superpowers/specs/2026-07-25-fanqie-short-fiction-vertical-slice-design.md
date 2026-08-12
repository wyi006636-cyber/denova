# Fanqie Short-Fiction Conversational Workflow Design

Date: 2026-07-28

Status: Implemented as the primary workflow in PR #13; the earlier complete-story slice from PR #6 is retained as Quick mode

Product owner direction: one built-in Skill, one entry, and the existing Writing workbench

## Outcome

Deliver a useful author-visible loop inside the existing Writing workbench:

1. The author chooses **New Short Story** from Book Management.
2. Denova creates the normal writing workspace, opens the existing Writing Agent, and visibly selects `fanqie-short`.
3. The Agent discusses the idea with one to three adaptive questions at a time.
4. The Agent proposes a story plan and waits for confirmation.
5. After confirmation, the Agent proposes a chapter outline and waits again.
6. After outline confirmation, the Agent writes one chapter at a time into the existing editor and workspace through the existing Diff.
7. The author can continue by conversation, point to a passage, or select text, then accept or reject the resulting Diff.

The author never supplies a Markdown path in this primary flow. The existing **Complete Fanqie Short Story (Quick)** action remains available for authors who explicitly want one-shot generation.

## Product Surface

- Reuse Book Management's current book dialog and the current Writing workbench.
- Do not add a top-level menu, short-story page, second editor, or mode switch.
- Selecting **New Short Story** may initialize Writing mode because the author explicitly chose a writing creation action; shared top-level navigation must not switch modes.
- Open the existing Agent panel and show `fanqie-short` as the active built-in Skill.
- Reuse current chat history, editor, file tree, selection, versions, `read_file`, `write_file`, `edit_file`, and Diff accept/reject.
- Every new visible label, empty state, and error message must be paired in `zh-CN` and `en-US`.

## Single Skill Boundary

The product exposes only one built-in short-fiction Skill: `fanqie-short`.

Its entry file owns stage recognition and conversation progression. It loads only the method reference needed for the current stage:

- `references/story-concept.md`: premise, protagonist pressure, relationships, conflict, reversal, and ending direction;
- `references/short-structure.md`: short-story beats, escalation, chapter jobs, and outline hooks;
- `references/fanqie-style.md`: first-person prose, paragraph rhythm, dialogue, and chapter hooks;
- `references/chapter-writing.md`: bounded context reading, chapter drafting, workspace writing, and readback;
- `references/revision.md`: logic, common sense, motivation, continuity, dialogue, and flat-plot revision.

The Skill distills useful methods from the existing `lore-init`, `outline`, `continue`, `rewrite`, and `novel-lite` Skills and the former Fanqie system prompt. It does not expose those Skills as separate required product steps and does not start subagents.

## Conversation Stages

### 1. Discuss the idea

- Start from the title, idea, and emotion already provided by the author.
- Ask only the one to three missing questions most likely to change the conflict, reversal, ending, or length.
- Do not use a fixed questionnaire or ask the author to repeat known information.

### 2. Confirm the story proposal

When information is sufficient, propose:

- one-sentence story;
- two to four important character relationships;
- core external and internal conflict;
- main reversal with prior setup;
- ending direction and price paid by the protagonist.

Wait for explicit confirmation or revision. Do not write an outline or workspace content before confirmation.

### 3. Confirm the chapter outline

After proposal confirmation, provide the complete chapter outline. Every chapter identifies its job, immediate conflict, change or revelation, consequence, and concrete end hook. Wait for confirmation again.

### 4. Write chapter by chapter

After outline confirmation:

- write the confirmed outline to `setting/outline.md` through the existing tool and Diff;
- determine the next chapter path from the existing workspace naming convention;
- read only the confirmed outline, the latest one or two chapters, and directly relevant bounded setting context;
- draft one chapter per turn unless the author explicitly requests more;
- write through `write_file`, read back key passages, and leave the proposed change in the existing Diff.

When the author explicitly requests multiple chapters in one turn, the Agent still processes them sequentially. Before each file, it separates the current chapter's required events from the next chapter's first irreversible action, decision, or result. It writes, reads back, and brings the current file to the confirmed length before resetting that boundary for the next chapter; it must not draft the whole range and split it afterward. Foreshadowing later pressure is allowed, but the current chapter cannot complete a later chapter's decisive result, climax, or ending.

This corrects a reproduced prose defect in which a multi-chapter request realized later outline beats early and then repeated the climax or ending. It adds no workflow state, backend service, or review chain and stays within fewer than 20 production-content lines.

### 5. Revise through conversation or selection

- Treat selected text and its file as the direct edit target.
- When the author points to a passage, read the real source before editing.
- Use `edit_file` for the smallest sufficient change; use `write_file` only for an intentional whole-chapter rewrite.
- Tell the author that the change is pending in Diff. Never claim a failed tool call changed the manuscript.

## Writing Quality Contract

- Default to first person unless the confirmed proposal chooses another viewpoint.
- Establish the protagonist, immediate goal, credible pressure, concrete stakes, and story promise in the opening scene.
- Default to eight chapters and roughly 24,000-40,000 Chinese characters total. Each chapter targets 3,000-5,000 characters; explicit author instructions override the default.
- Every chapter must change the action conditions through conflict, consequence, loss, choice, or revelation.
- A local victory must expose a weakness, consume a resource, damage a relationship, or force a harder commitment.
- An opposing character or pressure source responds according to an established interest and capability; conflict must not depend on sudden accidents, implausible platform behavior, or a newly invented institution.
- Dialogue must pursue different character goals and change power, information, or action. Remove greetings and shared-information exposition.
- When the author says the plot is flat, first offer one causal three-to-five-step escalation chain using existing people, relationships, and resources; revise only after confirmation.

## Context and Tool Boundaries

- Stage is inferred from the existing conversation, confirmed content, and workspace files; do not add a backend workflow state machine or a parallel confirmation protocol.
- Model context is incrementally assembled and bounded. Use the outline, selection, recent chapters, or a relevant excerpt instead of unbounded manuscript history.
- Keep display history separate from model context; tool cards, logs, and thinking previews do not become prose context by default.
- Reuse the current Writing/IDE model configuration and its configurable timeout. Do not add a short-fiction-only model service.

## Existing Write Guardrails

No new safety pipeline is introduced. The existing three user-data protections remain sufficient:

| Threat source | Concrete loss | Existing protection |
|---|---|---|
| The author rejects or has not reviewed generated prose | Unwanted prose becomes manuscript content | Existing Diff preview with accept/reject |
| The manuscript changes between proposal and acceptance | A newer local edit is overwritten | Existing revision conflict check |
| The author later regrets an accepted change | Desired manuscript content is lost | Existing version rollback |

## Quick Mode

PR #6's complete-story candidate Sheet remains a secondary, explicit Quick mode:

- the author enters a short brief;
- Denova automatically chooses an unused `chapters/short*.md` target;
- the configured Writing model generates one complete candidate without tools;
- the author previews it and explicitly confirms before the existing snapshot seam writes it.

Quick mode may keep its smaller one-call length profile. It is not invoked by `fanqie-short`, is not the default **New Short Story** flow, and must not reintroduce a Markdown-path field.

## Implementation and Acceptance Status

| Requirement | Implementation | Evidence status |
|---|---|---|
| New Short Story reuses the Writing workbench | Implemented | Merged in PR #13 |
| `fanqie-short` is selected and visible | Implemented | Merged and previously observed on the real page |
| Proposal then outline confirmation | Implemented in Skill instructions | Previously completed on the real page |
| First chapter enters editor/workspace Diff | Implemented with existing tools | Previously written, rejected, rewritten, and accepted on the real page |
| Pointed passage can be revised through Diff | Implemented with existing tools | Previously edited and accepted on the real page |
| Direct mouse-selection revision | Reuses existing selection support | Verified on the real page: a mouse-selected sentence in chapter 8 produced a focused `+1/-1` Diff |
| Full eight-chapter conversational story | Mechanism implemented | Verified on the real page with all eight chapters present after refresh |
| Multi-chapter requests preserve chapter boundaries and confirmed length | Implemented in `fanqie-short` guidance | Verified on the real page with `方案署名不是我`: chapters 1-3 stopped at their confirmed hooks, no chapter 4 or later resolution was created, and the reproduced 2,117-2,262-character drafts were expanded sequentially to 3,028-3,107 characters without crossing those boundaries |
| Chinese/English, light/dark, narrow/wide entry rendering | Copy and adaptive components implemented | Verified in both languages and themes, including a 390px viewport |
| Complete-story Quick mode | Implemented | Merged in PR #6 and retained |

Passing unit tests or receiving model text is not completion. Acceptance requires real prose written into the real editor, reviewed through Diff, and still present after refresh.

## Archived Quality Harness Asset Policy

The closed PR #1 and `feat/quality-harness-foundation` branch remain recoverable history. They are not a product dependency and must not be merged wholesale.

| Classification | Assets | Policy |
|---|---|---|
| Keep in main | Writing workbench, write/edit tools, selection, Diff, versions, workspace snapshot seam, Quick mode, extracted interactive candidate-order fix | These already serve author-visible behavior. |
| Extract only for a named need | Quality profiles and rubrics, selected hand-written examples, a specific reproduced correctness fix | Name the user-visible consumer first, then port the smallest coherent slice. |
| Archive on branch/PR | `internal/quality` runtime, evaluation engine, projection, repositories, Quality UI, `cmd/quality-eval` | Preserve for reference; do not maintain or integrate now. |
| Do not restore to main | Generated discovery JSON, run output, caches, bulk evaluation artifacts, large generated fixtures | Regenerate if a future approved task genuinely needs them. |

Past line count is not a reason to merge code. A future extraction should normally remain within the 500-line first-version target and must demonstrate one author-visible capability that did not exist before.

## Explicit Non-Goals

- No independent short-story workbench, page, editor, or top-level menu.
- No short-fiction-only backend generator, durable candidate repository, workflow state machine, new confirmation protocol, file-security pipeline, dependency, or migration.
- No Quality Harness, automatic ranking scan, crawler, evaluation platform, benchmark system, Context Pack Builder, or general Quality page.
- No reviewer, fixer, final-gate, or multi-agent review chain.
- No unrelated refactor, dependency upgrade, release preparation, compatibility project, or publication action.
- No Zhihu Salt mode until the Fanqie conversational workflow has demonstrated enough real author value to justify another profile.

## First-Version Budget

The merged conversational implementation remains the product slice. This specification closeout and chapter-boundary refinement adjust Skill references and bilingual changelog copy but add no new runtime architecture. Expected tracked change is documentation plus fewer than 20 production-content lines, well below the 500-line production target.

## Historical Note

The previous version of this file described the complete-story candidate/confirm vertical slice. That implementation was merged in PR #6 and remains available as Quick mode. Git history and PR #6 preserve its detailed transport, candidate, confirmation, and snapshot design; those details are no longer the primary short-story product contract.

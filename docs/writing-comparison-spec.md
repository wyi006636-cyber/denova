# 多模型章节比较与合成规格

## Problem Statement

作者目前可以在写作会话中切换一个模型，也可以用开发评测工具比较不同模型的调用结果，但不能在当前作品的同一章任务上，让多个模型分别生成候选稿，再在作品上下文不变的前提下比较、选段、合成并安全写回。

这会带来几个实际问题：作者需要手工复制上下文和提示词，难以保证候选之间公平；候选结果容易混入正式章节或普通写作会话；模型评分无法追溯到具体证据；作者无法把一个模型的对白和另一个模型的剧情设计组合起来；生成完成后也缺少断点恢复、单候选重试和正式章节 revision 冲突保护。

## Solution

新增面向作者的“比较任务”。作者为一个明确目标章节配置 2–4 个候选生成模型，每个候选可选择自己的写作模板/Skill 和附加要求。任务创建时复用现有 Writing Agent 的上下文解析，冻结实际使用的文件、Lore、章节游标、修订号和内容快照。

每个候选在隔离且只读的执行范围内生成不可变候选稿。候选完成后由独立的评审/整理模型按可调整的评分维度给出带证据的评分；作者可以从 2–4 个候选中选择比较对象、阅读差异、选取片段并写备注。评审/整理模型再把作者的选择整理成可编辑的综合提示词，最终书写模型据此生成合成稿。

候选、评分、选段、综合提示词和合成稿均属于持久化比较任务，正式章节及进度、角色状态、Lore 在生成和评审阶段保持不变。作者确认后，合成稿通过现有 revision/Change Review 流程应用；目标章节已发生修改时阻止覆盖。应用成功后，按正常写作流程同步必要的连续性状态。

## User Stories

1. As an author, I want to create a comparison task from a chapter editor or chapter task card, so that I can compare models without leaving the writing workflow.
2. As an author, I want to specify an exact target chapter path, so that the system never guesses which chapter should receive the result.
3. As an author, I want to provide a chapter task and creative requirements, so that every candidate responds to the same goal.
4. As an author, I want to choose between two and four registered Writing Profiles, so that I can compare the models I actually have configured.
5. As an author, I want each candidate to have its own writing template or Skill, so that I can compare different writing approaches as well as different models.
6. As an author, I want to add candidate-specific requirements, so that one candidate can emphasize dialogue while another emphasizes plot structure.
7. As an author, I want the task to show the expected number of candidate, review, synthesis, and final-writing calls, so that I understand the work before starting it.
8. As an author, I want the system to capture the Writing Agent's resolved context when the task starts, so that every candidate sees the same chapter task and story facts.
9. As an author, I want the snapshot to record source revisions, Lore identities, chapter cursor, truncation and omissions, so that I can understand exactly what each candidate knew.
10. As an author, I want candidate generation to leave the formal chapter and continuity files unchanged, so that experimentation cannot damage my manuscript.
11. As an author, I want candidates to run serially by default, so that local models that cannot run concurrently still work reliably.
12. As an author, I want supported environments to opt into bounded parallel execution, so that I can reduce waiting time without making concurrency a correctness requirement.
13. As an author, I want one candidate to fail without discarding successful candidates, so that a provider problem does not force an expensive full rerun.
14. As an author, I want to retry one failed or incomplete candidate, so that completed candidates and their scores remain available.
15. As an author, I want to pause, cancel, resume, and leave a running task, so that the task follows my workflow instead of the page lifecycle.
16. As an author, I want a restarted application to mark unfinished model work as incomplete rather than replay it automatically, so that paid or side-effecting calls are never silently duplicated.
17. As an author, I want each candidate's model, runtime, template, Skill and custom requirement shown with its result, so that I can explain why the drafts differ.
18. As an author, I want candidate content to be immutable, so that scores and selections remain tied to the exact text that was reviewed.
19. As an author, I want an independent review and synthesis model, so that the model judging the drafts can be chosen separately from the candidate writers.
20. As an author, I want a user-level default review Profile, so that I do not have to configure the scoring model for every task.
21. As an author, I want to override the review Profile for one task, so that an individual comparison can use a more suitable judge.
22. As an author, I want default review dimensions for plot progression, character motivation, continuity, pacing, prose, dialogue, and chapter hook/payoff, so that scoring starts with useful criteria.
23. As an author, I want to adjust the review dimensions for a task, so that genre-specific or chapter-specific priorities are respected.
24. As an author, I want every model score to include evidence from the frozen context or candidate text, so that a score is inspectable rather than an unexplained ranking.
25. As an author, I want later candidates to receive additional review records without rewriting earlier scores, so that the history remains auditable.
26. As an author, I want to override or ignore a review result, so that automatic judgment never becomes canon by itself.
27. As an author, I want to choose two to four completed candidates for a comparison view, so that I can focus on the drafts relevant to my decision.
28. As an author, I want to read selected candidates side by side and inspect differences, so that I can compare prose and structure directly.
29. As an author, I want to compare more than four candidates in batches, so that the interface remains readable without limiting the task history.
30. As an author, I want to highlight a passage in a candidate, so that I can preserve a strong line, scene beat, or piece of dialogue.
31. As an author, I want each selected passage to retain its candidate revision, hash, UTF-8 range, and original text, so that a retry cannot silently move my selection.
32. As an author, I want to attach notes and corrections to selected passages, so that I can explain what should be retained or changed.
33. As an author, I want to select useful material from several candidates, so that the final chapter can combine strengths instead of choosing only one draft.
34. As an author, I want the review and synthesis model to turn my selections into structured scores, evidence, integration guidance and writing constraints, so that the next writing step has a coherent brief.
35. As an author, I want to edit the synthesis result before generation, so that the model's interpretation remains under my control.
36. As an author, I want to choose a final writer independently of the candidate and review models, so that I can use the best model for final prose.
37. As an author, I want the final writer to produce a composition draft without modifying project files, so that I can inspect it before committing it.
38. As an author, I want the composition draft to retain the input snapshot and synthesis revision it used, so that its provenance is clear.
39. As an author, I want the composition to enter the existing Change Review flow, so that I can review the exact change before it touches the formal chapter.
40. As an author, I want a revision conflict to block application rather than overwrite newer writing, so that my intervening edits are protected.
41. As an author, I want a successful application to be linked to the comparison task, so that I can trace which candidates and synthesis produced the chapter.
42. As an author, I want normal progress, character-state and necessary Lore synchronization after applying a chapter, so that continuity remains current.
43. As an author, I want comparison tasks separated from ordinary writing sessions, so that candidate runs do not clutter my conversation history.
44. As an author, I want to return to unfinished comparison tasks from a Writing workbench list, so that I can continue after switching projects or closing the page.
45. As an author, I want to archive a completed task and restore it later, so that my history stays organized without losing evidence.
46. As an author, I want permanent deletion to require explicit confirmation and remove all comparison assets, so that cleanup is deliberate and predictable.
47. As an author, I want prompt templates migrated from Yanzhou to be owned and versioned by Denova, so that the feature remains reproducible without another checkout.
48. As an author, I want unsupported model profiles to be excluded with a clear explanation, so that the task never silently switches to a different model.

## Implementation Decisions

- The feature is a Project-owned comparison task, not a special ordinary chat session and not a developer-only evaluation run.
- The task has a dedicated Project Store domain and canonical JSONL. Candidate content, context snapshots, reviews, selections, synthesis revisions and composition revisions are task assets; directory scans are rebuildable indexes only.
- The task stores two related state dimensions: business phase (configuration, generation, partial completion, completion, synthesis, pending application, applied, archived) and execution state (queued, running, paused, cancelled, incomplete). Every transition is persisted.
- A task freezes the Writing Agent's resolved input: selected project files, revisions, Lore entries and versions, chapter cursor, chapter task, Skill/template version, content, budget and omission records. Candidates never replace this snapshot with live project reads.
- Candidate drafts have stable identities and immutable revisions. A retry appends a new revision; selection anchors bind to a candidate revision hash, UTF-8 byte range and original text.
- Candidate configuration snapshots contain the selected Writing Profile/runtime, migrated template or Skill identity/version, custom requirement and scheduling position. Secrets are resolved from existing settings and never copied into task data.
- Candidate execution is orchestrated in the application layer and uses the existing runtime/profile adapters with a read-only tool contract. Native runtime internals do not receive comparison-task concepts, external runtime selection or a second Goal/Todo system.
- Candidate scheduling defaults to serial execution. Parallelism is explicit, bounded, capability-checked and optional; one candidate failure does not cancel successful candidates.
- An independent review and synthesis model is selected from the available Writing Profiles. A user-level default is available; task-level changes are saved as snapshots.
- Review output is structured and immutable: dimensions, scores, evidence anchors, rubric version, input candidate revisions and model snapshot. Later candidate completion appends a new review record instead of changing an old one.
- Default review dimensions cover plot progression, character motivation, continuity, pacing, prose, dialogue and chapter hook/payoff. Authors can edit the dimension set per task. Scores are advisory and cannot select canon automatically.
- The comparison view accepts two to four completed candidates at a time, supports side-by-side reading, selection and difference inspection, and batches larger sets.
- Selection input stores the candidate revision, content digest, UTF-8 byte range, original excerpt and author note. Invalid anchors remain visible and require re-selection; no fuzzy relocation is performed.
- The review and synthesis model produces structured scoring, evidence, integration guidance, constraints and an editable synthesis prompt. This is an intermediate instruction artifact, never chapter prose.
- The final writer is selected independently and receives the frozen input snapshot, the selected material and an author-approved synthesis revision. It produces a composition revision without writing project files.
- Applying a composition uses the existing revision-aware Change Review path. Existing and new target files are supported; an observed target revision mismatch blocks application and preserves the task assets.
- After author confirmation, the normal writing-state synchronization path updates progress, character state and only the Lore changes permitted by existing writing rules.
- The Writing workbench provides creation from a chapter editor or chapter task card, a task list, task detail, candidate comparison, selection input, synthesis editing, composition preview and Change Review application.
- The task remains runnable when its page is closed. On restart, unfinished calls become `incomplete` and are not replayed automatically; the author resumes or retries explicitly.
- Archived tasks can be restored. Permanent deletion requires explicit confirmation and removes task assets without touching formal chapter files.
- Yanzhou prompt components are an audited migration source only. The first migration covers full-chapter writing/continuation, opening/chapter-task writing, consistency/foreshadow review, and composition guidance. Denova Skills/resources are the only runtime source and keep stable versions.
- The primary application seam is one Project-scoped Writing Comparison Service/API driven by a deterministic model fixture. The browser layer verifies visible wiring and the author flow; only small pure helpers receive isolated tests where they define UTF-8 anchors or structured schema validation.

## Testing Decisions

- The main test must exercise external behavior through the Project-scoped comparison Service/API, not internal helper calls. A deterministic model fixture should run the whole path: create task, freeze context, generate two candidates, persist partial failure and retry, review, select material, synthesize, compose, create Change Review and apply.
- The integration fixture must assert that formal chapter, progress, character state and Lore bytes remain unchanged until explicit application.
- The integration fixture must assert that all candidates use the same input snapshot hash while retaining different model/template/Skill snapshots.
- The store and journal tests must cover replay after restart, append-only lifecycle transitions, CAS/revision conflicts, immutable candidate revisions, archive/restore and confirmed permanent deletion.
- Scheduler tests must cover default serial execution, bounded opt-in parallel execution, task pause/cancel, candidate-level retry, one failed candidate not blocking others, and incomplete recovery without automatic replay.
- Context snapshot tests must use the existing Writing context resolver and verify selected sources, revisions, content hash, omission records and stable snapshot identity.
- Review tests must verify schema rejection, evidence references, configurable dimensions, incremental review records and independent author overrides.
- Selection tests may be pure because UTF-16-to-UTF-8 conversion and anchor validation are deterministic; they must assert content-hash mismatch invalidates a selection rather than relocating it.
- Change Review integration tests must verify both existing-file replacement and new-file creation, target revision conflict rejection, task provenance, and no mutation on rejected application.
- Frontend unit tests should cover candidate selection limits, task-state presentation, invalid anchors, synthesis editing and localized errors. They should assert user-visible behavior rather than component internals.
- Browser E2E should open the Writing workbench, create a task from a chapter, run deterministic candidates, compare and select text, edit the synthesis prompt, preview the composition and complete Change Review. It should cover narrow and wide layouts, long candidate text, partial failure and an empty target file.
- Existing change review, version conflict, writing Agent, model Profile and creative-evaluation fixtures are prior art. New tests should reuse their project setup, deterministic provider and API event patterns rather than introduce a parallel harness.
- Run focused Go tests and `go vet` during implementation; expand to affected Go modules and frontend unit/browser/E2E/i18n checks after cross-package and UI integration changes. Pure documentation changes only require static checks.

## Out of Scope

- Comparing multiple chapters in one task.
- Unlimited candidates, unlimited concurrency, real-time collaborative editing or cross-user sharing.
- Automatic selection of a winning candidate or automatic conversion of a score into canon.
- Candidate, review or synthesis writes to formal chapters, progress, character state or Lore before author confirmation.
- Direct overwrite that bypasses revision checks or Change Review.
- Treating each candidate as a normal writing conversation.
- Runtime dependency on Yanzhou files or Yanzhou business-data formats.
- Billing or monetary cost prediction when the provider cannot reliably report it.
- A second writable Goal/Todo implementation for comparison tasks.
- Claiming model scores, automated checks or fixture tests prove literary quality or user acceptance.

## Further Notes

- The accepted domain vocabulary is recorded in the project glossary: comparison task, candidate draft, candidate configuration, candidate generator, formal chapter, selected material, composition, selection input, synthesis prompt, review and synthesis model, final writer and writing input snapshot.
- The accepted architectural boundary is recorded in the ADR for isolated immutable candidates and Project Store canonical JSONL.
- The recommended delivery order starts with a small vertical slice: two candidates, serial execution, immutable storage, whole-draft comparison, whole-draft selection and Change Review application. Scoring, selection anchors, synthesis editing, final-writer separation and optional parallelism follow after the storage and safety path is proven.
- Prompt migration from Yanzhou should be audited against Denova's `novel-lite` and `novel-standard` workflows to avoid injecting duplicate or conflicting writing rules.
- The planned issue must receive the configured tracker label corresponding to `ready-for-agent` once tracker setup is complete.

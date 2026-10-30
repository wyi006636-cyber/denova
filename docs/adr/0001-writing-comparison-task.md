# Writing comparison tasks use isolated immutable candidates

**Status: accepted**

Denova will implement multi-model chapter comparison as a project-owned comparison task, not as a special ordinary chat session or a developer-only evaluation run. A task freezes the Writing Agent's resolved input snapshot, runs 2–4 independent candidate generators with their own model/template/Skill configuration snapshots, keeps candidate content immutable, and stores task state in a dedicated Project Store canonical JSONL. Candidate generation, review, synthesis, and composition do not modify the formal chapter or continuity files; an author must explicitly apply the composition through the existing revision-aware Change Review path before normal writing-state synchronization can occur.

This boundary is deliberate. Separate task storage keeps comparison history out of ordinary writing-chat history and makes restart, retry, archive, and deletion recoverable. Immutable input and candidate revisions make scores and selected UTF-8 ranges reproducible even when the live project changes. A dedicated review/synthesis model and a separately chosen final writer preserve the author's ability to use different strengths without letting an automatic rank silently decide canon. The scheduler defaults to serial execution because local models may not support concurrent calls; opt-in parallelism is an optimization, not a correctness requirement. Yanzhou prompt material is migrated and versioned into Denova-owned Skills/resources after audit; runtime never depends on another checkout.

## Consequences

- The Project Store needs a comparison-task domain with a canonical journal plus immutable snapshot and candidate artifacts.
- The application layer owns orchestration and runtime/profile selection; the Native Agent Runtime remains unaware of comparison-task product semantics.
- The feature must expose explicit incomplete, retry, revision-conflict, archive, and permanent-delete states rather than inferring them from directory scans.
- Applying a composition is a normal project mutation with a revision conflict gate. It cannot be implemented as a direct file overwrite or by silently replacing the active Writing session.

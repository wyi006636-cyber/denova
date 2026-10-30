# Writing Comparison Context

This context defines the author-facing language for comparing independent AI drafts of a chapter inside a Denova writing project. It covers the comparison task, its candidate drafts, and how an author turns selected material into the formal chapter.

## Writing comparison

**Comparison task**:
One author-initiated operation for one target chapter that uses one frozen set of writing inputs to produce multiple independent candidate drafts. The first version compares one chapter at a time.

**Candidate draft**:
A complete chapter draft produced for a comparison task by one model configuration. A candidate keeps its generating model, prompt, and writing Skill as part of its identity. Avoid: version, final draft.

**Candidate configuration**:
The model, prompt, and writing Skill chosen for one candidate draft. Avoid: global model choice.

**Candidate generator**:
The model configuration that writes one candidate draft from the writing input snapshot. A comparison task may have several candidate generators, and each candidate keeps its own configuration snapshot.

**Formal chapter**:
The chapter file that belongs to the project and represents the author's current authoritative prose. A comparison task does not change it until the author explicitly applies a result.

**Selected material**:
Text the author marks as useful from one or more candidate drafts, such as dialogue, a plot development, or a scene passage. Selected material may be used in a later composition and does not itself become canon.

**Composition**:
The author's work of combining selected material, revising it, and turning it into the next formal chapter draft. Avoid: merge, which implies an automatic conflict-free operation.

**Selection input**:
The author's accepted excerpts, notes, and corrections attached to a candidate or comparison task. It records what the author wants carried into composition, not a claim that the selected text is already final prose.

**Synthesis prompt**:
A model-produced instruction that organizes the author's selection inputs into a coherent brief for the final writer. It is an intermediate instruction artifact, not chapter prose.

**Review and synthesis model**:
The independently chosen model responsible for scoring candidate drafts and turning the author's selection inputs into a synthesis prompt. It is separate from the candidate generation models and may be the same configuration as the final writer.

**Final writer**:
The model configuration chosen to turn the synthesis prompt and frozen writing context into a composition draft. It may be the same model used for review or candidate generation, but those roles are conceptually separate.

**Writing input snapshot**:
The chapter task and project writing context captured for a comparison task so every candidate starts from the same author-approved basis. Avoid: live context, when referring to the frozen basis of a running comparison.

---
name: novel-market-research
description: Research current web-fiction market signals and platform-specific opening techniques from public rankings, book pages, catalogs, and readable chapters; use when choosing a track, benchmarking live titles, or turning platform evidence into a writing brief.
category: research
capabilities:
  - writing-workflow
agent: ide
---

# Novel Market Research

Turn current public platform evidence into a writing decision, not a generic trend summary. Separate what the market pages show, what the sampled prose demonstrates, and what you infer from those observations.

Use the user's language in the report. Do not create or modify files unless the user asks for a saved artifact.

## Define the decision

Before searching, identify:

- target platform, audience channel, and genre or track;
- the decision the research must support, such as selecting a track, shaping a premise, or designing an opening;
- freshness window and snapshot date;
- requested deliverable and file path, if any.

If the request is broad, choose one mainstream platform and one coherent track as a stated test assumption. Do not mix unrelated genres into one set of writing rules.

When the user asks to compare many platforms, define a bounded mainstream set and say that it is representative rather than literally exhaustive. Group platforms before comparing them by audience channel, monetization model, and track. Never pool male-oriented and female-oriented fiction, or free-reading and paid-reading markets, into one writing formula.

Use these default coverage targets when the user gives none:

- at least 5 comparable titles for the market snapshot;
- at least 3 titles with publicly readable opening prose for craft analysis;
- the current ranking snapshot plus book pages, catalogs, and up to the first 3 public chapters per title.

Keep prose samples aligned to the same audience and track. A cross-platform fallback must still match the target track; if three comparable openings are unavailable, report incomplete craft coverage instead of combining unrelated genres into one pattern.

## Compare multiple platforms

Use two layers so breadth does not erase evidence quality:

1. **Coverage scan:** for every platform, read a current official ranking or category page, record 2-3 visible titles, and check whether official opening chapters are publicly readable.
2. **Comparable craft sample:** choose one audience and track, then analyze prose only on platforms where at least 3 same-track openings are readable.

Classify each platform as:

- `market + craft`: current market pages and at least 3 comparable openings were read;
- `market-only`: current market pages were read, but comparable prose coverage was insufficient;
- `access-limited`: even the current official market surface could not be verified.

If the user explicitly asks about writing methods, a breadth-only map is not a completed result. Before producing a writing decision card, at least one platform must reach `market + craft`. If none does, keep the platform set bounded but try the remaining materially different official routes or another comparable mainstream platform. If no platform reaches the threshold, report the writing research as incomplete instead of presenting an all-`market-only` report as delivery.

Every `market + craft` classification must include an evidence manifest for at least 3 same-track titles. For each title, list the official work page and every opening-chapter URL actually read, plus a short paraphrase of the first concrete scene event. A catalog entry, chapter title, synopsis, search snippet, or claimed prior read without this manifest does not count toward the threshold.

For each platform record its official discovery route, sample date, visible audience/track signals, chapter accessibility, and evidence class. If a platform is app-first, personalized, login-only, or blocked in the current environment, state that limitation instead of substituting search snippets or third-party summaries.

Compare only like with like. A valid cross-platform craft matrix holds the audience and track constant, then separates:

- patterns observed across at least 3 platforms;
- patterns observed only on one platform;
- hypotheses that still need a larger or readable sample.

Do not rank platforms by quality from a single snapshot. Monetization and recommendation models may explain different packaging or pacing, but label that as inference unless official guidance or direct evidence supports it.

## Collect evidence

Use `web_search` to discover candidate pages, `web_fetch` to read accessible pages, and `browser` only when an important official page requires rendered interaction. Do not treat search-result snippets as final evidence.

Prefer evidence in this order:

1. current official platform rankings or category lists;
2. official book pages with title, author, tags, status, word count, readership, synopsis, and update time;
3. official public catalogs and readable opening chapters;
4. official author guidance for platform rules or submission mechanics;
5. independent reporting only for market context that the platform pages cannot establish.

For every sampled title, record the exact source URL and which fields were actually visible. If a ranking is personalized, rolling, or calculated over a disclosed period, label the snapshot accordingly.

Do not use ranking position alone as proof of literary quality. Do not use a synopsis, tag list, review, or chapter title as proof of prose technique. If an opening is not publicly readable, keep the title in the market sample but exclude it from prose-level claims.

When access is blocked or content is empty, try one materially different route: an official alternate page, direct public chapter URL, or rendered browser. Then mark the gap and continue; do not loop on the same page. Do not download or decode anti-scraping fonts, reverse-engineer protected content APIs, or build a site-specific scraper or bypass. If another platform supplies a useful comparison, label it as separate evidence and never present its craft patterns as rules for the blocked platform.

## Analyze the market sample

Build a compact comparison table using only fields supported by the pages:

- title and author;
- list, category, or discovery route;
- serialization status, word count, readership, score, or update time when shown;
- premise promise and visible tags;
- source link and access date.

Identify recurring promises, protagonist starting positions, fantasy mechanisms, emotional payoffs, and visible audience signals. Treat repeated marketing language as positioning evidence, not as a verdict about the actual story.

## Analyze the opening craft

Read enough public prose to reconstruct the opening movement, normally no more than the first 3 chapters. Paraphrase rather than reproduce substantial copyrighted text.

For each readable opening, inspect:

- first-screen reader promise and the concrete situation that carries it;
- protagonist's immediate desire, pressure, cost, and available action;
- trigger timing and how much setup appears before it;
- scene entry, information release, dialogue-to-exposition balance, and conflict escalation;
- first concrete payoff, reversal, or proof of the core mechanism;
- chapter-end question or unfinished action that pulls the next click;
- whether the prose delivers the promise made by the title and synopsis.

Use observable scene evidence. Avoid vague labels such as “fast-paced,” “immersive,” or “highly emotional” unless the report explains what caused that effect.

## Derive patterns without overclaiming

- Call something a recurring pattern only when it appears in at least 3 independent sampled titles.
- Label conclusions as `Observed`, `Inference`, or `Unverified`, and give each major conclusion High, Medium, or Low confidence.
- Note counterexamples and meaningful differences between successful openings; do not average every title into one formula.
- Separate platform fit from originality. A common mechanism may signal reader familiarity while also creating imitation risk.
- Do not infer what causes ranking, retention, or commercial performance from one ranking snapshot. Such causal claims require historical or behavioral evidence.
- Never invent rankings, readership numbers, chapter events, dates, or source links to fill missing coverage.

If fewer than 3 openings are readable, return a valid market snapshot and a clearly limited craft hypothesis. Do not present it as a platform writing law.

## Produce a writing decision card

End with an actionable brief that a writer can use immediately:

1. target reader and one-sentence reading promise;
2. proven market conventions worth retaining;
3. crowded elements that need a meaningful difference;
4. protagonist, pressure, mechanism, and first visible payoff;
5. a beat map for the opening screen and first 3 chapters;
6. specific opening pitfalls suggested by the sampled evidence;
7. the smallest next writing test, such as two contrasting opening auditions.

Include only rules supported by the target-platform evidence. Convert craft evidence from another platform into explicit hypotheses or contrasting opening auditions, not target-platform rules. Do not write the novel or silently choose a final premise unless the user asks. Research should narrow a creative decision while preserving the writer's choice.

## Report format

Return:

- scope, assumptions, snapshot date, and coverage achieved;
- for multi-platform work, a coverage matrix and the evidence class for every platform;
- market sample table with claim-adjacent source links;
- opening-craft comparison based only on readable, same-track prose;
- recurring patterns, counterexamples, confidence, and evidence gaps;
- the writing decision card;
- a short source list containing only pages actually read.

When the user requests a file, write the complete report to that path and read back its key sections before claiming completion.

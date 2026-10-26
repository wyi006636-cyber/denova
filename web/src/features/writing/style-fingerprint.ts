/**
 * Deterministic, browser-only prose statistics. Nothing in this module
 * writes project files or calls a model; callers decide which chapters to
 * provide and can use the returned Markdown as a copyable draft.
 */

export const STYLE_FINGERPRINT_LIMITS = {
  maxChapters: 12,
  maxCharacters: 200_000,
  maxRepeatedPhrases: 12,
  repeatedPhraseMinimumCount: 3,
  repeatedPhraseMinimumLength: 2,
  repeatedPhraseMaximumLength: 4,
  // Fragment filtering is linear after this cap; keep a bounded candidate
  // set when a repetitive 200k-character sample produces many phrases.
  maxRepeatedPhraseCandidates: 4096,
} as const

/**
 * A phrase is filtered only when the complete phrase is a function word.
 * Filtering individual characters would hide useful repetitions such as
 * 「看了看」 merely because they contain 「了」.
 */
export const STYLE_FINGERPRINT_FUNCTION_WORDS = new Set([
  '的', '了', '着', '过', '地', '得', '和', '与', '及', '或', '而', '但', '却', '也',
  '都', '还', '很', '更', '最', '在', '从', '向', '对', '把', '被', '让', '给', '将',
  '是', '有', '无', '不', '没', '未', '能', '会', '要', '可', '这', '那', '其', '他',
  '她', '它', '我', '你', '们', '一个', '一种', '然后', '但是', '因为', '所以',
  '如果', '就是', '已经', '还是', '虽然', '因此', '那么', '只是', '能够', '正在',
  '所有', '这些', '那些', '什么', '没有', '可以', '自己', '他们', '我们', '不是',
])

export interface StyleFingerprintChapter {
  path?: string
  label?: string
  content: string
}

export interface StyleFingerprintQuantiles {
  p50: number
  p90: number
  sampleCount: number
  unit: 'characters' | 'words'
}

export interface StyleFingerprintPunctuation {
  ellipsis: number
  dash: number
  exclamation: number
}

export interface StyleFingerprintPhrase {
  phrase: string
  count: number
}

export interface StyleFingerprintResult {
  chapterCount: number
  characterCount: number
  sentenceLength: StyleFingerprintQuantiles
  paragraphLength: StyleFingerprintQuantiles
  dialogueParagraphRatio: number
  dialogueParagraphCount: number
  paragraphCount: number
  dialogueRunAverage: number
  dialogueRunLongest: number
  punctuationPerThousand: StyleFingerprintPunctuation
  repeatedPhrases: StyleFingerprintPhrase[]
  truncated: boolean
  truncatedByChapters: boolean
  truncatedByCharacters: boolean
  requestedChapterCount: number
  skippedChapterCount: number
  elapsedMs: number
  truncatedChapterLabels?: string[]
}

export interface StyleFingerprintOptions {
  includeRepeatedPhrases?: boolean
  maxChapters?: number
  maxCharacters?: number
  skippedChapterCount?: number
  truncatedChapterLabels?: readonly string[]
}

export type StyleFingerprintMarkdownLocale = 'zh' | 'en'

const CJK_PATTERN = /[\u3400-\u9fff]/g
const WORD_PATTERN = /[A-Za-z]+(?:['’][A-Za-z]+)?/g
const CONTENT_PATTERN = /[\p{L}\p{N}\u3400-\u9fff]/u
const ATX_HEADING_PATTERN = /^\s{0,3}#{1,6}(?:\s+|$)/
// These abbreviations contain periods that must not end a sentence. Keep the
// list explicit so adding a new common form does not alter sentence parsing by
// accident.
const ABBREVIATION_PATTERN = /(?:Mr|Mrs|Ms|Dr|Prof|Sr|Jr|St|vs|etc|i\.e|e\.g|U\.S|U\.S\.A|a\.m|p\.m)\.$/i

/** Calculate the fingerprint for the supplied chapters in source order. */
export function computeStyleFingerprint(
  chapters: readonly StyleFingerprintChapter[],
  options: StyleFingerprintOptions = {},
): StyleFingerprintResult {
  const started = typeof performance !== 'undefined' ? performance.now() : Date.now()
  const maxChapters = options.maxChapters ?? STYLE_FINGERPRINT_LIMITS.maxChapters
  const maxCharacters = options.maxCharacters ?? STYLE_FINGERPRINT_LIMITS.maxCharacters
  const requestedChapterCount = chapters.length
  const accepted: string[] = []
  const computedTruncatedChapterLabels: string[] = []
  let characterCount = 0
  let truncatedByChapters = false
  let truncatedByCharacters = false

  for (let chapterIndex = 0; chapterIndex < chapters.length; chapterIndex++) {
    const chapter = chapters[chapterIndex]
    if (accepted.length >= maxChapters) {
      truncatedByChapters = true
      computedTruncatedChapterLabels.push(...chapters.slice(chapterIndex).map(chapter => chapter.label || chapter.path || '').filter(Boolean))
      break
    }
    const content = normalizeMarkdownBody(chapter.content || '')
    const available = maxCharacters - characterCount
    if (available <= 0) {
      truncatedByCharacters = true
      break
    }
    const contentLength = countNonWhitespaceCharacters(content)
    if (contentLength === 0) continue
    if (contentLength <= available) {
      accepted.push(content)
      characterCount += contentLength
      continue
    }
    // Character limits are a prefix limit. Cutting a chapter is intentional:
    // "first 200k characters" is more useful than silently dropping it.
    accepted.push(takeNonWhitespacePrefix(content, available))
    characterCount = maxCharacters
    truncatedByCharacters = true
    break
  }

  const text = accepted.join('\n\n')
  const paragraphs = splitParagraphs(text)
  const sentenceUnit = chooseSentenceUnit(text)
  const sentences = paragraphs.flatMap(splitSentences).filter(hasProseContent)
  const sentenceValues = sentences.map(sentence => measureSentence(sentence, sentenceUnit))
  const paragraphValues = paragraphs
    .filter(hasProseContent)
    .map(paragraph => countNonWhitespaceCharacters(paragraph))
  const dialogueFlags = paragraphs.map(hasDialogue)
  const dialogueParagraphCount = dialogueFlags.filter(Boolean).length
  const dialogueRuns = collectDialogueRuns(dialogueFlags)
  const punctuationPerThousand = countPunctuation(text, characterCount)
  const repeatedPhrases = options.includeRepeatedPhrases === false ? [] : findRepeatedPhrases(text)
  // Keep the timer around the complete operation, including repeated phrase
  // extraction, so the displayed number represents what the author waited for.
  const ended = typeof performance !== 'undefined' ? performance.now() : Date.now()

  return {
    chapterCount: accepted.length,
    characterCount,
    sentenceLength: quantiles(sentenceValues, sentenceUnit),
    paragraphLength: quantiles(paragraphValues, 'characters'),
    dialogueParagraphRatio: paragraphs.length ? dialogueParagraphCount / paragraphs.length : 0,
    dialogueParagraphCount,
    paragraphCount: paragraphs.length,
    dialogueRunAverage: dialogueRuns.length ? round(sum(dialogueRuns) / dialogueRuns.length, 2) : 0,
    dialogueRunLongest: dialogueRuns.length ? Math.max(...dialogueRuns) : 0,
    punctuationPerThousand,
    repeatedPhrases,
    truncated: truncatedByChapters || truncatedByCharacters,
    truncatedByChapters,
    truncatedByCharacters,
    requestedChapterCount,
    skippedChapterCount: options.skippedChapterCount ?? 0,
    elapsedMs: ended - started,
    truncatedChapterLabels: computedTruncatedChapterLabels.length
      ? computedTruncatedChapterLabels
      : options.truncatedChapterLabels ? [...options.truncatedChapterLabels] : undefined,
  }
}

/** Produce a short Markdown block that can be pasted into a style profile. */
export function styleFingerprintToMarkdown(
  result: StyleFingerprintResult,
  locale: StyleFingerprintMarkdownLocale = 'zh',
): string {
  if (locale === 'en') {
    const unit = result.sentenceLength.unit === 'words' ? 'words' : 'characters'
    const lines = [
      `- Scope: ${result.chapterCount} chapter(s), ${result.characterCount} characters.`,
      `- Sentence length: P50 ${result.sentenceLength.p50}, P90 ${result.sentenceLength.p90} (${unit}).`,
      `- Paragraph length: P50 ${result.paragraphLength.p50}, P90 ${result.paragraphLength.p90} characters.`,
      `- Dialogue: ${formatPercent(result.dialogueParagraphRatio)} of paragraphs contain dialogue; average run ${result.dialogueRunAverage}, longest ${result.dialogueRunLongest} paragraphs.`,
      `- Punctuation per 1,000 characters: ellipses ${result.punctuationPerThousand.ellipsis}, dashes ${result.punctuationPerThousand.dash}, exclamations ${result.punctuationPerThousand.exclamation}.`,
    ]
    if (result.repeatedPhrases.length) lines.push(`- Repeated phrases: ${result.repeatedPhrases.map(item => `"${item.phrase}" ${item.count} times`).join(', ')}.`)
    if (result.truncatedByChapters) lines.push(`- Scope note: only the first ${result.chapterCount} chapter(s) were included${formatDroppedLabels(result.truncatedChapterLabels, '; skipped: ', 'en')}.`)
    if (result.truncatedByCharacters) lines.push(`- Scope note: only the first ${result.characterCount} characters were included.`)
    if (result.skippedChapterCount) lines.push(`- Read note: ${result.skippedChapterCount} chapter(s) could not be read and were skipped.`)
    return lines.join('\n')
  }
  const lines = [
    `- 范围：${result.chapterCount} 章，${result.characterCount} 字。`,
    `- 句长：P50 ${result.sentenceLength.p50}，P90 ${result.sentenceLength.p90}（按${result.sentenceLength.unit === 'words' ? '词数' : '字数'}统计）。`,
    `- 段长：P50 ${result.paragraphLength.p50}，P90 ${result.paragraphLength.p90} 字。`,
    `- 对白：${formatPercent(result.dialogueParagraphRatio)} 的段落含对白，连续对白平均 ${result.dialogueRunAverage} 段，最长 ${result.dialogueRunLongest} 段。`,
    `- 标点（每千字）：省略号 ${result.punctuationPerThousand.ellipsis}，破折号 ${result.punctuationPerThousand.dash}，叹号 ${result.punctuationPerThousand.exclamation}。`,
  ]
  if (result.repeatedPhrases.length) lines.push(`- 重复短语：${result.repeatedPhrases.map(item => `「${item.phrase}」${item.count} 次`).join('、')}。`)
  if (result.truncatedByChapters) lines.push(`- 范围提示：只统计了前 ${result.chapterCount} 章${formatDroppedLabels(result.truncatedChapterLabels, '，已跳过：', 'zh')}。`)
  if (result.truncatedByCharacters) lines.push(`- 范围提示：只统计了前 ${result.characterCount} 字。`)
  if (result.skippedChapterCount) lines.push(`- 读取提示：有 ${result.skippedChapterCount} 章读取失败，未参与统计。`)
  return lines.join('\n')
}

function normalizeMarkdownBody(value: string) {
  return value.split(/\r?\n/).filter(line => !ATX_HEADING_PATTERN.test(line)).join('\n')
}

function countNonWhitespaceCharacters(value: string) {
  return [...value.replace(/\s/gu, '')].length
}

function takeNonWhitespacePrefix(value: string, limit: number) {
  if (limit <= 0) return ''
  let count = 0
  let end = 0
  for (const character of value) {
    end += character.length
    if (/\s/u.test(character)) continue
    count++
    if (count >= limit) break
  }
  return value.slice(0, end)
}

function splitParagraphs(text: string) {
  return text.split(/(?:\r?\n\s*){2,}/).map(value => value.trim()).filter(Boolean)
}

function splitSentences(paragraph: string) {
  const sentences: string[] = []
  let start = 0
  for (let index = 0; index < paragraph.length; index++) {
    const character = paragraph[index]
    const isPeriod = character === '.'
      && (index + 1 === paragraph.length || /\s/u.test(paragraph[index + 1] || ''))
      && !/\d/u.test(paragraph[index - 1] || '')
      && !isAbbreviationPeriod(paragraph, index)
    const isSentencePunctuation = /[。！？!?…]/.test(character)
    if (!isSentencePunctuation && !isPeriod) continue
    let end = index + 1
    if (character === '.' && paragraph.slice(index, index + 3) === '...') end = index + 3
    while (end < paragraph.length && /[。！？!?…]/.test(paragraph[end])) end++
    const value = paragraph.slice(start, end).trim()
    if (value) sentences.push(value)
    start = end
    index = end - 1
  }
  const tail = paragraph.slice(start).trim()
  if (tail) sentences.push(tail)
  return sentences
}

function isAbbreviationPeriod(value: string, index: number) {
  return ABBREVIATION_PATTERN.test(value.slice(Math.max(0, index - 6), index + 1))
}

function hasProseContent(value: string) {
  return CONTENT_PATTERN.test(value)
}

function chooseSentenceUnit(text: string): 'characters' | 'words' {
  // The body decides one unit for the whole result. In a mixed chapter the
  // minority language is converted to that unit (each CJK character counts as
  // one word in a word-based result; Latin tokens count as characters in a
  // character-based result) so one percentile never mixes scales.
  const compact = text.replace(/\s/gu, '')
  const cjk = (compact.match(CJK_PATTERN) || []).length
  return cjk > compact.length / 2 ? 'characters' : 'words'
}

function measureSentence(value: string, unit: 'characters' | 'words') {
  return unit === 'characters' ? countSentenceCharacters(value) : countSentenceWords(value)
}

function countSentenceCharacters(value: string) {
  return countNonWhitespaceCharacters(value.replace(/[\p{P}\p{S}]/gu, ''))
}

function countSentenceWords(value: string) {
  const latinWords = value.match(WORD_PATTERN)?.length || 0
  const cjkWords = (value.match(CJK_PATTERN) || []).length
  return latinWords + cjkWords
}

function quantiles(values: number[], unit: 'characters' | 'words'): StyleFingerprintQuantiles {
  const numbers = values.filter(value => value > 0).sort((left, right) => left - right)
  return {
    p50: Math.round(percentile(numbers, 0.5)),
    p90: Math.round(percentile(numbers, 0.9)),
    sampleCount: numbers.length,
    unit,
  }
}

function percentile(values: number[], percentileValue: number) {
  if (!values.length) return 0
  const position = (values.length - 1) * percentileValue
  const lower = Math.floor(position)
  const upper = Math.ceil(position)
  if (lower === upper) return values[lower]
  const weight = position - lower
  return values[lower] + (values[upper] - values[lower]) * weight
}

function hasDialogue(paragraph: string) {
  return /[「『“‘][^」』”’]*[」』”’]|["'][^"']+["']/.test(paragraph)
}

function collectDialogueRuns(flags: boolean[]) {
  const runs: number[] = []
  let current = 0
  for (const flag of flags) {
    if (flag) current++
    else if (current) { runs.push(current); current = 0 }
  }
  if (current) runs.push(current)
  return runs
}

function countPunctuation(text: string, characterCount: number): StyleFingerprintPunctuation {
  if (characterCount === 0) return { ellipsis: 0, dash: 0, exclamation: 0 }
  const ellipsis = text.match(/…+|\.{3,}/g)?.length || 0
  const dash = text.match(/—+|——+|--+/g)?.length || 0
  const exclamation = text.match(/[!！]/g)?.length || 0
  const scale = 1000 / characterCount
  return { ellipsis: round(ellipsis * scale, 2), dash: round(dash * scale, 2), exclamation: round(exclamation * scale, 2) }
}

function findRepeatedPhrases(text: string): StyleFingerprintPhrase[] {
  const counts = new Map<string, number>()
  const ranges = [...text.matchAll(/[\u3400-\u9fff]+/g)]
  for (const match of ranges) {
    countPhraseWindows(match[0], counts)
  }

  // Keep only the highest-frequency candidates before fragment filtering.
  // Without this bound a periodic 200k-character sample can create tens of
  // thousands of candidates and turn the UI into an O(n²) operation.
  const ranked = [...counts.entries()]
    .filter(([phrase, count]) => count >= STYLE_FINGERPRINT_LIMITS.repeatedPhraseMinimumCount && !isSingleCharacterReduplication(phrase))
    .sort(comparePhraseCandidates)
    .slice(0, STYLE_FINGERPRINT_LIMITS.maxRepeatedPhraseCandidates)
  if (!ranked.length) return []

  const selected = new Set(ranked.map(([phrase]) => phrase))
  const positions = collectPhrasePositions(ranges, selected)
  const candidates = ranked.map(([phrase, count]) => ({
    phrase,
    count,
    starts: positions.get(phrase) || [],
    shape: phraseOccurrenceShape(positions.get(phrase) || []),
  }))
  const reduplicatedCandidates = new Set(
    candidates.filter(candidate => isReduplicatedPhrase(candidate.phrase)).map(candidate => `${candidate.count}\u0000${candidate.phrase}`),
  )

  // Index substrings by (count, phrase) so maximal-fragment filtering stays
  // linear in the bounded candidate set instead of comparing every pair.
  const containedByLonger = new Set<string>()
  for (const candidate of candidates) {
    for (let length = STYLE_FINGERPRINT_LIMITS.repeatedPhraseMinimumLength; length < candidate.phrase.length; length++) {
      for (let index = 0; index + length <= candidate.phrase.length; index++) {
        containedByLonger.add(`${candidate.count}\u0000${candidate.phrase.slice(index, index + length)}`)
      }
    }
  }
  const maximal = candidates.filter(candidate => {
    const reduplicated = isReduplicatedPhrase(candidate.phrase)
    // A longer same-count candidate owns ordinary fragments. Reduplicated
    // oral phrases such as 「看了看」 remain useful style signals.
    if (reduplicated) return true
    for (let length = 3; length < candidate.phrase.length; length++) {
      for (let index = 0; index + length <= candidate.phrase.length; index++) {
        if (reduplicatedCandidates.has(`${candidate.count}\u0000${candidate.phrase.slice(index, index + length)}`)) return false
      }
    }
    return !containedByLonger.has(`${candidate.count}\u0000${candidate.phrase}`)
  })

  // Equal-length windows from the same repeated passage have the same gap
  // pattern and overlapping starts. Keep the earliest window as the single
  // representative; distinct passages stay separate because their starts do
  // not overlap. This removes sliding fragments without a pairwise scan.
  const representatives = new Map<string, Array<typeof maximal[number]>>()
  for (const candidate of maximal) {
    const firstStart = candidate.starts[0] ?? Number.MAX_SAFE_INTEGER
    const bucket = Math.floor(firstStart / candidate.phrase.length)
    const keyPrefix = `${candidate.phrase.length}:${candidate.count}:${candidate.shape}`
    let replaced = false
    for (const bucketOffset of [-1, 0, 1]) {
      const key = `${keyPrefix}:${bucket + bucketOffset}`
      const existingCandidates = representatives.get(key) || []
      const existingIndex = existingCandidates.findIndex(existing => sameSlidingWindowFamily(existing, candidate))
      if (existingIndex < 0) continue
      if ((candidate.starts[0] ?? Number.MAX_SAFE_INTEGER) < (existingCandidates[existingIndex].starts[0] ?? Number.MAX_SAFE_INTEGER)) existingCandidates[existingIndex] = candidate
      representatives.set(key, existingCandidates)
      replaced = true
      break
    }
    if (!replaced) {
      const key = `${keyPrefix}:${bucket}`
      representatives.set(key, [...(representatives.get(key) || []), candidate])
    }
  }

  return [...representatives.values()].flat()
    .sort((left, right) => comparePhraseCandidates([left.phrase, left.count], [right.phrase, right.count]))
    .slice(0, STYLE_FINGERPRINT_LIMITS.maxRepeatedPhrases)
    .map(({ phrase, count }) => ({ phrase, count }))
}

function countPhraseWindows(range: string, counts: Map<string, number>) {
  // Do not count overlapping windows from one continuous run as separate
  // occurrences; this prevents 「哈哈哈哈」 from becoming repeated 「哈哈」.
  const lastEnd = new Map<string, number>()
  for (let index = 0; index < range.length; index++) {
    for (let length = STYLE_FINGERPRINT_LIMITS.repeatedPhraseMinimumLength; length <= STYLE_FINGERPRINT_LIMITS.repeatedPhraseMaximumLength; length++) {
      if (index + length > range.length) break
      const phrase = range.slice(index, index + length)
      if (STYLE_FINGERPRINT_FUNCTION_WORDS.has(phrase)) continue
      if ((lastEnd.get(phrase) ?? -1) > index) continue
      lastEnd.set(phrase, index + length)
      counts.set(phrase, (counts.get(phrase) || 0) + 1)
    }
  }
}

function collectPhrasePositions(ranges: RegExpMatchArray[], selected: Set<string>) {
  const positions = new Map<string, number[]>()
  for (const match of ranges) {
    const range = match[0]
    const rangeStart = match.index ?? 0
    const lastEnd = new Map<string, number>()
    for (let index = 0; index < range.length; index++) {
      for (let length = STYLE_FINGERPRINT_LIMITS.repeatedPhraseMinimumLength; length <= STYLE_FINGERPRINT_LIMITS.repeatedPhraseMaximumLength; length++) {
        if (index + length > range.length) break
        const phrase = range.slice(index, index + length)
        if (!selected.has(phrase) || (lastEnd.get(phrase) ?? -1) > index) continue
        lastEnd.set(phrase, index + length)
        const starts = positions.get(phrase) || []
        starts.push(rangeStart + index)
        positions.set(phrase, starts)
      }
    }
  }
  return positions
}

function phraseOccurrenceShape(starts: readonly number[]) {
  if (starts.length < 2) return ''
  const gaps: number[] = []
  for (let index = 1; index < starts.length; index++) gaps.push(starts[index] - starts[index - 1])
  return gaps.join(',')
}

function sameSlidingWindowFamily(left: { phrase: string; count: number; starts: number[]; shape: string }, right: { phrase: string; count: number; starts: number[]; shape: string }) {
  if (left.count !== right.count || left.phrase.length !== right.phrase.length || left.shape !== right.shape || left.starts.length !== right.starts.length) return false
  const distance = Math.abs((left.starts[0] ?? 0) - (right.starts[0] ?? 0))
  return distance < left.phrase.length
}

function isSingleCharacterReduplication(phrase: string) {
  return phrase.length >= 2 && [...phrase].every(character => character === phrase[0])
}

function isReduplicatedPhrase(phrase: string) {
  return phrase.length >= 3 && phrase[0] === phrase[phrase.length - 1]
}

function comparePhraseCandidates(left: [string, number], right: [string, number]) {
  const leftReduplicated = isReduplicatedPhrase(left[0])
  const rightReduplicated = isReduplicatedPhrase(right[0])
  return Number(rightReduplicated) - Number(leftReduplicated)
    || right[1] - left[1]
    || right[0].length - left[0].length
    || left[0].localeCompare(right[0])
}

function formatDroppedLabels(labels: readonly string[] | undefined, prefix: string, locale: StyleFingerprintMarkdownLocale) {
  if (!labels?.length) return ''
  if (locale === 'en') {
    const shown = labels.slice(0, 3).join(', ')
    const suffix = labels.length > 3 ? `${shown}, and ${labels.length - 3} more` : shown
    return `${prefix}${suffix}`
  }
  const shown = labels.slice(0, 3).join('、')
  const suffix = labels.length > 3 ? `${shown}，另有 ${labels.length - 3} 章` : shown
  return `${prefix}${suffix}`
}

function formatPercent(value: number) {
  return `${Math.round(value * 100)}%`
}

function sum(values: number[]) {
  return values.reduce((total, value) => total + value, 0)
}

function round(value: number, digits: number) {
  const scale = 10 ** digits
  return Math.round(value * scale) / scale
}

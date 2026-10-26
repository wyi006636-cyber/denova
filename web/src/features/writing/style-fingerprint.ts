/**
 * Deterministic, browser-only prose statistics.  Nothing in this module
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
} as const

/**
 * These are deliberately small, common function words.  A repeated phrase
 * containing only one of them is usually grammar noise rather than a useful
 * style signal, so it is omitted from the fingerprint.
 */
export const STYLE_FINGERPRINT_FUNCTION_WORDS = new Set([
  '的', '了', '着', '过', '地', '得', '和', '与', '及', '或', '而', '但', '却', '也',
  '都', '还', '很', '更', '最', '在', '从', '向', '对', '把', '被', '让', '给', '将',
  '是', '有', '无', '不', '没', '未', '能', '会', '要', '可', '这', '那', '其', '他',
  '她', '它', '我', '你', '们', '一个', '一种', '然后', '但是', '因为', '所以',
  '如果', '就是', '已经', '还是', '虽然', '因此', '那么', '只是', '能够', '正在',
  '所有', '这些', '那些', '什么', '没有', '可以', '自己', '他们', '我们',
])
const SINGLE_FUNCTION_WORDS = new Set([...STYLE_FINGERPRINT_FUNCTION_WORDS].filter(word => [...word].length === 1))
const MULTI_FUNCTION_WORDS = [...STYLE_FINGERPRINT_FUNCTION_WORDS].filter(word => [...word].length > 1)

export interface StyleFingerprintChapter {
  path?: string
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
}

export interface StyleFingerprintOptions {
  includeRepeatedPhrases?: boolean
  maxChapters?: number
  maxCharacters?: number
  skippedChapterCount?: number
}

const CJK_PATTERN = /[\u3400-\u9fff]/g
const WORD_PATTERN = /[A-Za-z]+(?:['’][A-Za-z]+)?/g
const CONTENT_PATTERN = /[\p{L}\p{N}\u3400-\u9fff]/u

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
  let characterCount = 0
  let truncatedByChapters = false
  let truncatedByCharacters = false

  for (const chapter of chapters) {
    if (accepted.length >= maxChapters) {
      truncatedByChapters = true
      break
    }
    const content = chapter.content || ''
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
    // Character limits are a prefix limit.  Cutting a chapter is intentional:
    // "first 200k characters" is more useful than silently dropping the last
    // selected chapter altogether.
    accepted.push(takeNonWhitespacePrefix(content, available))
    characterCount = maxCharacters
    truncatedByCharacters = true
    break
  }

  const text = accepted.join('\n\n')
  const paragraphs = splitParagraphs(text)
  const sentences = paragraphs.flatMap(splitSentences).filter(hasProseContent)
  const sentenceValues = sentences.map(measureSentence)
  const paragraphValues = paragraphs
    .filter(hasProseContent)
    .map(paragraph => countNonWhitespaceCharacters(paragraph))
  const dialogueFlags = paragraphs.map(hasDialogue)
  const dialogueParagraphCount = dialogueFlags.filter(Boolean).length
  const dialogueRuns = collectDialogueRuns(dialogueFlags)
  const punctuationPerThousand = countPunctuation(text, characterCount)
  const ended = typeof performance !== 'undefined' ? performance.now() : Date.now()

  return {
    chapterCount: accepted.length,
    characterCount,
    sentenceLength: quantiles(sentenceValues, sentenceValues.some(value => value.unit === 'words') ? 'words' : 'characters'),
    paragraphLength: quantiles(paragraphValues, 'characters'),
    dialogueParagraphRatio: paragraphs.length ? dialogueParagraphCount / paragraphs.length : 0,
    dialogueParagraphCount,
    paragraphCount: paragraphs.length,
    dialogueRunAverage: dialogueRuns.length ? round(sum(dialogueRuns) / dialogueRuns.length, 2) : 0,
    dialogueRunLongest: dialogueRuns.length ? Math.max(...dialogueRuns) : 0,
    punctuationPerThousand,
    repeatedPhrases: options.includeRepeatedPhrases === false ? [] : findRepeatedPhrases(text),
    truncated: truncatedByChapters || truncatedByCharacters,
    truncatedByChapters,
    truncatedByCharacters,
    requestedChapterCount,
    skippedChapterCount: options.skippedChapterCount ?? 0,
    elapsedMs: ended - started,
  }
}

/** Produce a short Markdown block that can be pasted into a style profile. */
export function styleFingerprintToMarkdown(result: StyleFingerprintResult): string {
  const lines = [
    `- 范围：${result.chapterCount} 章，${result.characterCount} 字。`,
    `- 句长：P50 ${result.sentenceLength.p50}，P90 ${result.sentenceLength.p90}（按${result.sentenceLength.unit === 'words' ? '英文词数' : '字符'}统计）。`,
    `- 段长：P50 ${result.paragraphLength.p50}，P90 ${result.paragraphLength.p90} 字。`,
    `- 对白：${formatPercent(result.dialogueParagraphRatio)} 的段落含对白，连续对白平均 ${result.dialogueRunAverage} 段，最长 ${result.dialogueRunLongest} 段。`,
    `- 标点（每千字）：省略号 ${result.punctuationPerThousand.ellipsis}，破折号 ${result.punctuationPerThousand.dash}，叹号 ${result.punctuationPerThousand.exclamation}。`,
  ]
  if (result.repeatedPhrases.length) {
    lines.push(`- 重复短语：${result.repeatedPhrases.map(item => `「${item.phrase}」${item.count} 次`).join('、')}。`)
  }
  if (result.truncatedByChapters) lines.push(`- 范围提示：只统计了前 ${result.chapterCount} 章。`)
  if (result.truncatedByCharacters) lines.push(`- 范围提示：只统计了前 ${result.characterCount} 字。`)
  if (result.skippedChapterCount) lines.push(`- 读取提示：有 ${result.skippedChapterCount} 章读取失败，未参与统计。`)
  return lines.join('\n')
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

function hasProseContent(value: string) {
  return CONTENT_PATTERN.test(value)
}

function measureSentence(value: string): { value: number; unit: 'characters' | 'words' } {
  const cjk = (value.match(CJK_PATTERN) || []).length
  const nonWhitespace = countSentenceCharacters(value)
  if (cjk > 0 && cjk / Math.max(1, nonWhitespace) >= 0.15) {
    return { value: nonWhitespace, unit: 'characters' }
  }
  return { value: value.match(WORD_PATTERN)?.length || 0, unit: 'words' }
}

function countSentenceCharacters(value: string) {
  return countNonWhitespaceCharacters(value.replace(/[。！？!?….,，、；;：:「」『』“”‘’"'（）()]/g, ''))
}

function quantiles(values: Array<number | { value: number; unit: 'characters' | 'words' }>, unit: 'characters' | 'words'): StyleFingerprintQuantiles {
  const numbers = values
    .map(value => typeof value === 'number' ? value : value.value)
    .filter(value => value > 0)
    .sort((left, right) => left - right)
  return {
    p50: percentile(numbers, 0.5),
    p90: percentile(numbers, 0.9),
    sampleCount: numbers.length,
    unit,
  }
}

function percentile(values: number[], percentileValue: number) {
  if (!values.length) return 0
  const index = Math.min(values.length - 1, Math.ceil(values.length * percentileValue) - 1)
  return values[Math.max(0, index)]
}

function hasDialogue(paragraph: string) {
  return /[「『“‘][^」』”’]*[」』”’]|["'][^"']+["']/.test(paragraph)
}

function collectDialogueRuns(flags: boolean[]) {
  const runs: number[] = []
  let current = 0
  for (const flag of flags) {
    if (flag) {
      current++
    } else if (current) {
      runs.push(current)
      current = 0
    }
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
  return {
    ellipsis: round(ellipsis * scale, 2),
    dash: round(dash * scale, 2),
    exclamation: round(exclamation * scale, 2),
  }
}

function findRepeatedPhrases(text: string): StyleFingerprintPhrase[] {
  const counts = new Map<string, number>()
  const ranges = text.match(/[\u3400-\u9fff]+/g) || []
  for (const range of ranges) {
    for (let length = STYLE_FINGERPRINT_LIMITS.repeatedPhraseMinimumLength; length <= STYLE_FINGERPRINT_LIMITS.repeatedPhraseMaximumLength; length++) {
      for (let index = 0; index + length <= range.length; index++) {
        const phrase = range.slice(index, index + length)
        if (isFunctionPhrase(phrase)) continue
        counts.set(phrase, (counts.get(phrase) || 0) + 1)
      }
    }
  }
  return [...counts.entries()]
    .filter(([, count]) => count >= STYLE_FINGERPRINT_LIMITS.repeatedPhraseMinimumCount)
    .sort((left, right) => right[1] - left[1] || right[0].length - left[0].length || left[0].localeCompare(right[0]))
    .slice(0, STYLE_FINGERPRINT_LIMITS.maxRepeatedPhrases)
    .map(([phrase, count]) => ({ phrase, count }))
}

function isFunctionPhrase(phrase: string) {
  if (STYLE_FINGERPRINT_FUNCTION_WORDS.has(phrase)) return true
  if ([...phrase].some(character => SINGLE_FUNCTION_WORDS.has(character))) return true
  return MULTI_FUNCTION_WORDS.some(word => phrase.includes(word))
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

/**
 * Fast, deterministic chapter checks. This module deliberately has no React,
 * network or editor dependencies so a check never consumes model tokens.
 */

export const CHAPTER_LINT_LIMITS = {
  /** A repetition is only useful when the occurrences are close to one another. */
  repeatWindow: 120,
  /** Three occurrences are the minimum signal that a phrase is accidental. */
  repeatMinimumCount: 3,
  /** Two-character CJK names and function words are too noisy to report. */
  repeatMinimumCjkLength: 3,
  repeatMaximumPhraseLength: 6,
  maxParagraphCharacters: 800,
  maxSentenceCharacters: 120,
  /** English sentences use words because character counts over-report short words. */
  maxSentenceWords: 40,
  /** A small CJK share is enough to classify a chapter as Chinese prose. */
  cjkLanguageRatio: 0.15,
  maxIssues: 500,
} as const

export type ChapterLintCategory =
  | 'banned'
  | 'ai'
  | 'repeat'
  | 'punctuation'
  | 'length'
  | 'reference'

export type ChapterLintSeverity = 'error' | 'warning' | 'info'

export interface ChapterLintMessage {
  key: string
  params?: Record<string, string | number>
}

export interface ChapterLintOccurrence {
  start: number
  end: number
}

export interface BannedWord {
  word: string
  suggestion?: string
}

export interface BannedWordParseResult {
  words: BannedWord[]
  invalidLines: number[]
}

export interface ChapterLintIssue {
  id: string
  category: ChapterLintCategory
  severity: ChapterLintSeverity
  start: number
  end: number
  text: string
  message: ChapterLintMessage
  suggestion?: string
  /** Repetition findings are grouped, while this list keeps every jump target. */
  occurrences?: ChapterLintOccurrence[]
}

export interface ChapterLintOptions {
  bannedWords?: readonly BannedWord[]
  /** Names and keywords are both accepted as lore terms for reference/repeat checks. */
  loreNames?: readonly string[]
  loreTerms?: readonly string[]
  /** Kept for callers that still pass the UI locale; rule language is content-derived. */
  locale?: 'zh' | 'en'
  enabled?: Partial<Record<ChapterLintCategory, boolean>>
}

export interface ChapterLintReport {
  issues: ChapterLintIssue[]
  counts: Record<ChapterLintCategory, number>
  invalidBannedWordLines: number[]
  elapsedMs: number
}

export const DEFAULT_AI_TRACE_LEXICON_ZH = [
  '仿佛', '弥漫着', '笼罩着', '交织', '斑驳', '氤氲', '缥缈', '幽暗深邃',
  '难以名状', '说不清的感觉', '微微一怔', '微微一笑', '微微点头', '嘴角勾起',
  '一抹笑意', '眼中闪过一丝', '瞳孔骤缩', '倒吸一口凉气', '负手而立', '眸光流转',
  '眉头微蹙', '身体一僵', '心头一颤', '他感到', '心中一凛', '心头一紧', '五味杂陈',
  '他意识到', '终于明白', '终于懂了', '不由得', '不禁', '莫名地', '下意识地',
  '一股暖流涌上心头', '难以言喻', '无法形容', '宛如实质', '极其', '极度', '无比',
  '十分', '异常', '缓缓地', '淡淡地', '轻轻地', '深深地', '有条不紊', '不紧不慢地',
  '与此同时', '紧接着', '随后', '毫无疑问', '显而易见', '不得不说', '进行了一番',
  '被迫地', '作为一名', '他的眼睛里充满了', '这一刻，他终于明白',
] as const

/** English entries are intentionally multi-word, high-signal phrases. */
export const DEFAULT_AI_TRACE_LEXICON_EN = [
  'it is worth noting', 'there was a sense of', 'in a way', 'without a doubt',
  'needless to say', 'as if the air', 'a faint smile played', 'his eyes flashed',
  'she realized that', 'he realized that', 'finally understood', 'a surge of',
  'in that moment, he understood', 'with an air of',
] as const

/** Source: skills/humanize/references/ai-trace-lexicon.md. */
export const AI_TRACE_LEXICON_SOURCE = 'skills/humanize/references/ai-trace-lexicon.md'

const CATEGORY_ORDER: ChapterLintCategory[] = ['banned', 'ai', 'repeat', 'punctuation', 'length', 'reference']
const CJK_PATTERN = /[\u3400-\u9fff]/g
const REPEAT_STOP_WORDS = new Set([
  '我们', '他们', '自己', '这个', '那个', '什么', '没有', '可以', '因为', '所以', '这是', '一个', '一种',
  '然后', '但是', '如果', '就是', '已经', '还是', '虽然', '因此', '那么', '只是', '能够', '正在', '所有',
  '这些', '那些', '他的', '她的', '我的', '你的', 'the', 'and', 'for', 'with', 'that', 'this', 'from', 'have',
])

type AddIssue = (
  category: ChapterLintCategory,
  start: number,
  end: number,
  message: ChapterLintMessage,
  severity?: ChapterLintSeverity,
  extras?: { suggestion?: string; occurrences?: ChapterLintOccurrence[] },
) => void

export function parseBannedWords(markdown: string): BannedWordParseResult {
  const words: BannedWord[] = []
  const invalidLines: number[] = []
  const seen = new Set<string>()
  for (const [index, rawLine] of markdown.split(/\r?\n/).entries()) {
    const line = rawLine.trim()
    if (!line || line.startsWith('#')) continue
    const parts = line.split(/\s*->\s*/)
    const word = parts[0]?.trim() || ''
    if (!word || parts.length > 2) {
      invalidLines.push(index + 1)
      continue
    }
    const key = word.toLocaleLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    words.push({ word, suggestion: parts[1]?.trim() || undefined })
  }
  return { words, invalidLines }
}

export function runChapterLint(text: string, options: ChapterLintOptions = {}): ChapterLintReport {
  const started = typeof performance !== 'undefined' ? performance.now() : Date.now()
  const enabled = (category: ChapterLintCategory) => options.enabled?.[category] !== false
  const issues: ChapterLintIssue[] = []
  const add: AddIssue = (category, start, end, message, severity = 'warning', extras = {}) => {
    if (!enabled(category) || issues.length >= CHAPTER_LINT_LIMITS.maxIssues || end <= start) return
    issues.push({
      id: `${category}:${start}:${end}:${issues.length}`,
      category,
      severity,
      start,
      end,
      text: text.slice(start, end),
      message,
      suggestion: extras.suggestion,
      occurrences: extras.occurrences,
    })
  }

  if (enabled('banned')) {
    for (const banned of options.bannedWords || []) {
      const word = banned.word.trim()
      if (!word) continue
      const flags = /[A-Za-z]/.test(word) ? 'gi' : 'g'
      const expression = new RegExp(escapeRegExp(word), flags)
      let match: RegExpExecArray | null
      while ((match = expression.exec(text)) !== null) {
        const start = match.index
        add(
          'banned',
          start,
          start + match[0].length,
          { key: 'editor.lint.issue.banned', params: { word: match[0] } },
          'error',
          { suggestion: banned.suggestion },
        )
        if (match[0].length === 0) expression.lastIndex++
      }
    }
  }

  if (enabled('ai')) {
    const language = detectTextLanguage(text)
    const lexicon = language === 'en' ? DEFAULT_AI_TRACE_LEXICON_EN : DEFAULT_AI_TRACE_LEXICON_ZH
    for (const phrase of lexicon) {
      const expression = new RegExp(escapeRegExp(phrase), language === 'en' ? 'gi' : 'g')
      let match: RegExpExecArray | null
      while ((match = expression.exec(text)) !== null) {
        add('ai', match.index, match.index + match[0].length, {
          key: 'editor.lint.issue.ai',
          params: { phrase: match[0] },
        }, 'info')
        if (match[0].length === 0) expression.lastIndex++
      }
    }
  }

  const loreTerms = uniqueTerms([...(options.loreTerms || []), ...(options.loreNames || [])])
  if (enabled('repeat')) detectRepeats(text, loreTerms, add)
  if (enabled('punctuation')) detectPunctuation(text, add)
  if (enabled('length')) detectLength(text, add)
  if (enabled('reference')) {
    const names = new Set(loreTerms.map(name => name.trim()).filter(Boolean).map(healthTermKey))
    const expression = /\[\[([^\]]+)\]\]/g
    let match: RegExpExecArray | null
    while ((match = expression.exec(text)) !== null) {
      const name = match[1].trim()
      if (name && !names.has(healthTermKey(name))) {
        add('reference', match.index, match.index + match[0].length, {
          key: 'editor.lint.issue.reference',
          params: { name },
        }, 'error')
      }
    }
  }

  issues.sort((left, right) => left.start - right.start || categoryRank(left.category) - categoryRank(right.category))
  const counts = emptyCounts()
  for (const issue of issues) counts[issue.category]++
  const ended = typeof performance !== 'undefined' ? performance.now() : Date.now()
  return { issues, counts, invalidBannedWordLines: [], elapsedMs: ended - started }
}

export function runChapterLintWithBannedWords(
  text: string,
  bannedWordsFile: string,
  options: Omit<ChapterLintOptions, 'bannedWords'> = {},
): ChapterLintReport {
  const parsed = parseBannedWords(bannedWordsFile)
  const report = runChapterLint(text, { ...options, bannedWords: parsed.words })
  return { ...report, invalidBannedWordLines: parsed.invalidLines }
}

function detectRepeats(text: string, loreTerms: readonly string[], add: AddIssue) {
  const candidates = new Map<string, number[]>()
  const addCandidate = (phrase: string, start: number) => {
    const normalized = phrase.trim().toLocaleLowerCase()
    if (!normalized || isRepeatStopWord(normalized) || isExcludedRepeatPhrase(normalized, loreTerms)) return
    const positions = candidates.get(normalized) || []
    positions.push(start)
    candidates.set(normalized, positions)
  }

  const cjkRun = /[\u3400-\u9fff]+/g
  let match: RegExpExecArray | null
  while ((match = cjkRun.exec(text)) !== null) {
    const value = match[0]
    const maxSize = Math.min(CHAPTER_LINT_LIMITS.repeatMaximumPhraseLength, value.length)
    for (let size = CHAPTER_LINT_LIMITS.repeatMinimumCjkLength; size <= maxSize; size++) {
      for (let offset = 0; offset + size <= value.length; offset++) {
        addCandidate(value.slice(offset, offset + size), match.index + offset)
      }
    }
  }

  const words: Array<{ value: string; start: number; end: number }> = []
  const latin = /\b[A-Za-z]{3,}\b/g
  while ((match = latin.exec(text)) !== null) {
    const value = match[0].toLocaleLowerCase()
    words.push({ value, start: match.index, end: match.index + match[0].length })
    addCandidate(value, match.index)
  }
  for (let index = 0; index < words.length; index++) {
    for (let size = 2; size <= 3 && index + size <= words.length; size++) {
      const first = words[index]
      const last = words[index + size - 1]
      if (last.start - first.end > 2) break
      addCandidate(text.slice(first.start, last.end), first.start)
    }
  }

  const candidatesByLength = [...candidates.entries()]
    .map(([phrase, positions]) => ({ phrase, positions: dedupeOverlappingPositions(positions, phrase.length) }))
    .sort((left, right) => right.phrase.length - left.phrase.length || left.phrase.localeCompare(right.phrase))
  const selectedRanges: ChapterLintOccurrence[] = []
  for (const candidate of candidatesByLength) {
    // A six-character window can produce several equally frequent overlapping
    // slices from one sentence. Treat the selected slice plus its local
    // neighbourhood as covered so only one longest fragment is reported.
    const uncovered = candidate.positions.filter(start => !selectedRanges.some(range => {
      const coverageStart = range.start - CHAPTER_LINT_LIMITS.repeatMaximumPhraseLength
      const coverageEnd = range.end + CHAPTER_LINT_LIMITS.repeatMaximumPhraseLength
      return start < coverageEnd && start + candidate.phrase.length > coverageStart
    }))
    const occurrences = findRepeatCluster(uncovered, candidate.phrase.length)
    if (occurrences.length < CHAPTER_LINT_LIMITS.repeatMinimumCount) continue
    const ranges = occurrences.map(start => ({ start, end: start + candidate.phrase.length }))
    add('repeat', ranges[0].start, ranges[0].end, {
      key: 'editor.lint.issue.repeat',
      params: { phrase: candidate.phrase, count: ranges.length },
    }, 'warning', { occurrences: ranges })
    selectedRanges.push(...ranges)
  }
}

function findRepeatCluster(positions: number[], phraseLength: number): number[] {
  const sorted = [...positions].sort((left, right) => left - right)
  if (sorted.length < CHAPTER_LINT_LIMITS.repeatMinimumCount) return []
  let cluster: number[] = [sorted[0]]
  let best: number[] = cluster
  for (let index = 1; index < sorted.length; index++) {
    const previous = sorted[index - 1]
    const current = sorted[index]
    if (current - previous >= phraseLength && current - previous <= CHAPTER_LINT_LIMITS.repeatWindow) {
      cluster.push(current)
      continue
    }
    if (cluster.length > best.length) best = cluster
    cluster = [current]
  }
  if (cluster.length > best.length) best = cluster
  return best.length >= CHAPTER_LINT_LIMITS.repeatMinimumCount ? best : []
}

function dedupeOverlappingPositions(positions: number[], phraseLength: number): number[] {
  const sorted = [...new Set(positions)].sort((left, right) => left - right)
  const result: number[] = []
  for (const position of sorted) {
    if (result.length === 0 || position - result[result.length - 1] >= phraseLength) result.push(position)
  }
  return result
}

function detectPunctuation(text: string, add: AddIssue) {
  const repeated = /([！？!?，,；;：:])\1+/g
  let match: RegExpExecArray | null
  while ((match = repeated.exec(text)) !== null) {
    add('punctuation', match.index, match.index + match[0].length, {
      key: 'editor.lint.issue.repeatedPunctuation',
      params: { punctuation: match[0] },
    }, 'error')
  }
  const repeatedPeriods = /。{2,}/g
  while ((match = repeatedPeriods.exec(text)) !== null) {
    add('punctuation', match.index, match.index + match[0].length, {
      key: match[0].length >= 3 ? 'editor.lint.issue.ellipsis' : 'editor.lint.issue.repeatedPunctuation',
      params: match[0].length >= 3 ? undefined : { punctuation: match[0] },
    }, 'warning')
  }

  const unicodeEllipsis = /…+/g
  while ((match = unicodeEllipsis.exec(text)) !== null) {
    if (match[0].length === 2 || (match[0].length === 1 && !isChineseContext(text, match.index))) continue
    add('punctuation', match.index, match.index + match[0].length, {
      key: 'editor.lint.issue.ellipsis',
    }, 'warning')
  }
  const asciiPeriods = /\.+/g
  while ((match = asciiPeriods.exec(text)) !== null) {
    const length = match[0].length
    const malformed = length === 2 || length >= 4 || (length === 3 && isChineseContext(text, match.index))
    if (!malformed) continue
    add('punctuation', match.index, match.index + length, {
      key: 'editor.lint.issue.ellipsis',
    }, 'warning')
  }

  const mixed = /[^。！？!?\n]{0,160}[，,][^。！？!?\n]{0,160}[，,]/g
  while ((match = mixed.exec(text)) !== null) {
    const value = match[0]
    if (value.includes('，') && value.includes(',')) {
      const comma = value.indexOf(',')
      const ideographic = value.indexOf('，')
      const start = match.index + Math.max(comma, ideographic)
      add('punctuation', start, start + 1, { key: 'editor.lint.issue.mixedPunctuation' }, 'warning')
    }
  }
  detectUnbalancedPairs(text, add)
}

function detectUnbalancedPairs(text: string, add: AddIssue) {
  const pairs: Record<string, string> = { '(': ')', '（': '）', '[': ']', '［': '］', '【': '】', '「': '」', '『': '』', '“': '”', '‘': '’' }
  const closing = new Set(Object.values(pairs))
  const paragraphPattern = /[^\r\n]*(?:\r?\n|$)/g
  let paragraph: RegExpExecArray | null
  while ((paragraph = paragraphPattern.exec(text)) !== null) {
    if (!paragraph[0]) break
    const paragraphStart = paragraph.index
    const paragraphEnd = paragraph.index + paragraph[0].replace(/\r?\n$/, '').length
    if (paragraphEnd <= paragraphStart) continue
    const stack: { expected: string; start: number; open: string }[] = []
    for (let index = paragraphStart; index < paragraphEnd; index++) {
      const char = text[index]
      if ((char === '‘' || char === '’') && isEnglishApostrophe(text, index)) continue
      if (pairs[char]) stack.push({ expected: pairs[char], start: index, open: char })
      else if (closing.has(char)) {
        const top = stack[stack.length - 1]
        if (!top || top.expected !== char) {
          if (!isCrossParagraphQuote(text, index, paragraphStart, char)) {
            add('punctuation', index, index + 1, { key: 'editor.lint.issue.unbalancedClosing', params: { punctuation: char } }, 'error')
          }
        } else stack.pop()
      }
    }
    for (const unmatched of stack) {
      if (isCrossParagraphQuote(text, unmatched.start, paragraphStart, unmatched.open)) continue
      add('punctuation', unmatched.start, unmatched.start + 1, { key: 'editor.lint.issue.unbalancedOpening', params: { punctuation: unmatched.open } }, 'error')
    }
  }
}

function detectLength(text: string, add: AddIssue) {
  const paragraph = /[^\r\n]+/g
  let match: RegExpExecArray | null
  while ((match = paragraph.exec(text)) !== null) {
    const value = match[0]
    if (value.trim().length > CHAPTER_LINT_LIMITS.maxParagraphCharacters) {
      add('length', match.index, match.index + value.length, {
        key: 'editor.lint.issue.longParagraph',
        params: { limit: CHAPTER_LINT_LIMITS.maxParagraphCharacters },
      }, 'warning')
    }
  }
  const language = detectTextLanguage(text)
  const sentence = /[^。！？!?\.]+[。！？!?\.]+|[^。！？!?\.]+$/g
  while ((match = sentence.exec(text)) !== null) {
    const value = match[0]
    const tooLong = language === 'zh'
      ? value.trim().length > CHAPTER_LINT_LIMITS.maxSentenceCharacters
      : countEnglishWords(value) > CHAPTER_LINT_LIMITS.maxSentenceWords
    if (tooLong) {
      add('length', match.index, match.index + value.length, {
        key: 'editor.lint.issue.longSentence',
        params: { limit: language === 'zh' ? CHAPTER_LINT_LIMITS.maxSentenceCharacters : CHAPTER_LINT_LIMITS.maxSentenceWords },
      }, 'warning')
    }
  }
}

function emptyCounts(): Record<ChapterLintCategory, number> {
  return Object.fromEntries(CATEGORY_ORDER.map(category => [category, 0])) as Record<ChapterLintCategory, number>
}

function categoryRank(category: ChapterLintCategory) {
  return CATEGORY_ORDER.indexOf(category)
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function isRepeatStopWord(value: string) {
  return REPEAT_STOP_WORDS.has(value)
}

function isExcludedRepeatPhrase(value: string, loreTerms: readonly string[]) {
  return loreTerms.some(term => {
    const normalized = term.trim().toLocaleLowerCase()
    return normalized.length >= CHAPTER_LINT_LIMITS.repeatMinimumCjkLength && normalized.includes(value)
  })
}

function uniqueTerms(terms: readonly string[]) {
  return [...new Set(terms.map(term => term.trim()).filter(Boolean))]
}

function detectTextLanguage(text: string): 'zh' | 'en' {
  const cjk = (text.match(CJK_PATTERN) || []).length
  const sampleLength = text.replace(/\s/g, '').length
  return cjk > 0 && cjk / Math.max(1, sampleLength) >= CHAPTER_LINT_LIMITS.cjkLanguageRatio ? 'zh' : 'en'
}

function isChineseContext(text: string, start: number) {
  const nearby = text.slice(Math.max(0, start - 12), Math.min(text.length, start + 12))
  return /[\u3400-\u9fff]/.test(nearby)
}

function isEnglishApostrophe(text: string, index: number) {
  return index > 0 && index + 1 < text.length && /[A-Za-z]/.test(text[index - 1]) && /[A-Za-z]/.test(text[index + 1])
}

function isCrossParagraphQuote(text: string, index: number, paragraphStart: number, punctuation: string) {
  const leading = text.slice(paragraphStart, index)
  const atParagraphStart = leading.trim().length === 0
  if (atParagraphStart && ['「', '『', '“', '‘'].includes(punctuation)) return true
  // A closing Chinese quote can belong to an opening quote in a preceding
  // dialogue paragraph. Suppress this low-confidence false positive.
  return ['」', '』', '”'].includes(punctuation) && /[\u3400-\u9fff]/.test(text.slice(paragraphStart, index))
}

function countEnglishWords(value: string) {
  return value.match(/[A-Za-z]+(?:['’][A-Za-z]+)?/g)?.length || 0
}

function healthTermKey(value: string) {
  return value.toLocaleLowerCase().trim()
}

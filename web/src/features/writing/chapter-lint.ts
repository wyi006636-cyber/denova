/**
 * Fast, deterministic chapter checks. This module deliberately has no React,
 * network or editor dependencies so a check never consumes model tokens.
 */

export const CHAPTER_LINT_LIMITS = {
  repeatWindow: 120,
  repeatMinimumCount: 3,
  maxParagraphCharacters: 800,
  maxSentenceCharacters: 120,
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
  message: string
  suggestion?: string
}

export interface ChapterLintOptions {
  bannedWords?: readonly BannedWord[]
  loreNames?: readonly string[]
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
  const add = (
    category: ChapterLintCategory,
    start: number,
    end: number,
    message: string,
    severity: ChapterLintSeverity = 'warning',
    suggestion?: string,
  ) => {
    if (!enabled(category) || issues.length >= CHAPTER_LINT_LIMITS.maxIssues || end <= start) return
    issues.push({
      id: `${category}:${start}:${end}:${issues.length}`,
      category,
      severity,
      start,
      end,
      text: text.slice(start, end),
      message,
      suggestion,
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
        add('banned', start, start + match[0].length, `命中禁用词：${match[0]}`, 'error', banned.suggestion)
        if (match[0].length === 0) expression.lastIndex++
      }
    }
  }

  if (enabled('ai')) {
    const lexicon = options.locale === 'en' ? DEFAULT_AI_TRACE_LEXICON_EN : DEFAULT_AI_TRACE_LEXICON_ZH
    for (const phrase of lexicon) {
      const expression = new RegExp(escapeRegExp(phrase), options.locale === 'en' ? 'gi' : 'g')
      let match: RegExpExecArray | null
      while ((match = expression.exec(text)) !== null) {
        add('ai', match.index, match.index + match[0].length, `疑似 AI 腔词：${match[0]}`, 'info')
        if (match[0].length === 0) expression.lastIndex++
      }
    }
  }

  if (enabled('repeat')) detectRepeats(text, add)
  if (enabled('punctuation')) detectPunctuation(text, add)
  if (enabled('length')) detectLength(text, add)
  if (enabled('reference')) {
    const names = new Set((options.loreNames || []).map(name => name.trim()).filter(Boolean))
    const expression = /\[\[([^\]]+)\]\]/g
    let match: RegExpExecArray | null
    while ((match = expression.exec(text)) !== null) {
      const name = match[1].trim()
      if (name && !names.has(name)) {
        add('reference', match.index, match.index + match[0].length, `断链资料引用：${name}`, 'error')
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

function detectRepeats(
  text: string,
  add: (category: ChapterLintCategory, start: number, end: number, message: string, severity?: ChapterLintSeverity) => void,
) {
  const candidates = new Map<string, number[]>()
  const cjk = /[\u3400-\u9fff]{2,6}/g
  let match: RegExpExecArray | null
  while ((match = cjk.exec(text)) !== null) {
    const value = match[0]
    for (let size = 2; size <= Math.min(6, value.length); size++) {
      for (let offset = 0; offset + size <= value.length; offset++) {
        const phrase = value.slice(offset, offset + size)
        if (isRepeatStopWord(phrase)) continue
        const positions = candidates.get(phrase) || []
        positions.push(match.index + offset)
        candidates.set(phrase, positions)
      }
    }
  }
  const latin = /\b[A-Za-z]{3,}(?:\s+[A-Za-z]{3,})?\b/g
  while ((match = latin.exec(text)) !== null) {
    const phrase = match[0].toLocaleLowerCase()
    const positions = candidates.get(phrase) || []
    positions.push(match.index)
    candidates.set(phrase, positions)
  }
  const emitted = new Set<number>()
  for (const [phrase, positions] of candidates) {
    if (positions.length < CHAPTER_LINT_LIMITS.repeatMinimumCount) continue
    for (let index = 1; index < positions.length; index++) {
      const start = positions[index]
      if (start - positions[index - 1] > CHAPTER_LINT_LIMITS.repeatWindow) continue
      if (emitted.has(start)) continue
      emitted.add(start)
      add('repeat', start, start + phrase.length, `短距离重复词语：${phrase}`)
    }
  }
}

function detectPunctuation(
  text: string,
  add: (category: ChapterLintCategory, start: number, end: number, message: string, severity?: ChapterLintSeverity) => void,
) {
  const repeated = /([。！？!?，,；;：:])\1+/g
  let match: RegExpExecArray | null
  while ((match = repeated.exec(text)) !== null) {
    add('punctuation', match.index, match.index + match[0].length, `连续重复标点：${match[0]}`, 'error')
  }
  const malformedEllipsis = /…{1}(?!…)|…{3,}|\.{1,2}(?!\.)|\.{4,}/g
  while ((match = malformedEllipsis.exec(text)) !== null) {
    add('punctuation', match.index, match.index + match[0].length, '省略号写法不规范：中文使用“……”或英文使用“...”', 'warning')
  }
  const mixed = /[^。！？!?\n]{0,160}[，,][^。！？!?\n]{0,160}[，,]/g
  while ((match = mixed.exec(text)) !== null) {
    const value = match[0]
    if (value.includes('，') && value.includes(',')) {
      const start = match.index + Math.max(value.indexOf(','), value.indexOf('，'))
      add('punctuation', start, start + 1, '中英文标点混用', 'warning')
    }
  }
  detectUnbalancedPairs(text, add)
}

function detectUnbalancedPairs(
  text: string,
  add: (category: ChapterLintCategory, start: number, end: number, message: string, severity?: ChapterLintSeverity) => void,
) {
  const pairs: Record<string, string> = { '(': ')', '（': '）', '[': ']', '［': '］', '【': '】', '「': '」', '『': '』', '“': '”', '‘': '’' }
  const closing = new Set(Object.values(pairs))
  const stack: { expected: string; start: number; open: string }[] = []
  for (let index = 0; index < text.length; index++) {
    const char = text[index]
    if (pairs[char]) stack.push({ expected: pairs[char], start: index, open: char })
    else if (closing.has(char)) {
      const top = stack[stack.length - 1]
      if (!top || top.expected !== char) add('punctuation', index, index + 1, `括号或引号不配对：${char}`, 'error')
      else stack.pop()
    }
  }
  for (const unmatched of stack) add('punctuation', unmatched.start, unmatched.start + 1, `括号或引号缺少闭合：${unmatched.open}`, 'error')
}

function detectLength(
  text: string,
  add: (category: ChapterLintCategory, start: number, end: number, message: string, severity?: ChapterLintSeverity) => void,
) {
  const paragraph = /[^\r\n]+/g
  let match: RegExpExecArray | null
  while ((match = paragraph.exec(text)) !== null) {
    const value = match[0]
    if (value.trim().length > CHAPTER_LINT_LIMITS.maxParagraphCharacters) {
      add('length', match.index, match.index + value.length, `段落过长（>${CHAPTER_LINT_LIMITS.maxParagraphCharacters} 字）`, 'warning')
    }
  }
  const sentence = /[^。！？!?\.]+[。！？!?\.]+|[^。！？!?\.]+$/g
  while ((match = sentence.exec(text)) !== null) {
    const value = match[0]
    if (value.trim().length > CHAPTER_LINT_LIMITS.maxSentenceCharacters) {
      add('length', match.index, match.index + value.length, `句子过长（>${CHAPTER_LINT_LIMITS.maxSentenceCharacters} 字）`, 'warning')
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
  return ['我们', '他们', '自己', '这个', '那个', '什么', '没有', '可以', '因为', '所以'].includes(value)
}


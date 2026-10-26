import type {
  ChapterLintIssue,
  ChapterLintMessage,
  ChapterLintOccurrence,
} from './chapter-lint'

export interface VoiceLintCharacter {
  name: string
  aliases?: readonly string[]
  content: string
}

export interface ParsedVoiceRules {
  avoid: string[]
}

interface DialogueRange {
  start: number
  end: number
  opener: string
  closer: string
}

const VOICE_SECTION_HEADINGS = new Set(['说话方式', 'voice'])
const VOICE_FIELD_NAMES = new Set(['禁忌说法', 'avoid'])
const SPEECH_VERBS = [
  '说道', '说', '道', '问道', '问', '喊道', '喊', '叫道', '叫', '笑道', '笑',
  '答道', '答', '叹道', '叹', '吼道', '吼', '嘟囔道', '嘟囔', '低声道', '冷笑道',
  'said', 'says', 'asked', 'asks', 'shouted', 'shouts', 'replied', 'replies', 'laughed',
]
const DIALOGUE_PAIRS: Record<string, string> = {
  '「': '」',
  '『': '』',
  '“': '”',
  '‘': '’',
}

/** Parse only the optional Voice/说话方式 section of one lore body. */
export function parseVoiceRules(markdown: string): ParsedVoiceRules {
  const lines = markdown.split(/\r?\n/)
  const headingIndex = lines.findIndex(line => {
    const match = line.match(/^##\s+(.+?)\s*$/)
    return Boolean(match && VOICE_SECTION_HEADINGS.has(match[1].trim().toLocaleLowerCase()))
  })
  if (headingIndex < 0) return { avoid: [] }

  const section: string[] = []
  for (let index = headingIndex + 1; index < lines.length; index++) {
    if (/^##\s+/.test(lines[index])) break
    section.push(lines[index])
  }
  const avoid: string[] = []
  for (const line of section) {
    const match = line.match(/^\s*[-*]\s*([^:：]+?)\s*[:：]\s*(.*?)\s*$/)
    if (!match || !VOICE_FIELD_NAMES.has(match[1].trim().toLocaleLowerCase())) continue
    avoid.push(...parseAvoidTerms(match[2]))
  }
  return { avoid: [...new Set(avoid.filter(Boolean))] }
}

/** Run the zero-token check for a chapter's quoted dialogue. */
export function runVoiceLint(text: string, characters: readonly VoiceLintCharacter[]): { issues: ChapterLintIssue[]; parsedCharacters: number } {
  const parsed = characters
    .map(character => ({
      ...character,
      aliases: character.aliases || [],
      avoid: parseVoiceRules(character.content).avoid,
      names: [character.name, ...(character.aliases || [])].map(value => value.trim()).filter(Boolean),
    }))
    .filter(character => character.names.length > 0 && character.avoid.length > 0)
  if (!parsed.length) return { issues: [], parsedCharacters: 0 }

  const issues: ChapterLintIssue[] = []
  const seen = new Set<string>()
  for (const dialogue of extractDialogueRanges(text)) {
    const speaker = findSpeaker(text, dialogue, parsed)
    const candidates = speaker ? [speaker] : parsed
    const content = text.slice(dialogue.start, dialogue.end)
    let reportedUnknown = false
    for (const character of candidates) {
      for (const term of character.avoid) {
        if (reportedUnknown) break
        const match = findTerm(content, term)
        if (!match) continue
        const key = `${dialogue.start}:${character.names[0]}:${term}`
        if (seen.has(key)) continue
        seen.add(key)
        const start = dialogue.start + match.start
        const occurrence: ChapterLintOccurrence = { start, end: dialogue.start + match.end }
        const unknownSpeaker = !speaker
        const message: ChapterLintMessage = {
          key: unknownSpeaker ? 'editor.lint.issue.voiceUnknown' : 'editor.lint.issue.voiceAvoid',
          params: { name: character.names[0], phrase: term },
        }
        issues.push({
          id: `voice:${start}:${occurrence.end}:${issues.length}`,
          category: 'voice',
          severity: unknownSpeaker ? 'info' : 'warning',
          start,
          end: occurrence.end,
          text: text.slice(start, occurrence.end),
          message,
          occurrences: [occurrence],
        })
        if (!speaker) reportedUnknown = true
      }
      if (reportedUnknown) break
    }
  }
  return { issues, parsedCharacters: parsed.length }
}

function parseAvoidTerms(raw: string) {
  const quoted = [...raw.matchAll(/[「『“\"]([^」』”\"]+)[」』”\"]/g)].map(match => match[1].trim()).filter(Boolean)
  if (quoted.length) return quoted
  return raw
    .split(/[、，,;；]/)
    .map(value => value.trim())
    .map(value => value.replace(/^(?:不会说|不用|不说|避免|avoid)\s*/i, '').trim())
    .filter(Boolean)
}

function extractDialogueRanges(text: string): DialogueRange[] {
  const ranges: DialogueRange[] = []
  for (let index = 0; index < text.length; index++) {
    const opener = text[index]
    const closer = DIALOGUE_PAIRS[opener]
    if (!closer) continue
    const end = text.indexOf(closer, index + 1)
    if (end < 0 || end === index + 1) continue
    ranges.push({ start: index + 1, end, opener, closer })
    index = end
  }
  return ranges
}

function findSpeaker(
  text: string,
  dialogue: DialogueRange,
  characters: Array<VoiceLintCharacter & { aliases: readonly string[]; avoid: string[]; names: string[] }>,
) {
  // `dialogue.start` points just after the opening quote, so exclude that
  // opener before matching the nearby attribution phrase.
  const before = text.slice(Math.max(0, dialogue.start - 120), Math.max(0, dialogue.start - 1))
  const after = text.slice(dialogue.end + 1, Math.min(text.length, dialogue.end + 121))
  const beforeSentence = before.slice(Math.max(0, Math.max(before.lastIndexOf('\n'), before.lastIndexOf('。'), before.lastIndexOf('！'), before.lastIndexOf('？'), before.lastIndexOf('!'), before.lastIndexOf('?')) + 1))
  const afterSentence = after.split(/[。！？!?\n]/, 1)[0]
  for (const character of characters) {
    const names = [...character.names].sort((left, right) => right.length - left.length)
    for (const name of names) {
      const escapedName = escapeRegExp(name)
      const verb = SPEECH_VERBS.map(escapeRegExp).join('|')
      const beforeExpression = new RegExp(`${escapedName}[^。！？!?\\n]{0,24}(?:${verb})\\s*[:：]?\\s*$`, 'i')
      const afterExpression = new RegExp(`^\\s*[,，、:：]?\\s*${escapedName}[^。！？!?\\n]{0,24}(?:${verb})`, 'i')
      if (beforeExpression.test(beforeSentence) || afterExpression.test(afterSentence)) return character
    }
  }
  return undefined
}

function findTerm(value: string, term: string) {
  const flags = /[A-Za-z]/.test(term) ? 'i' : ''
  const expression = new RegExp(escapeRegExp(term), flags)
  const match = expression.exec(value)
  return match ? { start: match.index, end: match.index + match[0].length } : undefined
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

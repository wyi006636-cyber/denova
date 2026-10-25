export type ChapterGoal = {
  anchor: string
  title: string
  goal: string
  conflict: string
  plant: string
  payoff: string
  boundary: string
  wordTarget: string
  hook: string
  sourcePath?: string
}

export type StoryLedgerStatus = 'planned' | 'planted' | 'recovered' | 'deprecated'
export type StoryLedgerFilter = 'all' | StoryLedgerStatus

export type StoryLedgerEntry = {
  id: string
  title: string
  kind: string
  summary: string
  participants: string
  plantedChapter: string
  plannedRecovery: string
  actualRecovery: string
  deprecated: string
  pending: string
  status: StoryLedgerStatus
  startLine: number
  endLine: number
  fieldLines: Partial<Record<LedgerField, number>>
}

type LedgerField =
  | 'kind'
  | 'summary'
  | 'participants'
  | 'plantedChapter'
  | 'plannedRecovery'
  | 'actualRecovery'
  | 'deprecated'
  | 'pending'

const CHAPTER_HEADING = /^###\s+(ch\d{5})(?:[-_ ]+(.*?))?\s*$/i
const LEDGER_HEADING = /^##\s+(t-[A-Za-z0-9][A-Za-z0-9_-]*)(?:\s+(.*?))?\s*$/

const chapterFieldAliases: Record<keyof Omit<ChapterGoal, 'anchor' | 'title' | 'sourcePath'>, string[]> = {
  goal: ['目标', 'goal', 'objective'],
  conflict: ['冲突', 'conflict', 'obstacle'],
  plant: ['要埋', 'plant', 'setup', 'setup/plant'],
  payoff: ['要收', 'payoff', 'resolve', 'recovery'],
  boundary: ['不得越界', 'boundary', 'do not reveal', 'information boundary'],
  wordTarget: ['字数预期', 'word target', 'expected words', 'word count'],
  hook: ['章末钩子', 'hook', 'ending hook', 'chapter hook'],
}

const ledgerFieldAliases: Record<LedgerField, string[]> = {
  kind: ['种类', 'type', 'kind'],
  summary: ['一句话', 'summary', 'one-line', 'one line'],
  participants: ['参与者', 'participants', 'characters'],
  plantedChapter: ['埋设章', 'planted chapter', 'planted'],
  plannedRecovery: ['计划回收', 'planned recovery', 'planned payoff', 'planned'],
  actualRecovery: ['实际回收', 'actual recovery', 'actual payoff', 'recovered chapter', 'recovered'],
  deprecated: ['弃用', 'deprecated', 'abandoned'],
  pending: ['待确认', 'pending', 'pending confirmation', 'to confirm'],
}

const normalizedAliases = new Map<string, string>()
for (const [field, aliases] of Object.entries(chapterFieldAliases)) {
  for (const alias of aliases) normalizedAliases.set(normalizeLabel(alias), field)
}
for (const [field, aliases] of Object.entries(ledgerFieldAliases)) {
  for (const alias of aliases) normalizedAliases.set(normalizeLabel(alias), field)
}

export function parseChapterGoals(markdown: string, sourcePath?: string): Map<string, ChapterGoal> {
  const lines = splitLines(markdown)
  const result = new Map<string, ChapterGoal>()
  let current: ChapterGoal | null = null
  const commit = () => {
    if (current) result.set(current.anchor.toLowerCase(), current)
  }
  lines.forEach((line) => {
    const heading = line.match(CHAPTER_HEADING)
    if (heading) {
      commit()
      current = {
        anchor: heading[1],
        title: heading[2]?.trim() || '',
        goal: '',
        conflict: '',
        plant: '',
        payoff: '',
        boundary: '',
        wordTarget: '',
        hook: '',
        sourcePath,
      }
      return
    }
    if (!current) return
    const field = parseField(line)
    if (!field) return
    const target = normalizedAliases.get(normalizeLabel(field.label)) as keyof typeof chapterFieldAliases | undefined
    if (target && target in current) current[target] = field.value
  })
  commit()
  return result
}

export function parseStoryLedger(markdown: string): StoryLedgerEntry[] {
  const lines = splitLines(markdown)
  const headings: Array<{ index: number; id: string; title: string }> = []
  lines.forEach((line, index) => {
    const match = line.match(LEDGER_HEADING)
    if (match) headings.push({ index, id: match[1], title: match[2]?.trim() || '' })
  })
  return headings.map((heading, headingIndex) => {
    const endLine = headings[headingIndex + 1]?.index ?? lines.length
    const fields: Partial<Record<LedgerField, string>> = {}
    const fieldLines: Partial<Record<LedgerField, number>> = {}
    for (let index = heading.index + 1; index < endLine; index += 1) {
      const field = parseField(lines[index])
      if (!field) continue
      const key = normalizedAliases.get(normalizeLabel(field.label)) as LedgerField | undefined
      if (!key || key in fields) continue
      fields[key] = field.value
      fieldLines[key] = index
    }
    const entry = {
      id: heading.id,
      title: heading.title,
      kind: fields.kind || '',
      summary: fields.summary || '',
      participants: fields.participants || '',
      plantedChapter: fields.plantedChapter || '',
      plannedRecovery: fields.plannedRecovery || '',
      actualRecovery: fields.actualRecovery || '',
      deprecated: fields.deprecated || '',
      pending: fields.pending || '',
      status: ledgerStatus(fields),
      startLine: heading.index,
      endLine,
      fieldLines,
    } satisfies StoryLedgerEntry
    return entry
  })
}

export function ledgerStatus(entry: Pick<StoryLedgerEntry, 'deprecated' | 'actualRecovery' | 'plantedChapter'> | Partial<Record<LedgerField, string>>): StoryLedgerStatus {
  if (isYes(entry.deprecated)) return 'deprecated'
  if (hasChapterPrefix(entry.actualRecovery || '')) return 'recovered'
  if (hasChapterPrefix(entry.plantedChapter || '')) return 'planted'
  return 'planned'
}

export function filterStoryLedger(entries: StoryLedgerEntry[], filter: StoryLedgerFilter): StoryLedgerEntry[] {
  return filter === 'all' ? entries : entries.filter((entry) => entry.status === filter)
}

export function extractChapterPrefix(value: string): string | null {
  const match = value.match(/\b(ch\d{5})\b/i)
  return match?.[1] || null
}

export function patchLedgerEntry(
  markdown: string,
  entry: StoryLedgerEntry,
  action: 'confirm' | 'deprecate',
  chapterPrefix?: string,
): string {
  const lines = splitLines(markdown)
  const replacements = new Map<number, string>()
  if (action === 'confirm') {
    const actual = normalizeChapterPrefix(chapterPrefix || entry.pending)
    if (!actual) return markdown
    setFieldValue(lines, replacements, entry, 'actualRecovery', actual)
    setFieldValue(lines, replacements, entry, 'pending', '')
  } else {
    setFieldValue(lines, replacements, entry, 'deprecated', '是')
  }
  for (const [index, value] of replacements) lines[index] = value
  return joinLines(lines, markdown)
}

function setFieldValue(lines: string[], replacements: Map<number, string>, entry: StoryLedgerEntry, field: LedgerField, value: string) {
  const lineIndex = entry.fieldLines[field]
  if (lineIndex === undefined || lineIndex >= lines.length) return
  const match = lines[lineIndex].match(/^(\s*[-*+]\s*(?:\*\*)?[^：:]+(?:\*\*)?\s*[：:])\s*(.*?)\s*$/)
  if (!match) return
  replacements.set(lineIndex, `${match[1]}${value}`)
}

function parseField(line: string): { label: string; value: string } | null {
  const match = line.match(/^\s*[-*+]\s*(?:\*\*)?([^：:]+?)(?:\*\*)?\s*[：:]\s*(.*?)\s*$/)
  return match ? { label: match[1], value: match[2] } : null
}

function normalizeLabel(value: string) {
  return value.toLocaleLowerCase().replace(/[\s_\-/]+/g, ' ').trim()
}

function splitLines(markdown: string) {
  return markdown.split(/\r?\n/)
}

function joinLines(lines: string[], source: string) {
  return lines.join(source.includes('\r\n') ? '\r\n' : '\n')
}

function normalizeChapterPrefix(value: string) {
  const prefix = extractChapterPrefix(value)
  return prefix ? prefix.toLowerCase() : ''
}

function hasChapterPrefix(value: string) {
  return Boolean(extractChapterPrefix(value))
}

function isYes(value: string | undefined) {
  return /^(是|yes|true|y|弃用|deprecated|abandoned)$/i.test((value || '').trim())
}

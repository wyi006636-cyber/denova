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
export type StoryLedgerFilter = 'all' | StoryLedgerStatus | 'archived'

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
  archived: boolean
  startLine: number
  endLine: number
  fieldLines: Partial<Record<LedgerField, number>>
  fieldLabels: Partial<Record<LedgerField, string>>
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

const canonicalLedgerLabels = {
  zh: {
    actualRecovery: '实际回收',
    pending: '待确认',
    deprecated: '弃用',
  },
  en: {
    actualRecovery: 'Actual recovery',
    pending: 'Pending confirmation',
    deprecated: 'Deprecated',
  },
} as const

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
  const headings: Array<{ index: number; id: string; title: string; archived: boolean }> = []
  let archived = false
  lines.forEach((line, index) => {
    if (/^#\s+/.test(line)) archived = false
    if (/^##\s+(?:归档|archive)\s*$/i.test(line)) {
      archived = true
      return
    }
    const match = line.match(LEDGER_HEADING)
    if (match) headings.push({ index, id: match[1], title: match[2]?.trim() || '', archived })
  })
  return headings.map((heading) => {
    const endLine = nextLedgerBoundary(lines, heading.index)
    const fields: Partial<Record<LedgerField, string>> = {}
    const fieldLines: Partial<Record<LedgerField, number>> = {}
    const fieldLabels: Partial<Record<LedgerField, string>> = {}
    for (let index = heading.index + 1; index < endLine; index += 1) {
      const field = parseField(lines[index])
      if (!field) continue
      const key = normalizedAliases.get(normalizeLabel(field.label)) as LedgerField | undefined
      if (!key || key in fields) continue
      fields[key] = field.value
      fieldLines[key] = index
      fieldLabels[key] = field.label.trim()
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
      archived: heading.archived,
      startLine: heading.index,
      endLine,
      fieldLines,
      fieldLabels,
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
  if (filter === 'archived') return entries.filter((entry) => entry.archived)
  if (filter === 'all') return entries.filter((entry) => !entry.archived)
  return entries.filter((entry) => !entry.archived && entry.status === filter)
}

export function extractChapterPrefix(value: string): string | null {
  const match = value.match(/\b(ch\d{5})\b/i)
  return match?.[1] || null
}

/** Returns every chapter matching a ledger prefix; callers must only navigate on a unique match. */
export function findChapterPrefixMatches<T extends { path: string }>(chapters: readonly T[], value: string): T[] {
  const prefix = extractChapterPrefix(value)?.toLowerCase()
  if (!prefix) return []
  return chapters.filter((chapter) => extractChapterPrefix(chapter.path)?.toLowerCase() === prefix)
}

export function patchLedgerEntry(
  markdown: string,
  entry: StoryLedgerEntry,
  action: 'confirm' | 'deprecate',
  chapterPrefix?: string,
): string {
  const lines = splitLines(markdown)
  const replacements = new Map<number, string>()
  const insertions = new Map<number, string[]>()
  if (!isCurrentEntry(lines, entry)) return markdown
  if (action === 'confirm') {
    const actual = normalizeChapterPrefix(chapterPrefix || entry.pending)
    if (!actual) return markdown
    setFieldValue(lines, replacements, insertions, entry, 'actualRecovery', actual, markdown)
    setFieldValue(lines, replacements, insertions, entry, 'pending', '', markdown)
  } else {
    setFieldValue(lines, replacements, insertions, entry, 'deprecated', deprecatedValue(markdown, entry), markdown)
  }
  for (const [index, value] of replacements) lines[index] = value
  for (const [index, values] of [...insertions.entries()].sort(([left], [right]) => right - left)) {
    lines.splice(index, 0, ...values)
  }
  return joinLines(lines, markdown)
}

function setFieldValue(
  lines: string[],
  replacements: Map<number, string>,
  insertions: Map<number, string[]>,
  entry: StoryLedgerEntry,
  field: LedgerField,
  value: string,
  markdown: string,
) {
  const lineIndex = entry.fieldLines[field]
  if (lineIndex !== undefined && lineIndex < lines.length) {
    const match = lines[lineIndex].match(/^(\s*[-*+]\s*(?:\*\*)?[^：:]+(?:\*\*)?\s*[：:])\s*(.*?)\s*$/)
    if (!match) return
    replacements.set(lineIndex, `${match[1]}${value}`)
    return
  }
  const insertIndex = fieldInsertionIndex(lines, entry)
  const values = insertions.get(insertIndex) ?? []
  values.push(formatInsertedField(lines, entry, field, value, markdown))
  insertions.set(insertIndex, values)
}

function nextLedgerBoundary(lines: string[], startLine: number) {
  for (let index = startLine + 1; index < lines.length; index += 1) {
    if (/^#{1,2}\s+/.test(lines[index])) return index
  }
  return lines.length
}

function fieldInsertionIndex(lines: string[], entry: StoryLedgerEntry) {
  const fieldIndexes = Object.values(entry.fieldLines).filter((index): index is number => typeof index === 'number')
  const lastField = fieldIndexes.length > 0 ? Math.max(...fieldIndexes) : entry.startLine
  return Math.min(lastField + 1, entry.endLine, lines.length)
}

function formatInsertedField(lines: string[], entry: StoryLedgerEntry, field: LedgerField, value: string, markdown: string) {
  const language = ledgerLanguage(markdown, entry)
  const label = field === 'actualRecovery' || field === 'pending' || field === 'deprecated'
    ? canonicalLedgerLabels[language][field]
    : field
  const sampleIndex = Object.values(entry.fieldLines).find((index): index is number => typeof index === 'number')
  const sample = sampleIndex === undefined ? '' : lines[sampleIndex]
  const style = sample.match(/^(\s*[-*+]\s*)(\*\*)?[^：:]+(\*\*)?(\s*[：:])(\s*)/)
  const bullet = style?.[1] || '- '
  const bold = style?.[2] && style?.[3] ? '**' : ''
  const delimiter = style?.[4] || '：'
  const spacing = style?.[5] ?? ''
  return `${bullet}${bold}${label}${bold}${delimiter}${spacing}${value}`
}

function isCurrentEntry(lines: string[], entry: StoryLedgerEntry) {
  return entry.startLine >= 0
    && entry.startLine < lines.length
    && lines[entry.startLine].match(LEDGER_HEADING)?.[1].toLowerCase() === entry.id.toLowerCase()
}

function ledgerLanguage(markdown: string, entry: StoryLedgerEntry): 'zh' | 'en' {
  const labels = Object.values(entry.fieldLabels)
  const entryZh = labels.filter((label) => /[\u3400-\u9fff]/.test(label)).length
  const entryEn = labels.filter((label) => /[A-Za-z]/.test(label)).length
  if (entryZh > 0 || entryEn > 0) return entryZh >= entryEn ? 'zh' : 'en'
  let zh = 0
  let en = 0
  for (const candidate of parseStoryLedger(markdown)) {
    for (const label of Object.values(candidate.fieldLabels)) {
      if (/[\u3400-\u9fff]/.test(label)) zh += 1
      else if (/[A-Za-z]/.test(label)) en += 1
    }
  }
  return en > zh ? 'en' : 'zh'
}

function deprecatedValue(markdown: string, entry: StoryLedgerEntry) {
  if (ledgerLanguage(markdown, entry) === 'zh') return '是'
  const current = entry.fieldLines.deprecated === undefined ? '' : entry.deprecated.trim()
  return /^(yes|true|y)$/i.test(current) ? current : 'yes'
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

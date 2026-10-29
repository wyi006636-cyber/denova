import {
  agentViewContent,
  agentSubAgentSessionKey,
  isAgentSubAgentTimelineView,
  type AgentMessageView,
} from '@/lib/agent-message-view'

/**
 * The receipt is a bounded, display-only projection of one completed run. It
 * deliberately keeps only names and paths, so it cannot become a second
 * context source or change what the model receives.
 */
export const CONTEXT_RECEIPT_MAX_ITEMS = 100

export type ContextReceiptEntryKind = 'chapter' | 'file' | 'lore' | 'browse' | 'author' | 'failure'

export interface ContextReceiptEntry {
  key: string
  kind: ContextReceiptEntryKind
  label: string
  path?: string
  loreID?: string
  detail?: string
  source?: 'agent' | 'subagent'
  status?: 'success' | 'browse' | 'failed'
}

export interface ContextReceiptSource {
  key: string
  label?: string
  chapters: ContextReceiptEntry[]
  files: ContextReceiptEntry[]
  lore: ContextReceiptEntry[]
  browse: ContextReceiptEntry[]
  failures: ContextReceiptEntry[]
  unavailable: boolean
  omittedCount: number
}

export interface ContextReceipt {
  sources: ContextReceiptSource[]
  author: ContextReceiptEntry[]
  authorOmittedCount: number
  hasDetails: boolean
}

interface ReceiptInput {
  /** Views belonging to this run, including child-agent views. */
  runViews: AgentMessageView[]
  /** The user view immediately before the run, when one is available. */
  authorView?: AgentMessageView
  maxItems?: number
}

interface ParsedRead {
  path: string
  kind: string
  status: 'success' | 'failed'
  detail?: string
}

interface ParsedLore {
  id?: string
  name: string
  full: boolean
}

interface ReceiptSourceAccumulator extends ContextReceiptSource {
  seen: Set<string>
  seenBrowse: Set<string>
  seenFailure: Set<string>
}

/** Build a receipt from already projected tool views; never reads the journal. */
export function buildContextReceipt({ runViews, authorView, maxItems = CONTEXT_RECEIPT_MAX_ITEMS }: ReceiptInput): ContextReceipt {
  const limit = Number.isFinite(maxItems) && maxItems > 0 ? Math.floor(maxItems) : CONTEXT_RECEIPT_MAX_ITEMS
  const sources = new Map<string, ReceiptSourceAccumulator>()
  let omittedCount = 0
  let acceptedCount = 0

  const sourceFor = (view: AgentMessageView) => {
    const child = isAgentSubAgentTimelineView(view)
    const key = child ? `subagent:${agentSubAgentSessionKey(view)}` : 'agent'
    let source = sources.get(key)
    if (source) return source
    const name = child
      ? (view.metadata.agent_name || view.metadata.subagent_type || agentSubAgentSessionKey(view))
      : undefined
    source = {
      key,
      label: name?.trim() || undefined,
      chapters: [], files: [], lore: [], browse: [], failures: [],
      unavailable: false, omittedCount: 0,
      seen: new Set(), seenBrowse: new Set(), seenFailure: new Set(),
    }
    sources.set(key, source)
    return source
  }

  const addReference = (source: ReceiptSourceAccumulator, entry: ContextReceiptEntry, bucket: 'chapters' | 'files' | 'lore' | 'browse' | 'failures') => {
    const seen = bucket === 'browse' ? source.seenBrowse : bucket === 'failures' ? source.seenFailure : source.seen
    const key = entry.key
    if (seen.has(key)) return
    seen.add(key)
    if (acceptedCount >= limit) {
      source.omittedCount += 1
      omittedCount += 1
      return
    }
    source[bucket].push(entry)
    acceptedCount += 1
  }

  for (const view of runViews) {
    const source = sourceFor(view)
    const name = (view.toolName || '').trim().toLowerCase()
    if (isReadLikeTool(name)) {
      const parsed = parseReadView(view)
      if (parsed) {
        if (parsed.status === 'failed') {
          addReference(source, {
            key: `failure:file:${normalizeKey(parsed.path)}`,
            kind: 'failure', label: parsed.path, path: parsed.path,
            detail: parsed.detail, source: source.key === 'agent' ? 'agent' : 'subagent', status: 'failed',
          }, 'failures')
        } else if (parsed.kind !== 'directory') {
          const normalized = normalizePath(parsed.path)
          if (normalized) {
            const entry = {
              key: `file:${normalizeKey(normalized)}`,
              kind: isChapterPath(normalized) ? 'chapter' as const : 'file' as const,
              label: normalized,
              path: normalized,
              source: source.key === 'agent' ? 'agent' as const : 'subagent' as const,
              status: 'success' as const,
            }
            addReference(source, entry, entry.kind === 'chapter' ? 'chapters' : 'files')
          }
        }
      } else if (view.status === 'error' || view.status === 'cancelled') {
        const path = readInputPath(view)
        if (path) addReference(source, {
          key: `failure:file:${normalizeKey(path)}`,
          kind: 'failure', label: normalizePath(path) || path, path: normalizePath(path) || path,
          detail: view.errorText || agentViewContent(view),
          source: source.key === 'agent' ? 'agent' : 'subagent', status: 'failed',
        }, 'failures')
      } else if (view.status === 'success' && readInputPath(view)) {
        // External runtimes may omit Denova's result envelope. A settled read
        // with a concrete path is still safe to show as a successful read.
        const path = normalizePath(readInputPath(view))
        if (path) {
          const entry = {
            key: `file:${normalizeKey(path)}`,
            kind: isChapterPath(path) ? 'chapter' as const : 'file' as const,
            label: path, path, source: source.key === 'agent' ? 'agent' as const : 'subagent' as const,
            status: 'success' as const,
          }
          addReference(source, entry, entry.kind === 'chapter' ? 'chapters' : 'files')
        }
      } else if (view.status === 'success' || view.status === undefined) {
        source.unavailable = true
      }
      continue
    }

    if (isLoreQueryTool(name)) {
      const output = toolOutputText(view)
      if (view.status === 'error' || view.status === 'cancelled') {
        addReference(source, {
          key: `failure:lore:${view.partId || view.messageId}`,
          kind: 'failure', label: toolDisplayLabel(view), detail: view.errorText || output,
          source: source.key === 'agent' ? 'agent' : 'subagent', status: 'failed',
        }, 'failures')
        continue
      }
      if (!output && view.status !== 'success') {
        source.unavailable = true
        continue
      }
      const entries = parseLoreEntries(output)
      if (!entries.length && output && view.status === 'success') {
        // We know this was a lore operation, but its provider-specific shape
        // did not expose item names. Keep the honest fallback in the UI.
        source.unavailable = true
      }
      for (const item of entries) {
        const id = item.id?.trim()
        const key = `${id || item.name.toLocaleLowerCase()}`
        if (item.full) {
          addReference(source, {
            key: `lore:${normalizeKey(key)}`,
            kind: 'lore', label: item.name, loreID: id,
            source: source.key === 'agent' ? 'agent' : 'subagent', status: 'success',
          }, 'lore')
        } else {
          addReference(source, {
            key: `browse:${normalizeKey(key)}`,
            kind: 'browse', label: item.name, loreID: id,
            source: source.key === 'agent' ? 'agent' : 'subagent', status: 'browse',
          }, 'browse')
        }
      }
    }
    if (name && !isReadLikeTool(name) && !isLoreQueryTool(name) && /(read|lore|workspace[_-]?file)/i.test(name)) {
      // A provider-specific read event cannot be safely decoded here. Keep an
      // explicit fallback instead of implying that the turn read nothing.
      source.unavailable = true
    }
  }

  const author = collectAuthorReferences(authorView, Math.max(0, limit - acceptedCount), (count) => { omittedCount += count })
  const orderedSources = [...sources.values()].map(({ seen, seenBrowse, seenFailure, ...source }) => source)
  return {
    sources: orderedSources,
    author: author.entries,
    authorOmittedCount: author.omittedCount,
    hasDetails: orderedSources.some(source => source.chapters.length > 0 || source.files.length > 0 || source.lore.length > 0 || source.browse.length > 0 || source.failures.length > 0 || source.unavailable)
      || author.entries.length > 0 || omittedCount > 0,
  }
}

function collectAuthorReferences(view: AgentMessageView | undefined, limit: number, onOmitted: (count: number) => void) {
  const entries: ContextReceiptEntry[] = []
  const seen = new Set<string>()
  let omittedCount = 0
  const add = (entry: ContextReceiptEntry) => {
    if (seen.has(entry.key)) return
    seen.add(entry.key)
    if (entries.length >= limit) {
      omittedCount += 1
      onOmitted(1)
      return
    }
    entries.push(entry)
  }
  if (!view) return { entries, omittedCount }
  for (const reference of view.metadata.user_references || []) {
    const label = reference.label.trim()
    if (!label) continue
    const path = reference.kind === 'file' || reference.kind === 'selection' || reference.kind === 'review_comment'
      ? normalizePath(label) || label
      : undefined
    const detail = reference.kind === 'selection' && reference.start_line !== undefined
      ? `${reference.start_line}-${reference.end_line ?? reference.start_line}`
      : reference.detail
    add({
      key: `author:${reference.kind}:${normalizeKey(reference.id || path || label)}:${reference.start_line || ''}:${reference.end_line || ''}`,
      kind: 'author', label: path || label, path, loreID: reference.kind === 'lore' ? reference.id : undefined,
      detail, status: 'success',
    })
  }
  for (const attachment of view.metadata.attachments || []) {
    const label = attachment.name.trim()
    if (!label) continue
    add({ key: `author:attachment:${normalizeKey(attachment.id || label)}`, kind: 'author', label, detail: attachment.media_type, status: 'success' })
  }
  return { entries, omittedCount }
}

function isReadLikeTool(name: string) {
  return name === 'read' || name === 'read_file' || name === 'read_workspace_file' || name === 'read_text_file' || name === 'read_resource'
}

function isLoreQueryTool(name: string) {
  return name === 'query_lore_items' || name === 'list_lore_items' || name === 'read_lore_items'
}

function parseReadView(view: AgentMessageView): ParsedRead | null {
  const pathFromInput = readInputPath(view)
  const output = toolOutputText(view)
  const envelope = parseJSONPrefix(output)
  if (envelope && envelope.schema === 'resource.read.v1') {
    const source = asRecord(envelope.source)
    const path = stringValue(source?.path) || pathFromInput
    const status = stringValue(envelope.status).toLowerCase()
    return {
      path,
      kind: stringValue(source?.kind).toLowerCase(),
      status: status === 'success' || status === 'partial' ? 'success' : 'failed',
      detail: status === 'not_found' ? output.split('\n').slice(1).join('\n').trim() : undefined,
    }
  }
  if (view.status === 'error' || view.status === 'cancelled') {
    return pathFromInput ? { path: pathFromInput, kind: '', status: 'failed', detail: view.errorText || output } : null
  }
  if (view.status === 'success' && pathFromInput) return { path: pathFromInput, kind: '', status: 'success' }
  return null
}

function parseLoreEntries(output: string): ParsedLore[] {
  if (!output.trim()) return []
  const parsed = parseJSONValue(output)
  if (parsed) {
    const entries: ParsedLore[] = []
    collectJSONLoreEntries(parsed, entries)
    if (entries.length) return dedupeLoreEntries(entries)
  }
  const entries: ParsedLore[] = []
  const headings = [...output.matchAll(/^##\s+(.+?)(?:\s+\([^\n]+\))?\s*$/gm)]
  for (let index = 0; index < headings.length; index += 1) {
    const heading = headings[index]
    const start = (heading.index || 0) + heading[0].length
    const end = index + 1 < headings.length ? headings[index + 1].index || output.length : output.length
    const body = output.slice(start, end)
    const id = body.match(/^ID:\s*(.+)$/m)?.[1]?.trim()
    const fullBody = body.match(/```markdown\s*\n([\s\S]*?)\n```/m)?.[1]?.trim() || ''
    entries.push({ id, name: heading[1].trim(), full: Boolean(fullBody) })
  }
  for (const match of output.matchAll(/^-\s*id:\s*([^|\n]+?)(?:\s*\|\s*name:\s*(.+))?\s*$/gm)) {
    const id = match[1].trim()
    const name = match[2]?.trim()
    if (name) entries.push({ id, name, full: false })
  }
  for (const match of output.matchAll(/^-\s*id:\s*([^\n]+)\n\s+name:\s*(.+)$/gm)) {
    entries.push({ id: match[1].trim(), name: match[2].trim(), full: false })
  }
  return dedupeLoreEntries(entries)
}

function collectJSONLoreEntries(value: unknown, entries: ParsedLore[]) {
  if (Array.isArray(value)) {
    for (const item of value) collectJSONLoreEntries(item, entries)
    return
  }
  const record = asRecord(value)
  if (!record) return
  const name = stringValue(record.name) || stringValue(record.title)
  const id = stringValue(record.id) || stringValue(record.item_id)
  const body = stringValue(record.content) || stringValue(record.body)
  if (name || id) entries.push({ id: id || undefined, name: name || id, full: Boolean(body.trim()) })
  for (const key of ['items', 'entries', 'results', 'lore_items', 'data']) {
    if (record[key] !== undefined) collectJSONLoreEntries(record[key], entries)
  }
}

function dedupeLoreEntries(entries: ParsedLore[]) {
  const byKey = new Map<string, ParsedLore>()
  for (const entry of entries) {
    if (!entry.name.trim()) continue
    const key = normalizeKey(entry.id || entry.name)
    const previous = byKey.get(key)
    byKey.set(key, previous ? { ...previous, full: previous.full || entry.full } : { ...entry, name: entry.name.trim() })
  }
  return [...byKey.values()]
}

function readInputPath(view: AgentMessageView) {
  const input = asRecord(view.input)
  return stringValue(input?.path) || stringValue(input?.file_path) || stringValue(input?.file)
}

function toolOutputText(view: AgentMessageView) {
  const value = view.output ?? view.data.result ?? view.data.output ?? (view.kind === 'tool-result' ? view.content : '')
  if (typeof value === 'string') return value
  if (value === undefined || value === null) return ''
  try { return JSON.stringify(value) } catch { return String(value) }
}

function parseJSONPrefix(value: string): Record<string, unknown> | null {
  const firstLine = value.split('\n', 1)[0]?.trim() || ''
  return asRecord(parseJSONValue(firstLine))
}

function parseJSONValue(value: string): unknown {
  try { return JSON.parse(value) } catch { return null }
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null
}

function stringValue(value: unknown) {
  return typeof value === 'string' ? value : ''
}

function normalizePath(value: string) {
  const normalized = value.trim().replace(/\\/g, '/').replace(/^\.\//, '').replace(/\/+/g, '/')
  // The receipt is scoped to the current workspace. Without a workspace root
  // here, an absolute host path cannot be safely projected as a project file.
  if (normalized.startsWith('/') || /^[A-Za-z]:\//.test(normalized)) return ''
  return normalized === '.' ? '' : normalized
}

function normalizeKey(value: string) {
  return value.trim().replace(/\\/g, '/').toLocaleLowerCase()
}

function isChapterPath(path: string) {
  const normalized = path.toLocaleLowerCase()
  return normalized === 'chapters' || normalized.startsWith('chapters/') || normalized.startsWith('chapter/')
}

function toolDisplayLabel(view: AgentMessageView) {
  return view.toolName || 'lore'
}

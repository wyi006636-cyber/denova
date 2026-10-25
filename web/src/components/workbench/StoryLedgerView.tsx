import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Check, ChevronDown, ChevronRight, Edit3, ExternalLink, Loader2, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { ChapterSummary } from '@/lib/api'
import {
  extractChapterPrefix,
  filterStoryLedger,
  parseStoryLedger,
  patchLedgerEntry,
  type StoryLedgerEntry,
  type StoryLedgerFilter,
} from './story-ledger-parser'

interface StoryLedgerViewProps {
  path: string
  content: string
  revision: string
  chapters: ChapterSummary[]
  onSelectChapter: (path: string) => void | Promise<void>
  onSave: (path: string, content: string, baseRevision: string) => Promise<{ revision?: string }>
  onEditOriginal: () => void
  rawEditor: ReactNode
}

export function StoryLedgerView({
  path,
  content,
  revision,
  chapters,
  onSelectChapter,
  onSave,
  onEditOriginal,
  rawEditor,
}: StoryLedgerViewProps) {
  const { t } = useTranslation()
  const [listContent, setListContent] = useState(content)
  const [listRevision, setListRevision] = useState(revision)
  const [filter, setFilter] = useState<StoryLedgerFilter>('all')
  const [expandedID, setExpandedID] = useState<string | null>(null)
  const [savingID, setSavingID] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [raw, setRaw] = useState(false)

  useEffect(() => {
    setListContent(content)
    setListRevision(revision)
  }, [content, revision])

  const entries = useMemo(() => parseStoryLedger(listContent), [listContent])
  const visibleEntries = useMemo(() => filterStoryLedger(entries, filter), [entries, filter])
  const chapterByPrefix = useMemo(() => {
    const map = new Map<string, ChapterSummary[]>()
    for (const chapter of chapters) {
      const prefix = extractChapterPrefix(chapter.path)
      if (!prefix) continue
      const key = prefix.toLowerCase()
      map.set(key, [...(map.get(key) ?? []), chapter])
    }
    return map
  }, [chapters])

  const applyAction = async (entry: StoryLedgerEntry, action: 'confirm' | 'deprecate') => {
    if (savingID) return
    const chapter = action === 'confirm' ? extractChapterPrefix(entry.pending) : undefined
    if (action === 'confirm' && !chapter) {
      setError(t('planning.ledgerPendingChapterMissing'))
      return
    }
    const nextContent = patchLedgerEntry(listContent, entry, action, chapter || undefined)
    if (nextContent === listContent) return
    setSavingID(entry.id)
    setError('')
    try {
      const result = await onSave(path, nextContent, listRevision)
      setListContent(nextContent)
      if (result.revision) setListRevision(result.revision)
    } catch (cause) {
      console.error('[StoryLedgerView.tsx] failed to save story ledger action', { path, entry: entry.id, action, cause })
      setError(t('planning.ledgerSaveFailed'))
    } finally {
      setSavingID(null)
    }
  }

  if (raw) {
    return (
      <div className="relative flex min-h-0 flex-1 flex-col">
        <div className="flex shrink-0 items-center justify-between border-b border-[var(--nova-border)] bg-[var(--nova-surface)] px-3 py-1.5">
          <span className="text-[11px] font-medium text-[var(--nova-text-muted)]">{t('planning.storyLedger')}</span>
          <button type="button" className="nova-nav-item inline-flex items-center gap-1 rounded px-2 py-1 text-[11px]" onClick={() => setRaw(false)}>
            <ChevronRight className="h-3 w-3" />{t('planning.ledgerBackToList')}
          </button>
        </div>
        <div className="min-h-0 flex-1">{rawEditor}</div>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-[var(--nova-bg)]">
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-[var(--nova-border)] bg-[var(--nova-surface)] px-3 py-2">
        <div className="min-w-0">
          <h2 className="truncate text-sm font-medium text-[var(--nova-text)]">{t('planning.storyLedger')}</h2>
          <p className="truncate text-[10px] text-[var(--nova-text-faint)]">{path}</p>
        </div>
        <button type="button" className="nova-nav-item inline-flex items-center gap-1 rounded px-2 py-1.5 text-[11px]" onClick={() => { setRaw(true); onEditOriginal() }}>
          <Edit3 className="h-3 w-3" />{t('planning.ledgerEditOriginal')}
        </button>
      </div>
      <div className="shrink-0 overflow-x-auto border-b border-[var(--nova-border)] px-3 py-2">
        <div className="flex min-w-max gap-1" role="tablist" aria-label={t('planning.ledgerFilters')}>
          {(['all', 'planned', 'planted', 'recovered', 'deprecated'] as StoryLedgerFilter[]).map((value) => (
            <button key={value} type="button" role="tab" aria-selected={filter === value} className={`nova-nav-item rounded px-2.5 py-1 text-[11px] ${filter === value ? 'is-active' : ''}`} onClick={() => setFilter(value)}>
              {t(`planning.ledgerFilter.${value}`)}
            </button>
          ))}
        </div>
      </div>
      {error ? <div role="alert" className="mx-3 mt-2 rounded border border-[var(--nova-danger)]/40 bg-[var(--nova-danger-bg)] px-2.5 py-2 text-[11px] text-[var(--nova-danger)]">{error}</div> : null}
      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        {entries.length === 0 ? (
          <div className="rounded border border-dashed border-[var(--nova-border)] px-3 py-8 text-center text-xs text-[var(--nova-text-faint)]">{t('planning.ledgerEmpty')}</div>
        ) : visibleEntries.length === 0 ? (
          <div className="rounded border border-dashed border-[var(--nova-border)] px-3 py-8 text-center text-xs text-[var(--nova-text-faint)]">{t('planning.ledgerNoMatches')}</div>
        ) : (
          <div className="mx-auto max-w-4xl space-y-2">
            {visibleEntries.map((entry) => {
              const expanded = expandedID === entry.id
              const saving = savingID === entry.id
              return (
                <div key={entry.id} className="rounded border border-[var(--nova-border)] bg-[var(--nova-surface)]">
                  <button type="button" className="flex w-full min-w-0 items-center gap-2 px-3 py-2.5 text-left" aria-expanded={expanded} onClick={() => setExpandedID(expanded ? null : entry.id)}>
                    {expanded ? <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--nova-text-faint)]" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[var(--nova-text-faint)]" />}
                    <span className="shrink-0 rounded bg-[var(--nova-surface-2)] px-1.5 py-0.5 font-mono text-[10px] text-[var(--nova-text-muted)]">{entry.id}</span>
                    <span className="min-w-0 flex-1 truncate text-xs font-medium text-[var(--nova-text)]">{entry.title || entry.summary || t('planning.ledgerUntitled')}</span>
                    <span className="shrink-0 rounded border border-[var(--nova-border)] px-1.5 py-0.5 text-[10px] text-[var(--nova-text-muted)]">{t(`planning.ledgerStatus.${entry.status}`)}</span>
                    <span className="hidden shrink-0 text-[10px] text-[var(--nova-text-faint)] sm:inline">{entry.plantedChapter || '—'} → {entry.plannedRecovery || '—'}</span>
                  </button>
                  {expanded ? (
                    <div className="space-y-3 border-t border-[var(--nova-border)] px-3 py-3 text-xs">
                      <p className="whitespace-pre-wrap break-words text-[var(--nova-text-muted)]">{entry.summary || t('planning.ledgerNoSummary')}</p>
                      <dl className="grid min-w-0 gap-1.5 sm:grid-cols-2">
                        <LedgerField label={t('planning.ledgerParticipants')} value={entry.participants} />
                        <LedgerField label={t('planning.ledgerPlantedChapter')} value={entry.plantedChapter} chapterByPrefix={chapterByPrefix} onSelectChapter={onSelectChapter} />
                        <LedgerField label={t('planning.ledgerPlannedRecovery')} value={entry.plannedRecovery} chapterByPrefix={chapterByPrefix} onSelectChapter={onSelectChapter} />
                        <LedgerField label={t('planning.ledgerActualRecovery')} value={entry.actualRecovery} chapterByPrefix={chapterByPrefix} onSelectChapter={onSelectChapter} />
                        <LedgerField label={t('planning.ledgerPending')} value={entry.pending} chapterByPrefix={chapterByPrefix} onSelectChapter={onSelectChapter} />
                      </dl>
                      <div className="flex flex-wrap items-center gap-2">
                        <button type="button" disabled={saving || entry.status === 'recovered' || entry.status === 'deprecated'} className="nova-nav-item inline-flex items-center gap-1 rounded border border-[var(--nova-border)] px-2 py-1.5 text-[11px] disabled:cursor-not-allowed disabled:opacity-45" onClick={() => void applyAction(entry, 'confirm')}>
                          {saving ? <Loader2 className="h-3 w-3 animate-spin" /> : <Check className="h-3 w-3" />}{t('planning.ledgerConfirmRecovery')}
                        </button>
                        <button type="button" disabled={saving || entry.status === 'deprecated'} className="nova-nav-item inline-flex items-center gap-1 rounded border border-[var(--nova-border)] px-2 py-1.5 text-[11px] disabled:cursor-not-allowed disabled:opacity-45" onClick={() => void applyAction(entry, 'deprecate')}>
                          <Trash2 className="h-3 w-3" />{t('planning.ledgerMarkDeprecated')}
                        </button>
                      </div>
                    </div>
                  ) : null}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

function LedgerField({
  label,
  value,
  chapterByPrefix,
  onSelectChapter,
}: {
  label: string
  value: string
  chapterByPrefix?: ReadonlyMap<string, ChapterSummary[]>
  onSelectChapter?: (path: string) => void | Promise<void>
}) {
  const { t } = useTranslation()
  const prefix = extractChapterPrefix(value)
  const matches = prefix ? chapterByPrefix?.get(prefix.toLowerCase()) ?? [] : []
  return (
    <div className="grid min-w-0 grid-cols-[auto,minmax(0,1fr)] gap-2">
      <dt className="text-[var(--nova-text-faint)]">{label}</dt>
      <dd className="min-w-0 break-words text-[var(--nova-text-muted)]">
        {value ? <span>{value}</span> : <span>—</span>}
        {prefix && matches.length === 1 && onSelectChapter ? (
          <button type="button" className="ml-1 inline-flex max-w-full items-center gap-0.5 truncate text-[var(--nova-accent)] hover:underline" onClick={() => void onSelectChapter(matches[0].path)}>
            <ExternalLink className="h-3 w-3 shrink-0" />{t('planning.ledgerOpenChapter', { chapter: prefix })}
          </button>
        ) : prefix ? (
          <span className="ml-1 text-[var(--nova-warning)]">{t('planning.ledgerChapterNotFound', { chapter: prefix })}</span>
        ) : null}
      </dd>
    </div>
  )
}

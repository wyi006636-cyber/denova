import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { BookOpen, Check, ChevronDown, ChevronRight, FileUp, Loader2, MessageCircle, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { APIError } from '@/lib/api-client/client'
import { applyProjectFileOperations } from '@/lib/api-client/project-files'
import { getCasebook, toggleCasebookCase, type CaseEntry } from '@/lib/api-client/casebook'
import { WRITING_AGENT_INIT_EVENT } from '@/features/onboarding/events'
import { caseMethodPrompt } from '@/features/writing/casebook-method'
import { Button } from '@/components/ui/button'

interface CasebookViewProps {
  projectId: string
  path: string
  onSelectFile: (path: string) => void | Promise<void>
  rawEditor: ReactNode
}

type Filter = 'all' | 'collected' | 'pending'

export function CasebookView({ projectId, path, onSelectFile, rawEditor }: CasebookViewProps) {
  const { t } = useTranslation()
  const [cases, setCases] = useState<CaseEntry[]>([])
  const [indexRevision, setIndexRevision] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [direction, setDirection] = useState('all')
  const [expandedID, setExpandedID] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [savingID, setSavingID] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [raw, setRaw] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const reload = async () => {
    setLoading(true)
    try {
      const snapshot = await getCasebook(projectId)
      setCases(snapshot.cases)
      setIndexRevision(snapshot.index.revision)
      setError('')
    } catch (cause) {
      console.error('[CasebookView.tsx] failed to load casebook', { projectId, cause })
      setError(t('planning.casebook.loadFailed'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { void reload() }, [projectId]) // eslint-disable-line react-hooks/exhaustive-deps

  const directions = useMemo(() => Array.from(new Set(cases.map((item) => item.direction.trim()).filter(Boolean))), [cases])
  const visibleCases = useMemo(() => cases.filter((item) => {
    const statusMatches = filter === 'all' || (filter === 'collected' && item.status === '已收藏') || (filter === 'pending' && item.status !== '已收藏')
    return statusMatches && (direction === 'all' || item.direction === direction)
  }), [cases, direction, filter])

  const toggle = async (item: CaseEntry) => {
    if (!item.path || !item.revision || savingID) return
    setSavingID(item.id)
    setError('')
    try {
      const response = await toggleCasebookCase(projectId, {
        path: item.path,
        case_revision: item.revision,
        index_revision: indexRevision,
        collected: item.status !== '已收藏',
      })
      setIndexRevision(response.index.revision)
      setCases((current) => current.map((candidate) => candidate.id === item.id ? { ...candidate, ...response.case, revision: response.case.revision } : candidate))
    } catch (cause) {
      console.error('[CasebookView.tsx] failed to toggle case status', { projectId, caseID: item.id, cause })
      setError(localizeCasebookError(cause, t))
      await reload()
    } finally {
      setSavingID(null)
    }
  }

  const bringMethod = (item: CaseEntry) => {
    if (item.status !== '已收藏') return
    const prompt = caseMethodPrompt(item, {
      direction: t('planning.casebook.direction'),
      scenario: t('planning.casebook.scenario'),
      method: t('planning.casebook.method'),
      avoid: t('planning.casebook.avoid'),
    })
    if (!prompt) return
    window.dispatchEvent(new CustomEvent(WRITING_AGENT_INIT_EVENT, { detail: { prompt, autoSend: false } }))
  }

  const importSource = async (file: File) => {
    const requestedName = safeSourceName(file.name)
    if (!requestedName) {
      setError(t('planning.casebook.importInvalidName'))
      return
    }
    try {
      const content = await file.text()
      const extension = requestedName.includes('.') ? requestedName.slice(requestedName.lastIndexOf('.')) : '.txt'
      const stem = extension ? requestedName.slice(0, -extension.length) : requestedName
      let imported = false
      for (let suffix = 0; suffix < 100 && !imported; suffix++) {
        const name = suffix === 0 ? requestedName : `${stem}-${suffix + 1}${extension}`
        const results = await applyProjectFileOperations(projectId, [
          { kind: 'create', path: 'references', type: 'dir' },
          { kind: 'create', path: 'references/sources', type: 'dir' },
          { kind: 'create', path: `references/sources/${name}`, type: 'file', content },
        ])
        const last = results[results.length - 1]
        if (last?.ok) {
          imported = true
          break
        }
        if (last?.code !== 'target_exists') throw new Error(last?.code || 'import_failed')
      }
      if (!imported) throw new Error('import_name_exhausted')
      setError('')
      await reload()
    } catch (cause) {
      console.error('[CasebookView.tsx] failed to import source', { projectId, name: requestedName, cause })
      setError(t('planning.casebook.importFailed'))
    } finally {
      if (inputRef.current) inputRef.current.value = ''
    }
  }

  if (raw) {
    return (
      <div className="relative flex min-h-0 flex-1 flex-col">
        <div className="flex shrink-0 items-center justify-between border-b border-[var(--nova-border)] bg-[var(--nova-surface)] px-3 py-1.5">
          <span className="text-[11px] font-medium text-[var(--nova-text-muted)]">{t('planning.casebook.title')}</span>
          <button type="button" className="nova-nav-item inline-flex items-center gap-1 rounded px-2 py-1 text-[11px]" onClick={() => setRaw(false)}><ChevronRight className="h-3 w-3" />{t('planning.casebook.backToList')}</button>
        </div>
        <div className="min-h-0 flex-1">{rawEditor}</div>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-[var(--nova-bg)]">
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-[var(--nova-border)] bg-[var(--nova-surface)] px-3 py-2">
        <div className="min-w-0">
          <h2 className="truncate text-sm font-medium text-[var(--nova-text)]">{t('planning.casebook.title')}</h2>
          <p className="truncate text-[10px] text-[var(--nova-text-faint)]">{path}</p>
        </div>
        <div className="flex flex-wrap items-center gap-1">
          <input ref={inputRef} type="file" accept=".txt,.md,text/plain,text/markdown" className="hidden" onChange={(event) => { const file = event.target.files?.[0]; if (file) void importSource(file) }} />
          <Button type="button" variant="ghost" size="sm" className="h-7 gap-1 px-2 text-[11px]" onClick={() => inputRef.current?.click()}><FileUp className="h-3.5 w-3.5" />{t('planning.casebook.importSource')}</Button>
          <Button type="button" variant="ghost" size="sm" className="h-7 gap-1 px-2 text-[11px]" onClick={() => setRaw(true)}><BookOpen className="h-3.5 w-3.5" />{t('planning.casebook.editOriginal')}</Button>
        </div>
      </div>
      <div className="shrink-0 space-y-2 border-b border-[var(--nova-border)] px-3 py-2">
        <div className="flex flex-wrap gap-1" role="tablist" aria-label={t('planning.casebook.filters')}>
          {(['all', 'collected', 'pending'] as Filter[]).map((value) => <button key={value} type="button" role="tab" aria-selected={filter === value} className={`nova-nav-item rounded px-2.5 py-1 text-[11px] ${filter === value ? 'is-active' : ''}`} onClick={() => setFilter(value)}>{t(`planning.casebook.filter.${value}`)}</button>)}
        </div>
        {directions.length > 0 ? <div className="flex flex-wrap gap-1" aria-label={t('planning.casebook.directionFilter')}><button type="button" className={`nova-nav-item rounded px-2 py-1 text-[10px] ${direction === 'all' ? 'is-active' : ''}`} onClick={() => setDirection('all')}>{t('planning.casebook.allDirections')}</button>{directions.map((value) => <button key={value} type="button" className={`nova-nav-item rounded px-2 py-1 text-[10px] ${direction === value ? 'is-active' : ''}`} onClick={() => setDirection(value)}>{value}</button>)}</div> : null}
      </div>
      {error ? <div role="alert" className="mx-3 mt-2 rounded border border-[var(--nova-danger)]/40 bg-[var(--nova-danger-bg)] px-2.5 py-2 text-[11px] text-[var(--nova-danger)]">{error}</div> : null}
      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        {loading ? <div className="flex items-center justify-center gap-2 py-12 text-xs text-[var(--nova-text-faint)]"><Loader2 className="h-4 w-4 animate-spin" />{t('planning.casebook.loading')}</div> : cases.length === 0 ? <EmptyCasebook onImport={() => inputRef.current?.click()} t={t} /> : visibleCases.length === 0 ? <div className="rounded border border-dashed border-[var(--nova-border)] px-3 py-8 text-center text-xs text-[var(--nova-text-faint)]">{t('planning.casebook.noMatches')}</div> : <div className="mx-auto max-w-4xl space-y-2">{visibleCases.map((item) => <CaseRow key={item.id} item={item} expanded={expandedID === item.id} saving={savingID === item.id} onToggle={() => setExpandedID(expandedID === item.id ? null : item.id)} onStatus={() => void toggle(item)} onBring={() => bringMethod(item)} onOpenSource={() => { if (item.source) void onSelectFile(item.source) }} t={t} />)}</div>}
      </div>
    </div>
  )
}

function CaseRow({ item, expanded, saving, onToggle, onStatus, onBring, onOpenSource, t }: { item: CaseEntry; expanded: boolean; saving: boolean; onToggle: () => void; onStatus: () => void; onBring: () => void; onOpenSource: () => void; t: (key: string, options?: Record<string, unknown>) => string }) {
  return <div className="rounded border border-[var(--nova-border)] bg-[var(--nova-surface)]"><button type="button" className="flex w-full min-w-0 items-center gap-2 px-3 py-2.5 text-left" aria-expanded={expanded} onClick={onToggle}>{expanded ? <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--nova-text-faint)]" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[var(--nova-text-faint)]" />}<span className="shrink-0 rounded bg-[var(--nova-surface-2)] px-1.5 py-0.5 font-mono text-[10px] text-[var(--nova-text-muted)]">{item.id}</span><span className="min-w-0 flex-1 truncate text-xs font-medium text-[var(--nova-text)]">{item.title || item.method || t('planning.casebook.untitled')}</span><span className="shrink-0 rounded border border-[var(--nova-border)] px-1.5 py-0.5 text-[10px] text-[var(--nova-text-muted)]">{item.status === '已收藏' ? t('planning.casebook.status.collected') : t('planning.casebook.status.pending')}</span></button>{expanded ? <div className="space-y-3 border-t border-[var(--nova-border)] px-3 py-3 text-xs"><dl className="grid min-w-0 gap-1.5 sm:grid-cols-2"><Field label={t('planning.casebook.direction')} value={item.direction} /><Field label={t('planning.casebook.scenario')} value={item.scenario} /><Field label={t('planning.casebook.method')} value={item.method} /><Field label={t('planning.casebook.avoid')} value={item.avoid} /></dl><div className="rounded bg-[var(--nova-surface-2)] p-2.5"><div className="text-[10px] text-[var(--nova-text-faint)]">{t('planning.casebook.excerpt')} · {item.location || '—'}</div><p className="mt-1 whitespace-pre-wrap break-words text-[var(--nova-text-muted)]">{item.quote || t('planning.casebook.noExcerpt')}</p></div>{item.source ? <div className="flex flex-wrap items-center gap-2 text-[10px] text-[var(--nova-text-faint)]"><span className={item.source_exists === false ? 'text-[var(--nova-warning)]' : ''}>{item.source}</span>{item.source_exists !== false ? <button type="button" className="text-[var(--nova-accent)] hover:underline" onClick={onOpenSource}>{t('planning.casebook.openSource')}</button> : <span>{t('planning.casebook.sourceMissing')}</span>}</div> : null}<div className="flex flex-wrap items-center gap-2"><Button type="button" size="sm" variant={item.status === '已收藏' ? 'outline' : 'default'} className="h-7 gap-1 text-[11px]" disabled={saving || !item.path || !item.revision} onClick={onStatus}>{saving ? <Loader2 className="h-3 w-3 animate-spin" /> : item.status === '已收藏' ? <RotateCcw className="h-3 w-3" /> : <Check className="h-3 w-3" />}{item.status === '已收藏' ? t('planning.casebook.unfavorite') : t('planning.casebook.favorite')}</Button><Button type="button" size="sm" variant="outline" className="h-7 gap-1 text-[11px]" disabled={item.status !== '已收藏'} onClick={onBring}><MessageCircle className="h-3 w-3" />{t('planning.casebook.bringMethod')}</Button></div></div> : null}</div>
}

function Field({ label, value }: { label: string; value?: string }) { return <div className="grid min-w-0 grid-cols-[auto,minmax(0,1fr)] gap-2"><dt className="text-[var(--nova-text-faint)]">{label}</dt><dd className="min-w-0 whitespace-pre-wrap break-words text-[var(--nova-text-muted)]">{value || '—'}</dd></div> }

function EmptyCasebook({ onImport, t }: { onImport: () => void; t: (key: string) => string }) { return <div className="mx-auto flex max-w-lg flex-col items-center rounded border border-dashed border-[var(--nova-border)] px-5 py-12 text-center"><BookOpen className="mb-3 h-8 w-8 text-[var(--nova-text-faint)]" /><h3 className="text-sm font-medium text-[var(--nova-text)]">{t('planning.casebook.emptyTitle')}</h3><p className="mt-1 text-xs leading-5 text-[var(--nova-text-faint)]">{t('planning.casebook.emptyDescription')}</p><Button type="button" variant="outline" size="sm" className="mt-4 gap-1 text-xs" onClick={onImport}><FileUp className="h-3.5 w-3.5" />{t('planning.casebook.importSource')}</Button></div> }

function safeSourceName(name: string) {
  let normalized = name.trim().replace(/[^\p{L}\p{N}._-]+/gu, '-').replace(/^-+|-+$/g, '').replace(/[ .]+$/g, '')
  if (!normalized || normalized === '.' || normalized === '..') return ''
  const extension = normalized.includes('.') ? normalized.slice(normalized.lastIndexOf('.')).toLowerCase() : '.txt'
  const stem = (extension === '.txt' || extension === '.md') ? normalized.slice(0, -extension.length) : normalized
  const safeStem = /^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i.test(stem) ? `file-${stem}` : stem
  const boundedStem = safeStem.slice(0, 180).replace(/[ .]+$/g, '') || 'source'
  return (extension === '.txt' || extension === '.md') ? `${boundedStem}${extension}` : `${boundedStem}.txt`
}

function localizeCasebookError(error: unknown, t: (key: string) => string) {
  if (!(error instanceof APIError)) return t('planning.casebook.actionFailed')
  switch (error.code) {
    case 'case_quote_missing': return t('planning.casebook.error.quoteMissing')
    case 'case_quote_ambiguous': return t('planning.casebook.error.quoteAmbiguous')
    case 'case_quote_too_long': return t('planning.casebook.error.quoteTooLong')
    case 'case_source_missing': return t('planning.casebook.error.sourceMissing')
    case 'case_source_outside_book': return t('planning.casebook.error.sourceOutsideBook')
    case 'case_revision_conflict': return t('planning.casebook.error.revisionConflict')
    default: return t('planning.casebook.actionFailed')
  }
}

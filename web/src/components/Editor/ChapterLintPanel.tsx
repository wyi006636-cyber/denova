import { useCallback, useEffect, useMemo, useState } from 'react'
import type { Editor } from '@tiptap/react'
import { ChevronDown, ChevronRight, ExternalLink, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { getProjectLoreItems } from '@/lib/api-client/project-book'
import { readOptionalProjectFile } from '@/lib/api-client/project-files'
import {
  AI_TRACE_LEXICON_SOURCE,
  type ChapterLintCategory,
  type ChapterLintIssue,
  type ChapterLintReport,
  runChapterLintWithBannedWords,
} from '@/features/writing/chapter-lint'

const BANNED_WORDS_PATH = 'setting/banned-words.md'
const CATEGORY_KEYS: Record<ChapterLintCategory, string> = {
  banned: 'editor.lint.category.banned',
  ai: 'editor.lint.category.ai',
  repeat: 'editor.lint.category.repeat',
  punctuation: 'editor.lint.category.punctuation',
  length: 'editor.lint.category.length',
  reference: 'editor.lint.category.reference',
}

const DEFAULT_ENABLED: Record<ChapterLintCategory, boolean> = {
  banned: true,
  ai: true,
  repeat: true,
  punctuation: true,
  length: true,
  reference: true,
}

interface ChapterLintPanelProps {
  projectId: string
  content: string
  refreshToken?: number
  editor: Editor | null
  onClose: () => void
  onEditBannedWords?: () => void
  onIssueClick: (issue: ChapterLintIssue, occurrenceIndex?: number) => void
  onReportChange?: (report: ChapterLintReport) => void
}

export function ChapterLintPanel({
  projectId,
  content,
  refreshToken = 0,
  editor,
  onClose,
  onEditBannedWords,
  onIssueClick,
  onReportChange,
}: ChapterLintPanelProps) {
  const { t } = useTranslation()
  const [bannedWords, setBannedWords] = useState('')
  const [loreNames, setLoreNames] = useState<string[]>([])
  const [lintContent, setLintContent] = useState(content)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [enabled, setEnabled] = useState<Record<ChapterLintCategory, boolean>>(() => readPreferences())
  const [collapsed, setCollapsed] = useState<Record<ChapterLintCategory, boolean>>({
    banned: false, ai: false, repeat: false, punctuation: false, length: false, reference: false,
  })
  const [occurrenceIndexes, setOccurrenceIndexes] = useState<Record<string, number>>({})

  // Chapter checks are local but still scan the whole draft. Debouncing keeps
  // typing responsive while preserving fresh results after the user pauses.
  useEffect(() => {
    const timer = window.setTimeout(() => setLintContent(content), 400)
    return () => window.clearTimeout(timer)
  }, [content])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setLoadError('')
    void Promise.all([
      readOptionalProjectFile(projectId, BANNED_WORDS_PATH),
      getProjectLoreItems(projectId),
    ]).then(([bannedFile, items]) => {
      if (cancelled) return
      setBannedWords(bannedFile?.content || '')
      setLoreNames(items.flatMap(item => [item.name, ...(item.keywords || [])]).filter(Boolean))
    }).catch(() => {
      if (cancelled) return
      setLoadError('load-failed')
    }).finally(() => {
      if (!cancelled) setLoading(false)
    })
    return () => { cancelled = true }
  }, [projectId, refreshToken])

  useEffect(() => {
    try { window.localStorage.setItem('nova.chapter-lint.categories', JSON.stringify(enabled)) } catch { /* host storage may be unavailable */ }
  }, [enabled])

  const report = useMemo<ChapterLintReport>(() => runChapterLintWithBannedWords(lintContent, bannedWords, {
    loreTerms: loreNames,
    enabled,
  }), [bannedWords, enabled, lintContent, loreNames])
  useEffect(() => { onReportChange?.(report) }, [onReportChange, report])
  const grouped = useMemo(() => {
    const groups = new Map<ChapterLintCategory, ChapterLintIssue[]>()
    for (const issue of report.issues) {
      const group = groups.get(issue.category) || []
      group.push(issue)
      groups.set(issue.category, group)
    }
    return groups
  }, [report.issues])

  const toggleCategory = useCallback((category: ChapterLintCategory) => {
    setEnabled(current => ({ ...current, [category]: !current[category] }))
  }, [])

  const handleIssueClick = useCallback((issue: ChapterLintIssue) => {
    const occurrenceCount = issue.occurrences?.length || 1
    const nextIndex = ((occurrenceIndexes[issue.id] ?? -1) + 1) % occurrenceCount
    setOccurrenceIndexes(current => ({ ...current, [issue.id]: nextIndex }))
    onIssueClick(issue, nextIndex)
  }, [occurrenceIndexes, onIssueClick])

  return (
    <aside className="flex max-h-[min(70vh,680px)] w-full shrink-0 flex-col border-b border-[var(--nova-border)] bg-[var(--nova-surface-2)] text-[var(--nova-text)] md:max-h-none md:w-[clamp(220px,40%,360px)] md:border-b-0 md:border-r" aria-label={t('editor.lint.title')}>
      <div className="flex shrink-0 items-center gap-2 border-b border-[var(--nova-border)] px-3 py-2">
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-sm font-semibold">{t('editor.lint.title')}</h2>
          <p className="text-[11px] text-[var(--nova-text-faint)]">
            {t('editor.lint.summary', { count: report.issues.length })}
            {report.resultsTruncated ? ` · ${t('editor.lint.truncated', { total: report.issuesTotal })}` : null}
          </p>
        </div>
        <Button type="button" variant="ghost" size="icon-xs" aria-label={t('editor.lint.close')} onClick={onClose}><X /></Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        {loading ? <p className="py-6 text-center text-xs text-[var(--nova-text-faint)]">{t('editor.lint.loading')}</p> : null}
        {loadError ? <p className="mb-2 rounded border border-[var(--nova-danger-border)] bg-[var(--nova-danger-bg)] p-2 text-xs text-[var(--nova-danger)]">{t('editor.lint.loadFailed')}</p> : null}
        <div className="mb-3 flex flex-wrap items-center gap-2 text-[11px] text-[var(--nova-text-faint)]">
          <span>{t('editor.lint.localOnly')}</span>
          <Button type="button" variant="outline" size="xs" className="ml-auto" onClick={onEditBannedWords} disabled={!onEditBannedWords}>
            <ExternalLink data-icon="inline-start" />{t('editor.lint.editBannedWords')}
          </Button>
        </div>
        {report.invalidBannedWordLines.length > 0 ? (
          <p className="mb-3 rounded border border-[var(--nova-warning)]/40 bg-[var(--nova-warning-bg)] p-2 text-xs text-[var(--nova-warning)]">
            {t('editor.lint.invalidBannedWords', { lines: report.invalidBannedWordLines.join(', ') })}
          </p>
        ) : null}
        <div className="grid gap-2">
          {(Object.keys(CATEGORY_KEYS) as ChapterLintCategory[]).map(category => {
            const issues = grouped.get(category) || []
            const isCollapsed = collapsed[category]
            return (
              <section key={category} className="overflow-hidden rounded-md border border-[var(--nova-border)]">
                <div className="flex items-center gap-2 px-2 py-1.5">
                  <button type="button" className="flex min-w-0 flex-1 items-center gap-1 text-left text-xs font-medium" onClick={() => setCollapsed(current => ({ ...current, [category]: !isCollapsed }))}>
                    {isCollapsed ? <ChevronRight className="size-3.5" /> : <ChevronDown className="size-3.5" />}
                    <span className="truncate">{t(CATEGORY_KEYS[category])}</span>
                    <span className="rounded-full bg-[var(--nova-active)] px-1.5 text-[10px]">{report.counts[category]}</span>
                  </button>
                  <label className="flex items-center gap-1 text-[10px] text-[var(--nova-text-faint)]">
                    <input type="checkbox" checked={enabled[category]} onChange={() => toggleCategory(category)} />{t('editor.lint.enabled')}
                  </label>
                </div>
                {!isCollapsed && enabled[category] ? (
                  <div className="border-t border-[var(--nova-border)] px-2 py-1">
                    {issues.length ? issues.map(issue => <LintIssueRow key={issue.id} issue={issue} onClick={() => handleIssueClick(issue)} />) : <p className="py-2 text-[11px] text-[var(--nova-text-faint)]">{t('editor.lint.none')}</p>}
                  </div>
                ) : null}
              </section>
            )
          })}
        </div>
        <p className="mt-3 text-[10px] leading-relaxed text-[var(--nova-text-faint)]">{t('editor.lint.source', { source: AI_TRACE_LEXICON_SOURCE })}</p>
        {editor ? <span className="sr-only">{t('editor.lint.editorReady')}</span> : null}
      </div>
    </aside>
  )
}

function LintIssueRow({ issue, onClick }: { issue: ChapterLintIssue; onClick: () => void }) {
  const { t } = useTranslation()
  const message = t(issue.message.key, issue.message.params)
  return (
    <button type="button" className="grid w-full gap-0.5 rounded px-1.5 py-1.5 text-left text-xs hover:bg-[var(--nova-hover)]" onClick={onClick}>
      <span className="truncate font-medium">{issue.text || message}</span>
      <span className="line-clamp-2 text-[11px] text-[var(--nova-text-faint)]">{message}{issue.suggestion ? ` → ${issue.suggestion}` : ''}</span>
    </button>
  )
}

function readPreferences(): Record<ChapterLintCategory, boolean> {
  try {
    const parsed = JSON.parse(window.localStorage.getItem('nova.chapter-lint.categories') || '{}') as Partial<Record<ChapterLintCategory, boolean>>
    return { ...DEFAULT_ENABLED, ...parsed }
  } catch {
    return { ...DEFAULT_ENABLED }
  }
}


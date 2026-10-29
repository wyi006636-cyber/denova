import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Editor } from '@tiptap/react'
import { ChevronDown, ChevronRight, Copy, ExternalLink, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { getProjectBookSummary, getProjectLoreItems } from '@/lib/api-client/project-book'
import type { ChapterSummary, LoreItem } from '@/lib/api-client/types'
import { readOptionalProjectFile, readProjectFile } from '@/lib/api-client/project-files'
import {
  AI_TRACE_LEXICON_SOURCE,
  type ChapterLintCategory,
  type ChapterLintIssue,
  type ChapterLintReport,
  runChapterLintWithBannedWords,
} from '@/features/writing/chapter-lint'
import {
  computeStyleFingerprint,
  styleFingerprintToMarkdown,
  STYLE_FINGERPRINT_LIMITS,
  type StyleFingerprintResult,
} from '@/features/writing/style-fingerprint'
import { runVoiceLint } from '@/features/writing/voice-lint'

const BANNED_WORDS_PATH = 'setting/banned-words.md'
const CATEGORY_KEYS: Record<ChapterLintCategory, string> = {
  banned: 'editor.lint.category.banned',
  ai: 'editor.lint.category.ai',
  repeat: 'editor.lint.category.repeat',
  punctuation: 'editor.lint.category.punctuation',
  length: 'editor.lint.category.length',
  reference: 'editor.lint.category.reference',
  voice: 'editor.lint.category.voice',
}

const DEFAULT_ENABLED: Record<ChapterLintCategory, boolean> = {
  banned: true,
  ai: true,
  repeat: true,
  punctuation: true,
  length: true,
  reference: true,
  voice: true,
}

type LintTab = 'issues' | 'stats'

const STATS_ERROR_KEYS = {
  'stats-failed': 'editor.lint.statsFailed',
  'copy-failed': 'editor.lint.copyFailed',
} as const

interface ChapterLintPanelProps {
  projectId: string
  content: string
  currentChapterPath?: string | null
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
  currentChapterPath = null,
  refreshToken = 0,
  editor,
  onClose,
  onEditBannedWords,
  onIssueClick,
  onReportChange,
}: ChapterLintPanelProps) {
  const { t, i18n } = useTranslation()
  const [bannedWords, setBannedWords] = useState('')
  const [loreItems, setLoreItems] = useState<LoreItem[]>([])
  const [chapters, setChapters] = useState<ChapterSummary[]>([])
  const [lintContent, setLintContent] = useState(content)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [enabled, setEnabled] = useState<Record<ChapterLintCategory, boolean>>(() => readPreferences())
  const [collapsed, setCollapsed] = useState<Record<ChapterLintCategory, boolean>>({
    banned: false, ai: false, repeat: false, punctuation: false, length: false, reference: false, voice: false,
  })
  const [occurrenceIndexes, setOccurrenceIndexes] = useState<Record<string, number>>({})
  const [tab, setTab] = useState<LintTab>('issues')
  const [selectedChapterPaths, setSelectedChapterPaths] = useState<string[]>([])
  const [statsOverride, setStatsOverride] = useState<StyleFingerprintResult | null>(null)
  const [statsLoading, setStatsLoading] = useState(false)
  const [statsError, setStatsError] = useState('')
  const [copied, setCopied] = useState(false)
  const statsRequestGeneration = useRef(0)
  const selectionTouched = useRef(false)

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
      getProjectBookSummary(projectId),
    ]).then(([bannedFile, items, summary]) => {
      if (cancelled) return
      const nextChapters = [...(summary.chapters || [])].sort((left, right) => left.index - right.index)
      setBannedWords(bannedFile?.content || '')
      setLoreItems(items)
      setChapters(nextChapters)
      setSelectedChapterPaths(current => {
        const available = new Set(nextChapters.map(chapter => chapter.path))
        const retained = current.filter(path => available.has(path))
        if (!selectionTouched.current && !retained.length) {
          return currentChapterPath && available.has(currentChapterPath)
            ? [currentChapterPath]
            : nextChapters.slice(0, 1).map(chapter => chapter.path)
        }
        return [...new Set(retained)]
      })
    }).catch(() => {
      if (!cancelled) setLoadError('load-failed')
    }).finally(() => {
      if (!cancelled) setLoading(false)
    })
    return () => { cancelled = true }
  }, [currentChapterPath, projectId, refreshToken])

  useEffect(() => {
    setStatsOverride(null)
    setStatsError('')
    setStatsLoading(false)
    statsRequestGeneration.current += 1
  }, [content, currentChapterPath, projectId, refreshToken])

  useEffect(() => {
    try { window.localStorage.setItem('nova.chapter-lint.categories', JSON.stringify(enabled)) } catch { /* host storage may be unavailable */ }
  }, [enabled])

  const loreNames = useMemo(
    () => loreItems.flatMap(item => [item.name, ...(item.keywords || [])]).filter(Boolean),
    [loreItems],
  )
  const voiceCharacters = useMemo(() => loreItems
    // `character` is the stable category ID; display names are localized and
    // must never be used to decide whether an entry is a person.
    .filter(item => item.type === 'character')
    .map(item => ({ name: item.name, aliases: item.keywords || [], content: item.content })), [loreItems])

  const report = useMemo<ChapterLintReport>(() => {
    const base = runChapterLintWithBannedWords(lintContent, bannedWords, {
      loreTerms: loreNames,
      enabled,
    })
    const voiceIssues = enabled.voice ? runVoiceLint(lintContent, voiceCharacters).issues : []
    const issues = [...base.issues, ...voiceIssues].sort((left, right) => left.start - right.start || left.category.localeCompare(right.category))
    return {
      ...base,
      issues,
      counts: { ...base.counts, voice: voiceIssues.length },
      issuesTotal: base.issuesTotal + voiceIssues.length,
    }
  }, [bannedWords, enabled, lintContent, loreNames, voiceCharacters])

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

  const currentStats = useMemo(() => computeStyleFingerprint([
    { path: currentChapterPath || undefined, content: lintContent },
  ], { includeRepeatedPhrases: false }), [currentChapterPath, lintContent])
  const stats = statsOverride || currentStats

  const toggleCategory = useCallback((category: ChapterLintCategory) => {
    setEnabled(current => ({ ...current, [category]: !current[category] }))
  }, [])

  const handleIssueClick = useCallback((issue: ChapterLintIssue) => {
    const occurrenceCount = issue.occurrences?.length || 1
    const nextIndex = ((occurrenceIndexes[issue.id] ?? -1) + 1) % occurrenceCount
    setOccurrenceIndexes(current => ({ ...current, [issue.id]: nextIndex }))
    onIssueClick(issue, nextIndex)
  }, [occurrenceIndexes, onIssueClick])

  const toggleChapter = useCallback((path: string) => {
    selectionTouched.current = true
    statsRequestGeneration.current += 1
    setStatsOverride(null)
    setStatsError('')
    // A selection change invalidates the in-flight request immediately. Drop
    // its loading indicator so the author can start a fresh calculation.
    setStatsLoading(false)
    setSelectedChapterPaths(current => current.includes(path)
      ? current.filter(item => item !== path)
      : [...new Set([...current, path])])
  }, [])

  const calculateMultiChapterStats = useCallback(async () => {
    const paths = selectedChapterPaths
    if (!paths.length) return
    const generation = ++statsRequestGeneration.current
    setStatsLoading(true)
    setStatsError('')
    let skipped = 0
    const loaded: Array<{ path: string; label: string; content: string }> = []
    const orderedPaths = [
      ...chapters.filter(chapter => paths.includes(chapter.path)).map(chapter => chapter.path),
      ...paths.filter(path => !chapters.some(chapter => chapter.path === path)),
    ]
    try {
      for (const path of orderedPaths) {
        try {
          if (path === currentChapterPath) {
            const chapter = chapters.find(item => item.path === path)
            loaded.push({ path, label: chapter?.display_title || chapter?.file_name || path, content })
          } else {
            const file = await readProjectFile(projectId, path)
            const chapter = chapters.find(item => item.path === path)
            loaded.push({ path, label: chapter?.display_title || chapter?.file_name || path, content: file.content || '' })
          }
        } catch {
          skipped++
        }
      }
      if (generation !== statsRequestGeneration.current) return
      const result = computeStyleFingerprint(loaded, {
        skippedChapterCount: skipped,
      })
      setStatsOverride(result)
    } catch {
      if (generation === statsRequestGeneration.current) setStatsError('stats-failed')
    } finally {
      if (generation === statsRequestGeneration.current) setStatsLoading(false)
    }
  }, [chapters, content, currentChapterPath, projectId, selectedChapterPaths])

  const copyStats = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(styleFingerprintToMarkdown(stats, i18n.language.toLocaleLowerCase().startsWith('en') ? 'en' : 'zh'))
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1600)
    } catch {
      setStatsError('copy-failed')
    }
  }, [i18n.language, stats])

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
        {!loading ? <div role="tablist" aria-label={t('editor.lint.tabs')} className="mb-3 grid grid-cols-2 rounded-md border border-[var(--nova-border)] p-0.5">
          <button type="button" role="tab" aria-selected={tab === 'issues'} className={`rounded px-2 py-1.5 text-xs ${tab === 'issues' ? 'bg-[var(--nova-active)] font-medium' : 'text-[var(--nova-text-faint)]'}`} onClick={() => setTab('issues')}>{t('editor.lint.issuesTab')}</button>
          <button type="button" role="tab" aria-selected={tab === 'stats'} className={`rounded px-2 py-1.5 text-xs ${tab === 'stats' ? 'bg-[var(--nova-active)] font-medium' : 'text-[var(--nova-text-faint)]'}`} onClick={() => setTab('stats')}>{t('editor.lint.statsTab')}</button>
        </div> : null}
        {!loading && tab === 'stats' ? (
          <StyleFingerprintView
            chapters={chapters}
            currentChapterPath={currentChapterPath}
            selectedChapterPaths={selectedChapterPaths}
            stats={stats}
            multiChapterCalculated={Boolean(statsOverride)}
            loading={statsLoading}
            error={statsError}
            copied={copied}
            onToggleChapter={toggleChapter}
            onCalculate={calculateMultiChapterStats}
            onCopy={copyStats}
          />
        ) : !loading ? (
          <IssueList
            t={t}
            report={report}
            grouped={grouped}
            enabled={enabled}
            collapsed={collapsed}
            onToggleCategory={toggleCategory}
            onToggleCollapsed={category => setCollapsed(current => ({ ...current, [category]: !current[category] }))}
            onIssueClick={handleIssueClick}
            onEditBannedWords={onEditBannedWords}
          />
        ) : null}
        <p className="mt-3 text-[10px] leading-relaxed text-[var(--nova-text-faint)]">{t('editor.lint.source', { source: AI_TRACE_LEXICON_SOURCE })}</p>
        {editor ? <span className="sr-only">{t('editor.lint.editorReady')}</span> : null}
      </div>
    </aside>
  )
}

function IssueList({
  t,
  report,
  grouped,
  enabled,
  collapsed,
  onToggleCategory,
  onToggleCollapsed,
  onIssueClick,
  onEditBannedWords,
}: {
  t: (key: string, options?: Record<string, unknown>) => string
  report: ChapterLintReport
  grouped: Map<ChapterLintCategory, ChapterLintIssue[]>
  enabled: Record<ChapterLintCategory, boolean>
  collapsed: Record<ChapterLintCategory, boolean>
  onToggleCategory: (category: ChapterLintCategory) => void
  onToggleCollapsed: (category: ChapterLintCategory) => void
  onIssueClick: (issue: ChapterLintIssue) => void
  onEditBannedWords?: () => void
}) {
  return (
    <>
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
                <button type="button" className="flex min-w-0 flex-1 items-center gap-1 text-left text-xs font-medium" onClick={() => onToggleCollapsed(category)}>
                  {isCollapsed ? <ChevronRight className="size-3.5" /> : <ChevronDown className="size-3.5" />}
                  <span className="truncate">{t(CATEGORY_KEYS[category])}</span>
                  <span className="rounded-full bg-[var(--nova-active)] px-1.5 text-[10px]">{report.counts[category]}</span>
                </button>
                <label className="flex items-center gap-1 text-[10px] text-[var(--nova-text-faint)]">
                  <input type="checkbox" checked={enabled[category]} onChange={() => onToggleCategory(category)} />{t('editor.lint.enabled')}
                </label>
              </div>
              {!isCollapsed && enabled[category] ? (
                <div className="border-t border-[var(--nova-border)] px-2 py-1">
                  {issues.length ? issues.map(issue => <LintIssueRow key={issue.id} issue={issue} onClick={() => onIssueClick(issue)} />) : <p className="py-2 text-[11px] text-[var(--nova-text-faint)]">{t('editor.lint.none')}</p>}
                </div>
              ) : null}
            </section>
          )
        })}
      </div>
    </>
  )
}

function StyleFingerprintView({
  chapters,
  currentChapterPath,
  selectedChapterPaths,
  stats,
  multiChapterCalculated,
  loading,
  error,
  copied,
  onToggleChapter,
  onCalculate,
  onCopy,
}: {
  chapters: ChapterSummary[]
  currentChapterPath: string | null | undefined
  selectedChapterPaths: string[]
  stats: StyleFingerprintResult
  multiChapterCalculated: boolean
  loading: boolean
  error: string
  copied: boolean
  onToggleChapter: (path: string) => void
  onCalculate: () => void
  onCopy: () => void
}) {
  const { t, i18n } = useTranslation()
  const sentenceUnit = stats.sentenceLength.unit === 'words'
    ? t('editor.lint.statsUnitWords')
    : t('editor.lint.statsUnitCharacters')
  const errorKey = STATS_ERROR_KEYS[error as keyof typeof STATS_ERROR_KEYS]
  const markdownLocale = i18n.language.toLocaleLowerCase().startsWith('en') ? 'en' : 'zh'
  const droppedChapterSummary = stats.truncatedChapterLabels?.length
    ? stats.truncatedChapterLabels.length > 3
      ? t('editor.lint.statsDroppedMany', { chapters: stats.truncatedChapterLabels.slice(0, 3).join(markdownLocale === 'en' ? ', ' : '、'), count: stats.truncatedChapterLabels.length - 3 })
      : t('editor.lint.statsDropped', { chapters: stats.truncatedChapterLabels.join(markdownLocale === 'en' ? ', ' : '、') })
    : ''
  return (
    <div data-testid="style-fingerprint-panel" className="grid gap-3">
      <div className="rounded-md border border-[var(--nova-border)] p-2">
        <div className="mb-2 grid gap-1">
          <p className="text-xs font-medium">{t('editor.lint.statsScope')}</p>
          <span className="text-[10px] leading-relaxed text-[var(--nova-text-faint)]">{t('editor.lint.statsSelected', { count: selectedChapterPaths.length })}</span>
          {selectedChapterPaths.length > STYLE_FINGERPRINT_LIMITS.maxChapters ? <span className="text-[10px] leading-relaxed text-[var(--nova-text-faint)]">{t('editor.lint.statsWillCap')}</span> : null}
        </div>
        <div className="max-h-36 overflow-y-auto rounded border border-[var(--nova-border)] p-1">
          {chapters.length ? chapters.map(chapter => (
            <label key={chapter.path} className="flex min-w-0 items-center gap-2 rounded px-1.5 py-1 text-xs hover:bg-[var(--nova-hover)]">
              <input type="checkbox" checked={selectedChapterPaths.includes(chapter.path)} onChange={() => onToggleChapter(chapter.path)} />
              <span className="min-w-0 truncate">{chapter.display_title || chapter.file_name}</span>
              <span className="ml-auto shrink-0 text-[10px] text-[var(--nova-text-faint)]">{chapter.words}</span>
            </label>
          )) : <p className="px-1 py-2 text-[11px] text-[var(--nova-text-faint)]">{t('editor.lint.statsNoChapters')}</p>}
        </div>
        <div className="mt-2 flex flex-wrap gap-2">
          <Button type="button" variant="outline" size="xs" onClick={onCalculate} disabled={!selectedChapterPaths.length || loading}>
            {loading ? t('editor.lint.statsCalculating') : t('editor.lint.statsCalculate')}
          </Button>
          <Button type="button" variant="outline" size="xs" onClick={onCopy}>
            <Copy data-icon="inline-start" />{copied ? t('editor.lint.statsCopied') : t('editor.lint.statsCopy')}
          </Button>
        </div>
        {error && errorKey ? <p className="mt-2 text-[11px] text-[var(--nova-danger)]">{t(errorKey)}</p> : null}
      </div>
      {stats.truncated ? <p className="rounded border border-[var(--nova-warning)]/40 bg-[var(--nova-warning-bg)] p-2 text-xs text-[var(--nova-warning)]">
        {stats.truncatedByChapters ? t('editor.lint.statsChapterTruncated', { count: stats.chapterCount, chapters: droppedChapterSummary }) : null}
        {stats.truncatedByChapters && stats.truncatedByCharacters ? ' ' : null}
        {stats.truncatedByCharacters ? t('editor.lint.statsCharacterTruncated', { count: stats.characterCount }) : null}
      </p> : null}
      {stats.skippedChapterCount ? <p className="rounded border border-[var(--nova-warning)]/40 bg-[var(--nova-warning-bg)] p-2 text-xs text-[var(--nova-warning)]">{t('editor.lint.statsSkipped', { count: stats.skippedChapterCount })}</p> : null}
      <div className="grid grid-cols-2 gap-2 text-xs">
        <StatCard label={t('editor.lint.statsChapters')} value={String(stats.chapterCount)} />
        <StatCard label={t('editor.lint.statsCharacters')} value={String(stats.characterCount)} />
        <StatCard label={t('editor.lint.statsSentence')} value={`${stats.sentenceLength.p50} / ${stats.sentenceLength.p90} ${sentenceUnit}`} hint={t('editor.lint.statsP50P90')} />
        <StatCard label={t('editor.lint.statsParagraph')} value={`${stats.paragraphLength.p50} / ${stats.paragraphLength.p90} ${t('editor.lint.statsUnitCharacters')}`} hint={t('editor.lint.statsP50P90')} />
        <StatCard label={t('editor.lint.statsDialogueRatio')} value={`${Math.round(stats.dialogueParagraphRatio * 100)}%`} />
        <StatCard label={t('editor.lint.statsDialogueRuns')} value={`${stats.dialogueRunAverage} / ${stats.dialogueRunLongest}`} hint={t('editor.lint.statsAverageLongest')} />
      </div>
      <div className="rounded-md border border-[var(--nova-border)] p-2 text-xs">
        <p className="mb-1 font-medium">{t('editor.lint.statsPunctuation')}</p>
        <p className="text-[var(--nova-text-faint)]">{t('editor.lint.statsPunctuationValue', { ...stats.punctuationPerThousand })}</p>
      </div>
      <div className="rounded-md border border-[var(--nova-border)] p-2 text-xs">
        <p className="mb-1 font-medium">{t('editor.lint.statsRepeated')}</p>
        {stats.repeatedPhrases.length ? <ul className="grid gap-1 text-[var(--nova-text-faint)]">{stats.repeatedPhrases.map(item => <li key={item.phrase}>「{item.phrase}」 × {item.count}</li>)}</ul> : <p className="text-[var(--nova-text-faint)]">{t(multiChapterCalculated ? 'editor.lint.statsRepeatedNone' : 'editor.lint.statsRepeatedPending')}</p>}
      </div>
      <p className="text-[10px] text-[var(--nova-text-faint)]">{t('editor.lint.statsElapsed', { ms: Math.round(stats.elapsedMs) })}</p>
      {currentChapterPath && !selectedChapterPaths.length ? <p className="text-[10px] text-[var(--nova-text-faint)]">{currentChapterPath}</p> : null}
    </div>
  )
}

function StatCard({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return <div className="rounded-md border border-[var(--nova-border)] p-2"><p className="text-[10px] text-[var(--nova-text-faint)]">{label}</p><p className="mt-1 font-medium">{value}</p>{hint ? <p className="text-[10px] text-[var(--nova-text-faint)]">{hint}</p> : null}</div>
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

import { useMemo, useState } from 'react'
import { AlertTriangle, BookOpen, ChevronRight, FileText, Paperclip } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { useToolNavigation } from './tool-navigation'
import type { ContextReceipt, ContextReceiptEntry, ContextReceiptSource } from './context-receipt'
import { cn } from '@/lib/utils'

export function ContextReceiptRow({ receipt }: { receipt: ContextReceipt }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const summary = useMemo(() => receiptSummary(receipt, t), [receipt, t])
  const hasSources = receipt.sources.length > 0

  return (
    <div className="flex min-w-0 justify-start" data-context-receipt>
      <Collapsible open={expanded} onOpenChange={setExpanded} className="min-w-0 w-full px-1">
        <CollapsibleTrigger
          type="button"
          aria-label={expanded ? t('chat.contextReceipt.collapse') : t('chat.contextReceipt.expand')}
          className="group flex min-w-0 max-w-full flex-wrap items-center gap-1 py-1 text-left text-xs text-[var(--nova-text-muted)] transition-colors hover:text-[var(--nova-text)]"
        >
          <BookOpen aria-hidden="true" className="size-3 shrink-0 opacity-70" />
          <span className="shrink-0">{t('chat.contextReceipt.label')}</span>
          <span className="min-w-0 break-words">{summary}</span>
          <ChevronRight aria-hidden="true" className={cn('size-3 shrink-0 opacity-60 transition-transform duration-[var(--nova-motion-fast)]', expanded && 'rotate-90')} />
        </CollapsibleTrigger>
        <CollapsibleContent className="min-w-0 max-w-full pb-2 pt-1 text-xs text-[var(--nova-text-muted)]">
          {!receipt.hasDetails ? <p className="px-1 text-[var(--nova-text-faint)]">{t('chat.contextReceipt.emptyDetail')}</p> : null}
          {receipt.author.length ? <ReceiptSection title={t('chat.contextReceipt.author')} entries={receipt.author} /> : null}
          {hasSources ? receipt.sources.map(source => <SourceSection key={source.key} source={source} />) : null}
          {receipt.authorOmittedCount > 0 ? <OmittedHint count={receipt.authorOmittedCount} /> : null}
        </CollapsibleContent>
      </Collapsible>
    </div>
  )
}

function SourceSection({ source }: { source: ContextReceiptSource }) {
  const { t } = useTranslation()
  const title = source.key === 'agent'
    ? t('chat.contextReceipt.agentSource')
    : t('chat.contextReceipt.subagentSource', { name: source.label || t('chat.contextReceipt.unnamedSubagent') })
  return (
    <div className="min-w-0 space-y-2 border-t border-[var(--nova-border)] px-1 pt-2 first:border-t-0 first:pt-0">
      <div className="font-medium text-[var(--nova-text)]">{title}</div>
      {source.chapters.length ? <ReceiptSection title={t('chat.contextReceipt.chapters')} entries={source.chapters} /> : null}
      {source.files.length ? <ReceiptSection title={t('chat.contextReceipt.files')} entries={source.files} /> : null}
      {source.lore.length ? <ReceiptSection title={t('chat.contextReceipt.lore')} entries={source.lore} /> : null}
      {source.browse.length ? <ReceiptSection title={t('chat.contextReceipt.browse')} entries={source.browse} browse /> : null}
      {source.failures.length ? <ReceiptSection title={t('chat.contextReceipt.failures')} entries={source.failures} failure /> : null}
      {source.unavailable ? <p className="break-words text-[var(--nova-text-faint)]">{t('chat.contextReceipt.unavailable')}</p> : null}
      {source.omittedCount > 0 ? <OmittedHint count={source.omittedCount} /> : null}
    </div>
  )
}

function ReceiptSection({ title, entries, browse = false, failure = false }: { title: string; entries: ContextReceiptEntry[]; browse?: boolean; failure?: boolean }) {
  return (
    <section className="min-w-0 space-y-1">
      <div className="text-[10px] uppercase tracking-wide text-[var(--nova-text-faint)]">{title}</div>
      <div className="min-w-0 space-y-0.5">
        {entries.map(entry => <ReceiptEntry key={entry.key} entry={entry} browse={browse} failure={failure} />)}
      </div>
    </section>
  )
}

function ReceiptEntry({ entry, browse, failure }: { entry: ContextReceiptEntry; browse?: boolean; failure?: boolean }) {
  const navigation = useToolNavigation()
  const { t } = useTranslation()
  const canOpenFile = Boolean(navigation && entry.path && !failure)
  const canOpenLore = Boolean(navigation && entry.loreID && !failure)
  const open = () => {
    if (canOpenFile) navigation?.open({ kind: 'workspace_file', path: entry.path as string })
    else if (canOpenLore) navigation?.open({ kind: 'lore_item', id: entry.loreID, name: entry.label })
  }
  const icon = failure ? <AlertTriangle aria-hidden="true" className="mt-0.5 size-3 shrink-0 text-[var(--nova-danger)]" />
    : entry.loreID ? <BookOpen aria-hidden="true" className="mt-0.5 size-3 shrink-0 opacity-60" />
      : entry.path ? <FileText aria-hidden="true" className="mt-0.5 size-3 shrink-0 opacity-60" />
        : <Paperclip aria-hidden="true" className="mt-0.5 size-3 shrink-0 opacity-60" />
  return (
    <div className={cn('flex min-w-0 items-start gap-1.5 rounded px-1 py-0.5', failure && 'text-[var(--nova-danger)]')}>
      {icon}
      {canOpenFile || canOpenLore ? (
        <button
          type="button"
          onClick={open}
          className="min-w-0 max-w-full break-words text-left hover:text-[var(--nova-text)] hover:underline focus-visible:outline-none focus-visible:underline"
          title={entry.label}
        >
          <span className="break-words">{entry.label}</span>
          {entry.detail ? <span className="ml-1 text-[10px] text-[var(--nova-text-faint)]">{entry.detail}</span> : null}
          {browse ? <span className="ml-1 text-[10px] text-[var(--nova-text-faint)]">({t('chat.contextReceipt.browseBadge')})</span> : null}
        </button>
      ) : (
        <span className="min-w-0 break-words">
          {entry.label}
          {entry.detail ? <span className="ml-1 text-[10px] text-[var(--nova-text-faint)]">{entry.detail}</span> : null}
          {browse ? <span className="ml-1 text-[10px] text-[var(--nova-text-faint)]">({t('chat.contextReceipt.browseBadge')})</span> : null}
        </span>
      )}
    </div>
  )
}

function OmittedHint({ count }: { count: number }) {
  const { t } = useTranslation()
  return <p className="px-1 text-[10px] text-[var(--nova-text-faint)]">{t('chat.contextReceipt.omitted', { count })}</p>
}

function receiptSummary(receipt: ContextReceipt, t: ReturnType<typeof useTranslation>['t']) {
  const chapters = receipt.sources.reduce((total, source) => total + source.chapters.length, 0)
  const files = receipt.sources.reduce((total, source) => total + source.files.length, 0)
  const lore = receipt.sources.reduce((total, source) => total + source.lore.length, 0)
  const browse = receipt.sources.reduce((total, source) => total + source.browse.length, 0)
  const author = receipt.author.length
  const sources = receipt.sources.filter(source => source.key !== 'agent').length
  const unavailable = receipt.sources.some(source => source.unavailable)
  if (!chapters && !files && !lore && !browse && !author && unavailable) return t('chat.contextReceipt.unavailable')
  if (!chapters && !files && !lore && !browse && !author) return t('chat.contextReceipt.emptySummary')
  const parts = [
    chapters ? t('chat.contextReceipt.chapterCount', { count: chapters }) : '',
    lore ? t('chat.contextReceipt.loreCount', { count: lore }) : '',
    files ? t('chat.contextReceipt.fileCount', { count: files }) : '',
    browse ? t('chat.contextReceipt.browseCount', { count: browse }) : '',
    author ? t('chat.contextReceipt.authorCount', { count: author }) : '',
    sources ? t('chat.contextReceipt.subagentCount', { count: sources }) : '',
    unavailable ? t('chat.contextReceipt.unavailable') : '',
  ].filter(Boolean)
  return parts.join(' · ')
}

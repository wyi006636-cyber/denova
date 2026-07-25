import { ArrowRight, FileSearch, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import type { QualityMigrationPreviewDTO } from '../types'
import { projectStatus, shortDigest } from './quality-presenters'
import { QualityStateNotice } from './QualityStateNotice'

export function QualityMigrationPreview({
  data,
  error,
  pending,
  disabled,
  onPreview,
}: {
  data?: QualityMigrationPreviewDTO
  error: unknown
  pending: boolean
  disabled: boolean
  onPreview: () => void
}) {
  const { t } = useTranslation()
  return (
    <section aria-labelledby="quality-preview-title" className="min-w-0 space-y-3 pb-8">
      <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div className="min-w-0">
          <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-[var(--nova-text-faint)]">{t('quality.section.migration')}</p>
          <h2 id="quality-preview-title" className="mt-1 text-lg font-semibold tracking-tight">{t('quality.preview.title')}</h2>
          <p className="mt-1 max-w-2xl break-words text-xs leading-5 text-[var(--nova-text-muted)]">{t('quality.preview.description')}</p>
        </div>
        <Button type="button" variant="outline" size="sm" disabled={disabled || pending} onClick={onPreview}>
          <FileSearch data-icon="inline-start" />{pending ? t('quality.preview.loading') : t('quality.preview.action')}
        </Button>
      </div>
      {error ? <QualityStateNotice error={error} area="preview" onRetry={onPreview} /> : null}
      {!error && !data ? <PreviewIdle disabled={disabled} /> : null}
      {!error && data ? <PreviewResult preview={data} /> : null}
    </section>
  )
}

function PreviewIdle({ disabled }: { disabled: boolean }) {
  const { t } = useTranslation()
  return (
    <div className="flex min-h-28 items-center justify-center rounded-xl border border-dashed border-[var(--nova-border)] bg-[var(--nova-surface)] p-5 text-center">
      <p className="max-w-lg break-words text-xs leading-5 text-[var(--nova-text-faint)]">{disabled ? t('quality.preview.noWorkspace') : t('quality.preview.idle')}</p>
    </div>
  )
}

function PreviewResult({ preview }: { preview: QualityMigrationPreviewDTO }) {
  const { t } = useTranslation()
  const status = projectStatus(preview.compatibility, t)
  return (
    <Card className="min-w-0 border border-[var(--nova-border)] bg-[var(--nova-surface)] shadow-none">
      <CardHeader>
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="inline-flex size-8 items-center justify-center rounded-full border border-[var(--nova-success)]/40 text-[var(--nova-success)]"><ShieldCheck className="size-4" /></span>
          <CardTitle>{t('quality.preview.readOnly')}</CardTitle>
          <Badge variant="outline">{status.label}</Badge>
        </div>
        <CardDescription className="break-words">{t('quality.preview.summary', { entries: preview.totals.entries, operations: preview.totals.operations, conflicts: preview.totals.conflicts })}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid min-w-0 gap-2 sm:grid-cols-3">
          <PreviewFact label={t('quality.preview.digest')} value={shortDigest(preview.digest)} />
          <PreviewFact label={t('quality.preview.workspaceKind')} value={t(`quality.workspaceKind.${preview.workspace_kind}`)} />
          <PreviewFact label={t('quality.preview.schemaChange')} value={`${preview.current_schema_version} → ${preview.target_schema_version}`} />
        </div>
        {preview.compatibility.issues.length > 0 ? (
          <div className="min-w-0 rounded-lg border border-[var(--nova-warning)]/40 bg-[var(--nova-surface-2)] p-3">
            <h3 className="text-xs font-medium">{t('quality.preview.issues')}</h3>
            <ul className="mt-2 space-y-1">
              {preview.compatibility.issues.map((issue, index) => <li key={`${issue.code}-${index}`} className="break-all font-mono text-[10px] text-[var(--nova-text-muted)]">{issue.code}</li>)}
            </ul>
          </div>
        ) : null}
        <div className="grid min-w-0 gap-3 lg:grid-cols-3">
          <PreviewList title={t('quality.preview.files')} items={preview.entries.items.map((entry) => `${entry.source} → ${entry.destination}`)} empty={t('quality.preview.noFiles')} />
          <PreviewList title={t('quality.preview.operations')} items={preview.operations.items.map((operation) => `${operation.kind}: ${operation.destination}`)} empty={t('quality.preview.noOperations')} />
          <PreviewList title={t('quality.preview.conflicts')} items={preview.conflicts.items.map((conflict) => conflict.code)} empty={t('quality.preview.noConflicts')} />
        </div>
      </CardContent>
    </Card>
  )
}

function PreviewFact({ label, value }: { label: string; value: string }) {
  return <div className="min-w-0 rounded-lg bg-[var(--nova-surface-2)] p-3"><p className="text-[10px] uppercase tracking-[0.08em] text-[var(--nova-text-faint)]">{label}</p><p className="mt-1 break-all text-xs font-medium">{value}</p></div>
}

function PreviewList({ title, items, empty }: { title: string; items: string[]; empty: string }) {
  return (
    <div className="min-w-0 rounded-lg border border-[var(--nova-border)] p-3">
      <h3 className="text-xs font-medium">{title}</h3>
      {items.length === 0 ? <p className="mt-2 text-xs text-[var(--nova-text-faint)]">{empty}</p> : (
        <ul className="mt-2 space-y-2">{items.map((item, index) => <li key={`${item}-${index}`} className="flex min-w-0 gap-1.5 break-all text-[10px] leading-4 text-[var(--nova-text-muted)]"><ArrowRight className="mt-0.5 size-3 shrink-0" /><span>{item}</span></li>)}</ul>
      )}
    </div>
  )
}

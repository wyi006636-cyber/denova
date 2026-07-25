import { CheckCircle2, CircleAlert, FolderTree, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import type { QualityProjectDTO } from '../types'
import { projectStatus } from './quality-presenters'
import { QualityStateNotice } from './QualityStateNotice'

export function QualityProjectOverview({
  project,
  error,
  loading,
  onRetry,
}: {
  project?: QualityProjectDTO
  error: unknown
  loading: boolean
  onRetry: () => void
}) {
  const { t } = useTranslation()
  return (
    <section aria-labelledby="quality-overview-title" className="min-w-0 space-y-3">
      <div className="flex min-w-0 items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-[var(--nova-text-faint)]">{t('quality.section.currentWork')}</p>
          <h2 id="quality-overview-title" className="mt-1 text-lg font-semibold tracking-tight">{t('quality.overview.title')}</h2>
        </div>
      </div>
      {loading ? <OverviewSkeleton /> : null}
      {!loading && error ? <QualityStateNotice error={error} area="project" onRetry={onRetry} /> : null}
      {!loading && project ? <OverviewContent project={project} /> : null}
    </section>
  )
}

function OverviewContent({ project }: { project: QualityProjectDTO }) {
  const { t } = useTranslation()
  const status = projectStatus(project, t)
  const StatusIcon = status.tone === 'success' ? CheckCircle2 : CircleAlert
  return (
    <div className="grid min-w-0 gap-3 md:grid-cols-[minmax(0,1.25fr)_minmax(0,0.75fr)]">
      <Card className="border border-[var(--nova-border)] bg-[var(--nova-surface)] shadow-none">
        <CardHeader>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className={`inline-flex size-8 items-center justify-center rounded-full border border-[var(--nova-border)] ${status.tone === 'success' ? 'text-[var(--nova-success)]' : 'text-[var(--nova-warning)]'}`}>
              <StatusIcon className="size-4" />
            </span>
            <CardTitle className="break-words">{status.label}</CardTitle>
          </div>
          <CardDescription className="break-words leading-5">{t('quality.overview.readOnlyDescription')}</CardDescription>
        </CardHeader>
        <CardContent className="grid min-w-0 gap-2 sm:grid-cols-2">
          <OverviewFact icon={<FolderTree className="size-3.5" />} label={t('quality.overview.workspaceRoot')} value={project.active_root || t('quality.common.notAvailable')} />
          <OverviewFact icon={<ShieldCheck className="size-3.5" />} label={t('quality.overview.schema')} value={project.marker.present ? t('quality.overview.schemaVersion', { version: project.marker.schema_version ?? 1 }) : t('quality.overview.schemaMissing')} />
        </CardContent>
      </Card>
      <Card className="border border-[var(--nova-border)] bg-[var(--nova-surface)] shadow-none">
        <CardHeader>
          <CardTitle>{t('quality.overview.compatibility')}</CardTitle>
          <CardDescription>{t('quality.overview.compatibilityDescription')}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          {project.issues.length === 0 ? (
            <p className="text-xs leading-5 text-[var(--nova-text-muted)]">{t('quality.overview.noIssues')}</p>
          ) : project.issues.map((issue, index) => (
            <div key={`${issue.code}-${index}`} className="min-w-0 rounded-lg border border-[var(--nova-border)] bg-[var(--nova-surface-2)] p-2.5">
              <p className="break-words text-xs font-medium">{t('quality.overview.issue')}</p>
              <code className="mt-1 block break-all text-[10px] text-[var(--nova-text-faint)]">{issue.code}</code>
            </div>
          ))}
          {project.unknown_optional_features.map((feature) => <Badge key={feature} variant="outline" className="max-w-full break-all whitespace-normal">{feature}</Badge>)}
        </CardContent>
      </Card>
    </div>
  )
}

function OverviewFact({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-lg border border-[var(--nova-border)] bg-[var(--nova-surface-2)] p-3">
      <div className="flex items-center gap-2 text-[11px] text-[var(--nova-text-faint)]">{icon}<span>{label}</span></div>
      <p className="mt-1 break-all text-xs font-medium">{value}</p>
    </div>
  )
}

function OverviewSkeleton() {
  return <div aria-hidden="true" className="h-36 animate-pulse rounded-xl border border-[var(--nova-border)] bg-[var(--nova-surface)]" />
}

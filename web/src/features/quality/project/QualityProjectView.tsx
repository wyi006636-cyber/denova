import { ArrowLeft, ClipboardCheck } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { QualityProfileID } from '../types'
import { QualityMigrationPreview } from './QualityMigrationPreview'
import { QualityPlan } from './QualityPlan'
import { QualityProfileCatalog } from './QualityProfileCatalog'
import { QualityProjectOverview } from './QualityProjectOverview'
import { useQualityMigrationPreview, useQualityProfileDetail, useQualityProjectOverview } from './use-quality-project'

export function QualityProjectView({ onClose }: { onClose?: () => void }) {
  const { t } = useTranslation()
  const [selectedProfileID, setSelectedProfileID] = useState<QualityProfileID | null>(null)
  const { profiles, project } = useQualityProjectOverview()
  const activeProfileID = selectedProfileID && profiles.data?.some((item) => item.profile_id === selectedProfileID)
    ? selectedProfileID
    : profiles.data?.[0]?.profile_id ?? null
  const detail = useQualityProfileDetail(activeProfileID)
  const preview = useQualityMigrationPreview()
  const initialLoading = profiles.isPending && project.isPending
  const noWorkspace = project.error && 'status' in Object(project.error) && (project.error as { status?: number }).status === 409

  return (
    <section data-testid="quality-project-view" className="h-full min-w-0 overflow-x-hidden overflow-y-auto bg-[var(--nova-bg)] text-[var(--nova-text)]">
      <div className="mx-auto flex min-h-full w-full max-w-screen-2xl min-w-0 flex-col gap-6 px-4 py-5 sm:px-6 lg:px-8 lg:py-7">
        <header className="flex min-w-0 flex-col gap-4 border-b border-[var(--nova-border)] pb-5 sm:flex-row sm:items-end sm:justify-between">
          <div className="min-w-0">
            <div className="mb-2 flex items-center gap-2 text-[11px] font-medium uppercase tracking-[0.18em] text-[var(--nova-text-faint)]">
              <ClipboardCheck className="size-3.5 text-[var(--nova-success)]" />
              {t('quality.eyebrow')}
            </div>
            <h1 className="break-words text-2xl font-semibold tracking-[-0.025em] sm:text-3xl">{t('quality.title')}</h1>
            <p className="mt-2 max-w-3xl break-words text-sm leading-6 text-[var(--nova-text-muted)]">{t('quality.subtitle')}</p>
          </div>
          {onClose ? <Button type="button" variant="outline" size="sm" onClick={onClose}><ArrowLeft data-icon="inline-start" />{t('quality.actions.return')}</Button> : null}
        </header>

        {initialLoading ? <QualityLoadingState /> : null}
        {!initialLoading ? (
          <>
            <QualityProjectOverview project={project.data} error={project.error} loading={project.isPending} onRetry={() => { void project.refetch() }} />
            <section aria-labelledby="quality-catalog-plan-title" className="min-w-0 space-y-3">
              <div>
                <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-[var(--nova-text-faint)]">{t('quality.section.reference')}</p>
                <h2 id="quality-catalog-plan-title" className="mt-1 text-lg font-semibold tracking-tight">{t('quality.catalogPlan.title')}</h2>
                <p className="mt-1 break-words text-xs leading-5 text-[var(--nova-text-muted)]">{t('quality.catalogPlan.description')}</p>
              </div>
              <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(220px,0.32fr)_minmax(0,1fr)]">
                <QualityProfileCatalog
                  profiles={profiles.data}
                  activeProfileID={activeProfileID}
                  loading={profiles.isPending}
                  error={profiles.error}
                  onSelect={setSelectedProfileID}
                  onRetry={() => { void profiles.refetch() }}
                />
                <QualityPlan detail={detail.data} loading={detail.isPending && activeProfileID !== null} error={detail.error} onRetry={() => { void detail.refetch() }} />
              </div>
            </section>
            <QualityMigrationPreview
              data={preview.data}
              error={preview.error}
              pending={preview.isPending}
              disabled={Boolean(noWorkspace)}
              onPreview={() => preview.mutate()}
            />
          </>
        ) : null}
      </div>
    </section>
  )
}

function QualityLoadingState() {
  const { t } = useTranslation()
  return (
    <div role="status" data-testid="quality-loading-layout" className="grid min-w-0 gap-4 lg:grid-cols-[minmax(220px,0.32fr)_minmax(0,1fr)]">
      <span className="sr-only">{t('quality.loading')}</span>
      <div className="h-64 animate-pulse rounded-xl border border-[var(--nova-border)] bg-[var(--nova-surface)]" />
      <div className="h-64 animate-pulse rounded-xl border border-[var(--nova-border)] bg-[var(--nova-surface)]" />
    </div>
  )
}

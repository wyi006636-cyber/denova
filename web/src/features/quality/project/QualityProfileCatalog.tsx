import { BookOpenCheck, LibraryBig } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { SectionedNavigation } from '@/components/navigation/sectioned-navigation'
import type { QualityProfileID, QualityProfileSummaryDTO } from '../types'
import { localizedText } from './quality-presenters'
import { QualityStateNotice } from './QualityStateNotice'

export function QualityProfileCatalog({
  profiles,
  activeProfileID,
  loading,
  error,
  onSelect,
  onRetry,
}: {
  profiles?: QualityProfileSummaryDTO[]
  activeProfileID: QualityProfileID | null
  loading: boolean
  error: unknown
  onSelect: (profileID: QualityProfileID) => void
  onRetry: () => void
}) {
  const { t, i18n } = useTranslation()
  const groups = [{
    id: 'catalog',
    title: t('quality.catalog.group'),
    items: (profiles ?? []).map((profile) => ({
      id: profile.profile_id,
      title: localizedText(profile.summary, i18n.language),
      description: t(`quality.profile.${profile.profile_id}.description`),
      icon: BookOpenCheck,
    })),
  }]
  return (
    <aside className="min-w-0 space-y-3 rounded-xl border border-[var(--nova-border)] bg-[var(--nova-surface)] p-3 sm:p-4">
      <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="text-sm font-semibold">{t('quality.catalog.title')}</h3>
          <p className="mt-1 break-words text-xs leading-5 text-[var(--nova-text-muted)]">{t('quality.catalog.description')}</p>
        </div>
        <Badge variant="outline" className="border-[var(--nova-warning)]/40 text-[var(--nova-warning)]">{t('quality.catalog.readOnly')}</Badge>
      </div>
      {loading ? <div className="h-32 animate-pulse rounded-lg bg-[var(--nova-surface-2)]" /> : null}
      {!loading && error ? <QualityStateNotice error={error} area="catalog" onRetry={onRetry} /> : null}
      {!loading && !error && profiles?.length === 0 ? (
        <Empty className="min-h-44 border border-dashed border-[var(--nova-border)]">
          <EmptyHeader>
            <EmptyMedia variant="icon"><LibraryBig /></EmptyMedia>
            <EmptyTitle>{t('quality.catalog.emptyTitle')}</EmptyTitle>
            <EmptyDescription>{t('quality.catalog.emptyDescription')}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : null}
      {!loading && !error && profiles && profiles.length > 0 && activeProfileID ? (
        <SectionedNavigation groups={groups} activeId={activeProfileID} onSelect={onSelect} itemClassName="min-w-0" />
      ) : null}
    </aside>
  )
}

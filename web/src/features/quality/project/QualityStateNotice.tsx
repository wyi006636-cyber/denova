import { AlertTriangle, FileQuestion, PlugZap, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { APIError } from '@/lib/api-client/client'
import { QualityContractError } from '../contract-guards'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

type QualityErrorArea = 'catalog' | 'profile' | 'project' | 'preview'

export function QualityStateNotice({
  error,
  area,
  onRetry,
}: {
  error: unknown
  area: QualityErrorArea
  onRetry?: () => void
}) {
  const { t } = useTranslation()
  const state = errorState(error, area, t)
  const Icon = state.icon
  return (
    <Card role="alert" className="border border-[var(--nova-border)] bg-[var(--nova-surface)] shadow-none">
      <CardHeader className="flex-row items-start gap-3">
        <span className="mt-0.5 inline-flex size-8 shrink-0 items-center justify-center rounded-full border border-[var(--nova-border)] bg-[var(--nova-surface-2)]">
          <Icon className="size-4 text-[var(--nova-warning)]" />
        </span>
        <div className="min-w-0 space-y-1">
          <CardTitle className="break-words text-sm">{state.title}</CardTitle>
          <p className="break-words text-xs leading-5 text-[var(--nova-text-muted)]">{state.description}</p>
        </div>
      </CardHeader>
      {onRetry ? (
        <CardContent className="pl-15">
          <Button type="button" variant="outline" size="sm" onClick={onRetry}>{t('quality.actions.retry')}</Button>
        </CardContent>
      ) : null}
    </Card>
  )
}

function errorState(error: unknown, area: QualityErrorArea, t: ReturnType<typeof useTranslation>['t']) {
  if (error instanceof QualityContractError) {
    return error.kind === 'unsupported'
      ? { title: t('quality.error.unsupported.title'), description: t('quality.error.unsupported.description'), icon: ShieldAlert }
      : { title: t('quality.error.malformed.title'), description: t('quality.error.malformed.description'), icon: FileQuestion }
  }
  if (error instanceof APIError) {
    if (area === 'project' && (error.status === 409 || error.code === 'quality_no_workspace')) {
      return { title: t('quality.error.noWorkspace.title'), description: t('quality.error.noWorkspace.description'), icon: FileQuestion }
    }
    if (area === 'profile' && error.status === 404) {
      return { title: t('quality.error.profileNotFound.title'), description: t('quality.error.profileNotFound.description'), icon: FileQuestion }
    }
    if (area === 'preview') {
      return { title: t('quality.error.preview.title'), description: t('quality.error.preview.description'), icon: AlertTriangle }
    }
    if (area === 'catalog') {
      return { title: t('quality.error.catalog.title'), description: t('quality.error.catalog.description'), icon: AlertTriangle }
    }
  }
  return { title: t('quality.error.network.title'), description: t('quality.error.network.description'), icon: PlugZap }
}

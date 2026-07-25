import { ChevronDown, FileCheck2, Flag, ListChecks, Target } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import type { QualityGoal, QualityProfileDetailDTO, QualityProfileSetting } from '../types'
import { formatSettingValue, humanizeToken, localizedText } from './quality-presenters'
import { QualityStateNotice } from './QualityStateNotice'

export function QualityPlan({
  detail,
  loading,
  error,
  onRetry,
}: {
  detail?: QualityProfileDetailDTO
  loading: boolean
  error: unknown
  onRetry: () => void
}) {
  const { t, i18n } = useTranslation()
  if (loading) return <div className="h-80 animate-pulse rounded-xl border border-[var(--nova-border)] bg-[var(--nova-surface)]" />
  if (error) return <QualityStateNotice error={error} area="profile" onRetry={onRetry} />
  if (!detail) return null
  const spec = detail.profile.quality_spec
  return (
    <article className="min-w-0 space-y-4">
      <Card className="border border-[var(--nova-border)] bg-[var(--nova-surface)] shadow-none">
        <CardHeader>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <CardTitle className="break-words text-lg">{localizedText(detail.profile.display_name, i18n.language)}</CardTitle>
            <Badge variant="outline">{t('quality.catalog.readOnly')}</Badge>
          </div>
          <CardDescription className="break-words leading-5">{localizedText(detail.profile.walkthrough.description, i18n.language)}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex items-start gap-2 rounded-lg border border-[var(--nova-border)] bg-[var(--nova-surface-2)] p-3 text-xs leading-5 text-[var(--nova-text-muted)]">
            <FileCheck2 className="mt-0.5 size-4 shrink-0 text-[var(--nova-warning)]" />
            <p className="break-words">{t('quality.plan.notEnabled')}</p>
          </div>
        </CardContent>
      </Card>

      <section aria-labelledby="quality-plan-title" className="min-w-0 space-y-3">
        <div>
          <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-[var(--nova-text-faint)]">{t('quality.section.authorContract')}</p>
          <h2 id="quality-plan-title" className="mt-1 text-lg font-semibold tracking-tight">{t('quality.plan.title')}</h2>
          <p className="mt-1 break-words text-xs leading-5 text-[var(--nova-text-muted)]">{t('quality.plan.description')}</p>
        </div>
        <div className="grid min-w-0 gap-3 xl:grid-cols-2">
          {spec.goal_catalog.map((goal) => <QualityGoalCard key={goal.id} goal={goal} detail={detail} />)}
        </div>
      </section>

      <QualityAssumptions detail={detail} />
      <QualityTechnicalMetadata detail={detail} />
    </article>
  )
}

function QualityGoalCard({ goal, detail }: { goal: QualityGoal; detail: QualityProfileDetailDTO }) {
  const { t, i18n } = useTranslation()
  const resolved = detail.profile.quality_spec.resolution.resolved_goals.find((item) => item.goal_id === goal.id)
  return (
    <Card className="min-w-0 border border-[var(--nova-border)] bg-[var(--nova-surface)] shadow-none">
      <CardHeader>
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
          <Badge variant={goal.priority === 'must' ? 'destructive' : 'outline'}>{t(`quality.priority.${goal.priority}`)}</Badge>
          {resolved ? <span className="break-all text-[10px] text-[var(--nova-text-faint)]">{t('quality.plan.currentValue', { value: formatSettingValue(resolved.value) })}</span> : null}
        </div>
        <CardTitle className="break-words text-sm leading-6">{localizedText(goal.description, i18n.language)}</CardTitle>
        <CardDescription className="break-words leading-5">{localizedText(goal.purpose, i18n.language)}</CardDescription>
      </CardHeader>
      <CardContent className="grid min-w-0 gap-2 sm:grid-cols-3 xl:grid-cols-1 2xl:grid-cols-3">
        <GoalFact icon={<Flag className="size-3.5" />} label={t('quality.plan.source')} value={`${t(`quality.source.${goal.source.source_kind}`, { defaultValue: humanizeToken(goal.source.source_kind) })} · ${goal.source.observed_at}`} />
        <GoalFact icon={<Target className="size-3.5" />} label={t('quality.plan.scope')} value={[...goal.scope.operation_ids, ...goal.scope.artifact_types].map(humanizeToken).join(' · ')} />
        <GoalFact icon={<ListChecks className="size-3.5" />} label={t('quality.plan.evidence')} value={localizedText(goal.evidence_requirement.description, i18n.language)} />
      </CardContent>
    </Card>
  )
}

function GoalFact({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-lg bg-[var(--nova-surface-2)] p-2.5">
      <div className="flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-[0.08em] text-[var(--nova-text-faint)]">{icon}{label}</div>
      <p className="mt-1 break-words text-xs leading-5 text-[var(--nova-text-muted)]">{value}</p>
    </div>
  )
}

function QualityAssumptions({ detail }: { detail: QualityProfileDetailDTO }) {
  const { t } = useTranslation()
  const groups: Array<{ key: string; items: QualityProfileSetting[] }> = [
    { key: 'artifacts', items: detail.profile.settings.required_artifacts },
    { key: 'capabilities', items: detail.profile.settings.required_capabilities },
    { key: 'candidatePolicy', items: detail.profile.settings.candidate_policy },
    { key: 'reviewRubric', items: detail.profile.settings.review_rubric },
    { key: 'export', items: detail.profile.settings.export_config },
  ]
  return (
    <Collapsible className="rounded-xl border border-[var(--nova-border)] bg-[var(--nova-surface)]">
      <CollapsibleTrigger className="flex w-full items-center justify-between gap-3 p-4 text-left text-sm font-medium">
        <span>{t('quality.plan.assumptions')}</span><ChevronDown className="size-4 text-[var(--nova-text-faint)]" />
      </CollapsibleTrigger>
      <CollapsibleContent className="grid min-w-0 gap-3 border-t border-[var(--nova-border)] p-4 md:grid-cols-2">
        {groups.map((group) => (
          <div key={group.key} className="min-w-0 rounded-lg bg-[var(--nova-surface-2)] p-3">
            <h3 className="text-xs font-medium">{t(`quality.plan.group.${group.key}`)}</h3>
            <ul className="mt-2 space-y-2">
              {group.items.map((item) => <li key={item.id} className="break-words text-xs leading-5 text-[var(--nova-text-muted)]">{formatSettingValue(item.value)}</li>)}
            </ul>
          </div>
        ))}
      </CollapsibleContent>
    </Collapsible>
  )
}

function QualityTechnicalMetadata({ detail }: { detail: QualityProfileDetailDTO }) {
  const { t } = useTranslation()
  return (
    <details data-testid="quality-technical-metadata" className="min-w-0 rounded-xl border border-[var(--nova-border)] bg-[var(--nova-surface)] p-4 text-xs">
      <summary className="cursor-pointer font-medium">{t('quality.metadata.title')}</summary>
      <dl className="mt-3 grid min-w-0 gap-2 text-[var(--nova-text-muted)] sm:grid-cols-2">
        <Metadata label={t('quality.metadata.profileID')} value={detail.profile_id} />
        <Metadata label={t('quality.metadata.contract')} value={detail.contract_version} />
        <Metadata label={t('quality.metadata.spec')} value={`${detail.quality_spec.spec_id} · r${detail.quality_spec.revision}`} />
        <Metadata label={t('quality.metadata.hash')} value={detail.quality_spec.sha256} />
      </dl>
    </details>
  )
}

function Metadata({ label, value }: { label: string; value: string }) {
  return <div className="min-w-0"><dt className="text-[10px] uppercase tracking-[0.08em] text-[var(--nova-text-faint)]">{label}</dt><dd className="mt-1 break-all">{value}</dd></div>
}

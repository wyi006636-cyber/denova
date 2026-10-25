import { useTranslation } from 'react-i18next'
import { extractChapterPrefix, type ChapterGoal } from './story-ledger-parser'

export function ChapterGoalCard({
  goal,
  chapterPath,
  hasRecentPlans = false,
  onOpenPlan,
}: {
  goal?: ChapterGoal | null
  chapterPath?: string
  hasRecentPlans?: boolean
  onOpenPlan?: () => void
}) {
  const { t } = useTranslation()
  if (!goal) {
    const emptyMessage = chapterPath && !extractChapterPrefix(chapterPath)
      ? t('planning.chapterGoal.noPrefix')
      : hasRecentPlans
        ? t('planning.chapterGoal.notInRecentPlans')
        : t('planning.chapterGoal.empty')
    return (
      <div className="rounded border border-dashed border-[var(--nova-border)] bg-[var(--nova-surface)] px-2.5 py-2 text-[11px] leading-4 text-[var(--nova-text-faint)]">
        <div>{emptyMessage}</div>
        {onOpenPlan ? <button type="button" className="nova-nav-item mt-1.5 rounded px-2 py-1 text-[10px] text-[var(--nova-text-muted)]" onClick={onOpenPlan}>{t('planning.chapterGoal.openPlan')}</button> : null}
      </div>
    )
  }
  const fields = [
    ['planning.chapterGoal.goal', goal.goal],
    ['planning.chapterGoal.conflict', goal.conflict],
    ['planning.chapterGoal.plant', goal.plant],
    ['planning.chapterGoal.payoff', goal.payoff],
    ['planning.chapterGoal.boundary', goal.boundary],
    ['planning.chapterGoal.wordTarget', goal.wordTarget],
    ['planning.chapterGoal.hook', goal.hook],
  ] as const
  return (
    <div className="rounded border border-[var(--nova-border)] bg-[var(--nova-surface)] px-2.5 py-2 text-[11px]">
      <div className="mb-1.5 flex min-w-0 items-center gap-2">
        <span className="shrink-0 rounded bg-[var(--nova-surface-2)] px-1.5 py-0.5 font-mono text-[10px] text-[var(--nova-text-muted)]">{goal.anchor}</span>
        <span className="min-w-0 truncate font-medium text-[var(--nova-text)]">{goal.title || t('planning.chapterGoal.untitled')}</span>
      </div>
      <dl className="space-y-1">
        {fields.map(([label, value]) => value ? (
          <div key={label} className="grid min-w-0 grid-cols-[auto,minmax(0,1fr)] gap-2 leading-4">
            <dt className="shrink-0 text-[var(--nova-text-faint)]">{t(label)}</dt>
            <dd className="min-w-0 whitespace-pre-wrap break-words text-[var(--nova-text-muted)]">{value}</dd>
          </div>
        ) : null)}
      </dl>
      {onOpenPlan ? <button type="button" className="nova-nav-item mt-2 rounded px-2 py-1 text-[10px] text-[var(--nova-text-muted)]" onClick={onOpenPlan}>{t('planning.chapterGoal.openPlan')}</button> : null}
    </div>
  )
}

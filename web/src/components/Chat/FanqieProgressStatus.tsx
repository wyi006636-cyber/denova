import { CircleCheck, CircleX, GitCompareArrows, ListChecks, MessageCircle, PenLine } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import type { WorkspaceChangeGroupSummary } from '@/features/changes/types'
import { buildAgentMessageViews } from '@/lib/agent-message-view'
import type { AgentUIMessage } from '@/lib/agent-ui'

export type FanqieProgressStage = 'idea' | 'proposal' | 'outline' | 'writing' | 'diff' | 'accepted' | 'rejected'

export interface FanqieProgress {
  stage: FanqieProgressStage
  files: string[]
  chapter?: number
  totalChapters?: number
  operation?: 'write' | 'edit'
}

interface DeriveFanqieProgressInput {
  messages: AgentUIMessage[]
  isStreaming: boolean
  changeGroups: WorkspaceChangeGroupSummary[]
}

interface FanqieProgressMarker {
  stage: 'idea' | 'proposal' | 'outline' | 'chapter'
  chapter?: number
  totalChapters?: number
  path?: string
}

export function deriveFanqieProgress({ messages, isStreaming, changeGroups }: DeriveFanqieProgressInput): FanqieProgress {
  const views = buildAgentMessageViews(messages).filter((view) => !view.metadata.subagent)
  const marker = latestProgressMarker(views.filter((view) => view.kind === 'assistant').map((view) => view.content))
  const activeMutation = isStreaming
    ? [...views].reverse().find((view) => view.kind === 'tool' && view.status === 'running' && (view.toolName === 'write_file' || view.toolName === 'edit_file'))
    : undefined

  if (activeMutation) {
    const path = toolFilePath(activeMutation.input)
    return {
      stage: 'writing',
      files: path ? [path] : [],
      chapter: chapterNumber(path) ?? marker?.chapter,
      totalChapters: marker?.totalChapters,
      operation: activeMutation.toolName === 'edit_file' ? 'edit' : 'write',
    }
  }

  const pendingGroup = latestGroup(changeGroups.filter((group) => (
    (group.review_status === 'pending' || group.review_status === 'mixed') && group.apply_state !== 'reverted'
  )))
  const pendingFiles = groupFiles(pendingGroup)
  const pendingChapter = pendingFiles.find(isChapterPath)
  if (pendingChapter) {
    return {
      stage: 'diff',
      files: pendingFiles,
      chapter: marker?.chapter ?? chapterNumber(pendingChapter),
      totalChapters: marker?.totalChapters,
    }
  }

  if (marker?.stage === 'chapter') {
    const decidedGroup = latestGroup(changeGroups.filter((group) => (
      (group.review_status === 'accepted' || group.review_status === 'rejected')
      && (!marker.path || groupFiles(group).includes(marker.path))
    )))
    if (decidedGroup) {
      return {
        stage: decidedGroup.review_status === 'accepted' ? 'accepted' : 'rejected',
        files: groupFiles(decidedGroup),
        chapter: marker.chapter ?? chapterNumber(marker.path || ''),
        totalChapters: marker.totalChapters,
      }
    }
    return {
      stage: 'diff',
      files: marker.path ? [marker.path] : [],
      chapter: marker.chapter ?? chapterNumber(marker.path || ''),
      totalChapters: marker.totalChapters,
    }
  }
  if (marker?.stage === 'outline') {
    return { stage: 'outline', files: [], totalChapters: marker.totalChapters }
  }
  if (marker?.stage === 'proposal') return { stage: 'proposal', files: [] }
  return { stage: 'idea', files: [] }
}

export function FanqieProgressStatus({ progress }: { progress: FanqieProgress }) {
  const { t } = useTranslation()
  const Icon = progress.stage === 'idea'
    ? MessageCircle
    : progress.stage === 'proposal' || progress.stage === 'outline'
      ? ListChecks
      : progress.stage === 'writing'
        ? PenLine
        : progress.stage === 'accepted'
          ? CircleCheck
          : progress.stage === 'rejected'
            ? CircleX
            : GitCompareArrows
  const accent = progress.stage === 'diff'
    ? 'text-[var(--nova-warning)]'
    : progress.stage === 'accepted' || progress.stage === 'writing'
      ? 'text-[var(--nova-accent-green)]'
      : progress.stage === 'rejected'
        ? 'text-[var(--nova-danger)]'
        : 'text-[var(--nova-accent-blue)]'
  return (
    <section
      role="status"
      aria-live="polite"
      aria-label={t('writingAgent.progress.aria')}
      className="shrink-0 border-b border-[var(--nova-border)] bg-[var(--nova-surface-2)] px-4 py-3"
    >
      <div className="flex min-w-0 items-start gap-2.5">
        <span className={`mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-md border border-[var(--nova-border)] bg-[var(--nova-surface)] ${accent}`}>
          <Icon className="h-3.5 w-3.5" />
        </span>
        <div className="min-w-0 flex-1">
          <span className="text-[10px] font-semibold tracking-[0.08em] text-[var(--nova-text-faint)]">
            {t('writingAgent.progress.label')}
          </span>
          <div className="mt-2 grid min-w-0 gap-2 text-[12px] leading-[18px]">
            <ProgressLine label={t('writingAgent.progress.current')}>
              <strong className="block text-[13px] font-semibold text-[var(--nova-text)]">
                {progressTitle(t, progress)}
              </strong>
              <p className="mt-0.5 text-[12px] text-[var(--nova-text-muted)]">
                {progressDetail(t, progress)}
              </p>
            </ProgressLine>
            <ProgressLine label={t('writingAgent.progress.files')}>
              {progress.files.length ? (
                <div className="grid gap-0.5 font-mono text-[11px] text-[var(--nova-text-muted)]">
                  {progress.files.map((path) => <span key={path} className="break-all">{path}</span>)}
                </div>
              ) : (
                <span className="italic text-[var(--nova-text-muted)]">{t('writingAgent.progress.filesNone')}</span>
              )}
            </ProgressLine>
            <ProgressLine label={t('writingAgent.progress.next')}>
              <span className="whitespace-normal break-words text-[var(--nova-text-muted)]">
                {progressNext(t, progress)}
              </span>
            </ProgressLine>
          </div>
        </div>
      </div>
    </section>
  )
}

function ProgressLine({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid min-w-0 grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-x-2">
      <span className="font-medium text-[var(--nova-text-faint)]">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  )
}

function progressTitle(t: ReturnType<typeof useTranslation>['t'], progress: FanqieProgress) {
  if (progress.stage === 'idea') return t('writingAgent.progress.ideaTitle')
  if (progress.stage === 'proposal') return t('writingAgent.progress.proposalTitle')
  if (progress.stage === 'outline') return t('writingAgent.progress.outlineTitle')
  if (progress.stage === 'diff') return t('writingAgent.progress.diffTitle')
  if (progress.stage === 'accepted') return t('writingAgent.progress.acceptedTitle')
  if (progress.stage === 'rejected') return t('writingAgent.progress.rejectedTitle')
  const action = progress.operation === 'edit' ? 'editing' : 'writing'
  if (progress.chapter && progress.totalChapters) {
    return t(`writingAgent.progress.${action}ChapterWithTotal`, { chapter: progress.chapter, total: progress.totalChapters })
  }
  if (progress.chapter) return t(`writingAgent.progress.${action}Chapter`, { chapter: progress.chapter })
  return t(`writingAgent.progress.${action}File`)
}

function progressDetail(t: ReturnType<typeof useTranslation>['t'], progress: FanqieProgress) {
  if (progress.stage === 'idea') return t('writingAgent.progress.ideaDetail')
  if (progress.stage === 'proposal') return t('writingAgent.progress.proposalDetail')
  if (progress.stage === 'outline') return t('writingAgent.progress.outlineDetail')
  if (progress.stage === 'diff') return t('writingAgent.progress.diffDetail')
  if (progress.stage === 'accepted') return t('writingAgent.progress.acceptedDetail')
  if (progress.stage === 'rejected') return t('writingAgent.progress.rejectedDetail')
  return t(progress.operation === 'edit' ? 'writingAgent.progress.editingDetail' : 'writingAgent.progress.writingDetail')
}

function progressNext(t: ReturnType<typeof useTranslation>['t'], progress: FanqieProgress) {
  if (progress.stage === 'idea') return t('writingAgent.progress.ideaNext')
  if (progress.stage === 'proposal') return t('writingAgent.progress.proposalNext')
  if (progress.stage === 'outline') return t('writingAgent.progress.outlineNext')
  if (progress.stage === 'diff') return t('writingAgent.progress.diffNext')
  if (progress.stage === 'accepted') return t('writingAgent.progress.acceptedNext')
  if (progress.stage === 'rejected') return t('writingAgent.progress.rejectedNext')
  return t('writingAgent.progress.writingNext')
}

function latestProgressMarker(contents: string[]): FanqieProgressMarker | undefined {
  let latest: FanqieProgressMarker | undefined
  const markerPattern = /<!--\s*fanqie-progress:\s*(idea|proposal|outline|chapter)\b([\s\S]*?)-->/gi
  for (const content of contents) {
    for (const match of content.matchAll(markerPattern)) {
      const attributes = markerAttributes(match[2])
      latest = {
        stage: match[1].toLowerCase() as FanqieProgressMarker['stage'],
        chapter: positiveNumber(attributes.chapter),
        totalChapters: positiveNumber(attributes.total),
        path: attributes.path,
      }
    }
  }
  return latest
}

function markerAttributes(source: string): Record<string, string> {
  const attributes: Record<string, string> = {}
  const pattern = /\b(chapter|total|path)=(?:"([^"]*)"|'([^']*)'|([^\s]+))/gi
  for (const match of source.matchAll(pattern)) attributes[match[1].toLowerCase()] = match[2] ?? match[3] ?? match[4] ?? ''
  return attributes
}

function toolFilePath(input: unknown): string {
  if (input && typeof input === 'object' && !Array.isArray(input)) {
    const record = input as Record<string, unknown>
    const value = record.file_path ?? record.path
    return typeof value === 'string' ? value.trim() : ''
  }
  if (typeof input !== 'string') return ''
  try {
    return toolFilePath(JSON.parse(input))
  } catch {
    return ''
  }
}

function latestGroup(groups: WorkspaceChangeGroupSummary[]): WorkspaceChangeGroupSummary | undefined {
  return [...groups].sort((left, right) => right.created_at.localeCompare(left.created_at))[0]
}

function groupFiles(group?: WorkspaceChangeGroupSummary): string[] {
  if (!group) return []
  return uniquePaths(group.paths ?? group.change_sets?.map((changeSet) => changeSet.path) ?? [])
}

function uniquePaths(paths: string[]): string[] {
  return Array.from(new Set(paths.map((path) => path.trim()).filter(Boolean)))
}

function isChapterPath(path: string) {
  return /^chapters\//i.test(path.trim())
}

function chapterNumber(path: string): number | undefined {
  const fileName = path.split('/').pop() || ''
  const match = fileName.match(/^(?:ch(?:apter)?[-_ ]*0*|第)(\d{1,5})/i) || fileName.match(/^0*(\d{1,5})\b/)
  return positiveNumber(match?.[1])
}

function positiveNumber(value?: string): number | undefined {
  const number = Number(value)
  return Number.isInteger(number) && number > 0 ? number : undefined
}

import { useEffect, useMemo, useRef, useState } from 'react'
import { AlertCircle, CheckCircle2, FilePenLine, LoaderCircle, TriangleAlert, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ThemedMarkdownRenderer } from '@/components/common/MarkdownRenderer'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import { getResolvedLocale } from '@/i18n'
import { APIError } from '@/lib/api-client/client'
import {
  confirmShortFictionCandidate,
  generateShortFictionCandidate,
  type ShortFictionCandidate,
  type ShortFictionConfirmationResult,
} from '@/lib/api-client/short-fiction'
import { readFile } from '@/lib/api-client/workspace'

type SheetState =
  | { step: 'brief' }
  | { step: 'generating' }
  | { step: 'preview'; candidate: ShortFictionCandidate }
  | { step: 'confirming'; candidate: ShortFictionCandidate }
  | { step: 'result'; result: ShortFictionConfirmationResult }
  | { step: 'error'; phase: 'generate' | 'confirm'; message: string; candidate?: ShortFictionCandidate }

interface FanqieCandidateSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  workspace: string
  selectedFile: string | null
  fileSuggestions: string[]
  disabled: boolean
  onWorkspaceChanged?: (paths: string[]) => void | Promise<void>
}

export function FanqieCandidateSheet({
  open,
  onOpenChange,
  workspace,
  selectedFile,
  fileSuggestions,
  disabled,
  onWorkspaceChanged,
}: FanqieCandidateSheetProps) {
  const { t } = useTranslation()
  const [targetPath, setTargetPath] = useState(() => normalizeMarkdownTarget(selectedFile || '') || '')
  const [brief, setBrief] = useState('')
  const [state, setState] = useState<SheetState>({ step: 'brief' })
  const targetEdited = useRef(false)

  const targetSuggestions = useMemo(() => (
    Array.from(new Set(fileSuggestions.map(normalizeMarkdownTarget).filter((path): path is string => Boolean(path))))
      .slice(0, 6)
  ), [fileSuggestions])

  useEffect(() => {
    if (!open || targetEdited.current) return
    setTargetPath(normalizeMarkdownTarget(selectedFile || '') || '')
  }, [open, selectedFile])

  const generate = async () => {
    const normalizedTarget = normalizeMarkdownTarget(targetPath)
    if (!normalizedTarget) {
      setState({ step: 'error', phase: 'generate', message: t('chat.fanqie.error.invalidTarget') })
      return
    }
    if (!brief.trim()) {
      setState({ step: 'error', phase: 'generate', message: t('chat.fanqie.error.briefRequired') })
      return
    }
    if (!workspace.trim()) {
      setState({ step: 'error', phase: 'generate', message: t('chat.fanqie.error.workspaceRequired') })
      return
    }

    setTargetPath(normalizedTarget)
    setState({ step: 'generating' })

    let generationWorkspace = workspace
    let baseRevision = 'missing'
    try {
      const document = await readFile(normalizedTarget)
      if (!document.workspace || !document.revision) {
        setState({ step: 'error', phase: 'generate', message: t('chat.fanqie.error.revisionUnavailable') })
        return
      }
      generationWorkspace = document.workspace
      baseRevision = document.revision
    } catch (error) {
      if (!(error instanceof APIError) || error.status !== 404) {
        setState({ step: 'error', phase: 'generate', message: apiErrorMessage(error, t('chat.fanqie.error.readFailed')) })
        return
      }
    }

    try {
      const candidate = await generateShortFictionCandidate({
        workspace: generationWorkspace,
        profile_id: 'fanqie_short',
        target_path: normalizedTarget,
        base_revision: baseRevision,
        brief: brief.trim(),
      }, getResolvedLocale())
      if (!candidate.preview_markdown.trim()) {
        setState({ step: 'error', phase: 'generate', message: t('chat.fanqie.error.emptyCandidate') })
        return
      }
      setState({ step: 'preview', candidate })
    } catch (error) {
      setState({ step: 'error', phase: 'generate', message: generationErrorMessage(error, t) })
    }
  }

  const confirm = async (candidate: ShortFictionCandidate) => {
    if (!candidate.preview_markdown.trim()) return
    setState({ step: 'confirming', candidate })
    try {
      const result = await confirmShortFictionCandidate({ candidate }, getResolvedLocale())
      setState({ step: 'result', result })
      if (result.status === 'written' || result.status === 'written_checkpoint_failed') {
        void Promise.resolve(onWorkspaceChanged?.([candidate.target_path])).catch((error) => {
          console.error('[FanqieCandidateSheet] workspace refresh failed file=FanqieCandidateSheet.tsx path=%s', candidate.target_path, error)
        })
      }
    } catch (error) {
      setState({ step: 'error', phase: 'confirm', message: confirmationErrorMessage(error, t), candidate })
    }
  }

  const briefVisible = state.step === 'brief'
    || state.step === 'generating'
    || (state.step === 'error' && state.phase === 'generate')
  const candidate = state.step === 'preview' || state.step === 'confirming'
    ? state.candidate
    : state.step === 'error' && state.phase === 'confirm'
      ? state.candidate
      : undefined

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        showCloseButton={false}
        style={{ width: 'min(720px, calc(100vw - 0.75rem))', maxWidth: 'none' }}
        className="min-w-0 gap-0 overflow-hidden border-[var(--nova-border)] bg-[var(--nova-surface)] p-0 text-[var(--nova-text)] shadow-[var(--nova-shadow)]"
      >
        <SheetHeader className="shrink-0 gap-0 border-b border-[var(--nova-border)] bg-[var(--nova-surface-2)] px-4 py-3 sm:px-5">
          <div className="flex min-w-0 items-start gap-3 pr-0">
            <div className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-[var(--nova-radius)] border border-[var(--nova-border)] bg-[var(--nova-surface)] text-[var(--nova-text-muted)]">
              <FilePenLine className="size-4" />
            </div>
            <div className="min-w-0 flex-1">
              <SheetTitle className="text-sm font-semibold text-[var(--nova-text)]">{t('chat.fanqie.title')}</SheetTitle>
              <SheetDescription className="mt-1 max-w-xl text-xs leading-5 text-[var(--nova-text-faint)]">
                {t('chat.fanqie.description')}
              </SheetDescription>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t('chat.fanqie.close')}
              title={t('chat.fanqie.close')}
              onClick={() => onOpenChange(false)}
            >
              <X />
            </Button>
          </div>
        </SheetHeader>

        <ScrollArea
          className="min-h-0 min-w-0 flex-1 overflow-x-hidden"
          viewportProps={{ className: 'overflow-x-hidden' }}
          data-testid="fanqie-sheet-scroll"
        >
          <div className="mx-auto flex min-w-0 max-w-2xl flex-col gap-4 px-4 py-5 sm:px-6">
            {briefVisible ? (
              <BriefStep
                targetPath={targetPath}
                brief={brief}
                suggestions={targetSuggestions}
                busy={state.step === 'generating'}
                disabled={disabled}
                error={state.step === 'error' && state.phase === 'generate' ? state.message : undefined}
                onTargetPathChange={(value) => {
                  targetEdited.current = true
                  setTargetPath(value)
                }}
                onBriefChange={setBrief}
                onGenerate={() => void generate()}
                t={t}
              />
            ) : null}

            {candidate ? (
              <PreviewStep
                candidate={candidate}
                confirming={state.step === 'confirming'}
                disabled={disabled}
                error={state.step === 'error' && state.phase === 'confirm' ? state.message : undefined}
                onBack={() => setState({ step: 'brief' })}
                onConfirm={() => void confirm(candidate)}
                t={t}
              />
            ) : null}

            {state.step === 'result' ? <ResultStep result={state.result} targetPath={targetPath} t={t} /> : null}
          </div>
        </ScrollArea>
      </SheetContent>
    </Sheet>
  )
}

interface StepCopy {
  (key: string, options?: Record<string, unknown>): string
}

function BriefStep({
  targetPath,
  brief,
  suggestions,
  busy,
  disabled,
  error,
  onTargetPathChange,
  onBriefChange,
  onGenerate,
  t,
}: {
  targetPath: string
  brief: string
  suggestions: string[]
  busy: boolean
  disabled: boolean
  error?: string
  onTargetPathChange: (value: string) => void
  onBriefChange: (value: string) => void
  onGenerate: () => void
  t: StepCopy
}) {
  return (
    <section className="flex min-w-0 flex-col gap-4" aria-labelledby="fanqie-brief-heading">
      <div className="flex items-center justify-between gap-3">
        <h2 id="fanqie-brief-heading" className="text-sm font-semibold text-[var(--nova-text)]">{t('chat.fanqie.brief.title')}</h2>
        <Badge variant="outline">{busy ? t('chat.fanqie.status.generating') : t('chat.fanqie.status.brief')}</Badge>
      </div>

      {error ? <ErrorNotice title={t('chat.fanqie.error.generateTitle')} message={error} /> : null}

      <div className="flex min-w-0 flex-col gap-1.5">
        <label className="text-xs font-medium text-[var(--nova-text)]" htmlFor="fanqie-target-path">{t('chat.fanqie.target.label')}</label>
        <Textarea
          id="fanqie-target-path"
          value={targetPath}
          aria-invalid={Boolean(error && !normalizeMarkdownTarget(targetPath))}
          disabled={disabled || busy}
          autoResize
          minRows={1}
          maxRows={3}
          multilineMode="always"
          className="min-h-9 rounded-[var(--nova-radius)] border-[var(--nova-border)] bg-[var(--nova-surface-2)] font-mono text-xs [overflow-wrap:anywhere]"
          placeholder={t('chat.fanqie.target.placeholder')}
          onChange={(event) => onTargetPathChange(event.target.value)}
        />
        <span className="text-[11px] leading-4 text-[var(--nova-text-faint)]">{t('chat.fanqie.target.help')}</span>
      </div>

      {suggestions.length ? (
        <div className="flex min-w-0 flex-wrap gap-1.5" aria-label={t('chat.fanqie.target.suggestions')}>
          {suggestions.map((suggestion) => (
            <Button
              key={suggestion}
              type="button"
              variant="outline"
              size="xs"
              disabled={disabled || busy}
              className="h-auto max-w-full whitespace-normal break-all py-1 text-left font-mono text-[10px] [overflow-wrap:anywhere]"
              onClick={() => onTargetPathChange(suggestion)}
            >
              {suggestion}
            </Button>
          ))}
        </div>
      ) : null}

      <div className="flex min-w-0 flex-col gap-1.5">
        <label className="text-xs font-medium text-[var(--nova-text)]" htmlFor="fanqie-brief">{t('chat.fanqie.brief.label')}</label>
        <Textarea
          id="fanqie-brief"
          value={brief}
          disabled={disabled || busy}
          minRows={7}
          maxRows={14}
          multilineMode="always"
          className="min-h-40 rounded-[var(--nova-radius)] border-[var(--nova-border)] bg-[var(--nova-surface-2)] px-3 py-2.5 leading-6"
          placeholder={t('chat.fanqie.brief.placeholder')}
          onChange={(event) => onBriefChange(event.target.value)}
        />
        <span className="text-[11px] leading-4 text-[var(--nova-text-faint)]">{t('chat.fanqie.brief.help')}</span>
      </div>

      <div className="flex justify-end">
        <Button type="button" disabled={disabled || busy} onClick={onGenerate}>
          {busy ? <LoaderCircle className="animate-spin" /> : <FilePenLine />}
          {busy ? t('chat.fanqie.action.generating') : t('chat.fanqie.action.generate')}
        </Button>
      </div>
    </section>
  )
}

function PreviewStep({ candidate, confirming, disabled, error, onBack, onConfirm, t }: {
  candidate: ShortFictionCandidate
  confirming: boolean
  disabled: boolean
  error?: string
  onBack: () => void
  onConfirm: () => void
  t: StepCopy
}) {
  return (
    <section className="flex min-w-0 flex-col gap-4" aria-labelledby="fanqie-preview-heading">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 id="fanqie-preview-heading" className="text-sm font-semibold text-[var(--nova-text)]">{t('chat.fanqie.preview.title')}</h2>
          <p className="mt-1 text-xs leading-5 text-[var(--nova-text-faint)]">{t('chat.fanqie.preview.help')}</p>
        </div>
        <Badge variant="outline">{confirming ? t('chat.fanqie.status.confirming') : t('chat.fanqie.status.preview')}</Badge>
      </div>

      {error ? <ErrorNotice title={t('chat.fanqie.error.confirmTitle')} message={error} /> : null}

      <dl className="grid min-w-0 gap-2 rounded-[var(--nova-radius)] border border-[var(--nova-border)] bg-[var(--nova-surface-2)] p-3 text-[11px] sm:grid-cols-2">
        <Metadata label={t('chat.fanqie.preview.target')} value={candidate.target_path} />
        <Metadata label={t('chat.fanqie.preview.revision')} value={candidate.base_revision} />
        <Metadata label={t('chat.fanqie.preview.modelProfile')} value={candidate.model_profile_id} />
        <Metadata label={t('chat.fanqie.preview.model')} value={candidate.model} />
      </dl>

      <div
        data-testid="fanqie-preview"
        className="min-w-0 overflow-x-hidden rounded-[var(--nova-radius)] border border-[var(--nova-border)] bg-[var(--nova-bg)] px-4 py-5 shadow-inner [overflow-wrap:anywhere] [&_*]:max-w-full [&_code]:whitespace-pre-wrap [&_pre]:overflow-x-hidden [&_table]:overflow-x-hidden"
      >
        <ThemedMarkdownRenderer content={candidate.preview_markdown} className="min-w-0 break-words text-sm leading-7 [overflow-wrap:anywhere]" />
      </div>

      <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-between">
        <Button type="button" variant="outline" disabled={disabled || confirming} onClick={onBack}>
          {t('chat.fanqie.action.back')}
        </Button>
        <Button type="button" disabled={disabled || confirming || !candidate.preview_markdown.trim()} onClick={onConfirm}>
          {confirming ? <LoaderCircle className="animate-spin" /> : <CheckCircle2 />}
          {confirming ? t('chat.fanqie.action.confirming') : t('chat.fanqie.action.confirm')}
        </Button>
      </div>
    </section>
  )
}

function ResultStep({ result, targetPath, t }: { result: ShortFictionConfirmationResult; targetPath: string; t: StepCopy }) {
  const partial = result.status === 'written_checkpoint_failed'
  return (
    <section className="flex min-w-0 flex-col gap-4" aria-labelledby="fanqie-result-heading">
      <div className={`rounded-[var(--nova-radius)] border p-4 ${partial ? 'border-amber-500/40 bg-amber-500/10' : 'border-emerald-500/40 bg-emerald-500/10'}`}>
        <div className="flex items-start gap-3">
          {partial ? <TriangleAlert className="mt-0.5 size-5 shrink-0 text-amber-600 dark:text-amber-400" /> : <CheckCircle2 className="mt-0.5 size-5 shrink-0 text-emerald-600 dark:text-emerald-400" />}
          <div className="min-w-0">
            <h2 id="fanqie-result-heading" className="text-sm font-semibold text-[var(--nova-text)]">
              {partial ? t('chat.fanqie.result.partialTitle') : t('chat.fanqie.result.successTitle')}
            </h2>
            <p className="mt-1 break-words text-xs leading-5 text-[var(--nova-text-muted)] [overflow-wrap:anywhere]">
              {partial ? t('chat.fanqie.result.partialBody') : t('chat.fanqie.result.successBody')}
            </p>
          </div>
        </div>
      </div>
      <dl className="grid min-w-0 gap-2 rounded-[var(--nova-radius)] border border-[var(--nova-border)] bg-[var(--nova-surface-2)] p-3 text-[11px]">
        <Metadata label={t('chat.fanqie.preview.target')} value={targetPath} />
        <Metadata label={t('chat.fanqie.result.writeRevision')} value={result.write_revision} />
        {result.status === 'written' ? <Metadata label={t('chat.fanqie.result.version')} value={result.checkpoint.version_id} /> : null}
      </dl>
    </section>
  )
}

function Metadata({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-[var(--nova-text-faint)]">{label}</dt>
      <dd className="mt-0.5 break-all font-mono text-[var(--nova-text-muted)] [overflow-wrap:anywhere]">{value}</dd>
    </div>
  )
}

function ErrorNotice({ title, message }: { title: string; message: string }) {
  return (
    <div role="alert" className="flex min-w-0 items-start gap-2 rounded-[var(--nova-radius)] border border-red-500/35 bg-red-500/10 p-3 text-xs leading-5 text-[var(--nova-text-muted)]">
      <AlertCircle className="mt-0.5 size-4 shrink-0 text-red-600 dark:text-red-400" />
      <div className="min-w-0">
        <div className="font-medium text-[var(--nova-text)]">{title}</div>
        <div className="mt-0.5 break-words [overflow-wrap:anywhere]">{message}</div>
      </div>
    </div>
  )
}

function normalizeMarkdownTarget(value: string): string | null {
  const slashPath = value.trim().replace(/\\/g, '/')
  if (!slashPath || slashPath.startsWith('/') || /^[a-z]:\//i.test(slashPath)) return null

  const segments = slashPath.split('/').filter(Boolean)
  if (!segments.length || segments.some((segment) => segment.startsWith('.'))) return null

  const normalized = segments.join('/')
  return normalized.endsWith('.md') ? normalized : null
}

function apiErrorMessage(error: unknown, fallback: string) {
  return error instanceof APIError && error.message.trim() ? error.message : fallback
}

function generationErrorMessage(error: unknown, t: StepCopy) {
  if (error instanceof APIError) {
    if (error.code === 'generation_empty') return t('chat.fanqie.error.emptyCandidate')
    if (error.code === 'candidate_too_large' || error.code === 'oversized') return t('chat.fanqie.error.candidateTooLarge')
    if (error.code === 'workspace_conflict') return t('chat.fanqie.error.workspaceConflict')
  }
  return apiErrorMessage(error, t('chat.fanqie.error.generateFailed'))
}

function confirmationErrorMessage(error: unknown, t: StepCopy) {
  if (error instanceof APIError) {
    if (error.code === 'revision_conflict') return t('chat.fanqie.error.revisionConflict')
    if (error.code === 'workspace_conflict') return t('chat.fanqie.error.workspaceConflict')
  }
  return apiErrorMessage(error, t('chat.fanqie.error.confirmFailed'))
}

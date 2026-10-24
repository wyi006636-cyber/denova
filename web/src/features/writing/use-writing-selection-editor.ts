import { useCallback, useEffect, useMemo } from 'react'
import type { Editor } from '@tiptap/react'
import { closeHistory } from '@tiptap/pm/history'
import { useTranslation } from 'react-i18next'
import { readProjectFile, type TextSelection } from '@/lib/api'
import { toast } from '@/lib/toast'
import { getLineNumber } from '@/components/Editor/editorDocument'
import { WRITING_AGENT_INIT_EVENT } from '@/features/onboarding/events'
import { useWritingSelection, type WritingAdoptionEditor } from './WritingSelectionContext'
import { adoptionTargetFailure, rememberSelectionAction, writingSelectionPrompt, type AdoptionMode, type WritingSelectionAction, type WritingSelectionTarget } from './selection-actions'

export function useWritingSelectionEditor({ editor, projectId, fileName, enabled, flush, readDraft, onQuoteSelection }: {
  editor: Editor | null
  projectId: string
  fileName: string | null
  enabled: boolean
  flush: () => Promise<boolean>
  readDraft: () => string
  onQuoteSelection?: (selection: TextSelection) => void
}) {
  const { t } = useTranslation()
  const selectionContext = useWritingSelection()
  const register = selectionContext?.register
  const adapter = useMemo<(WritingAdoptionEditor & { activate: () => void; dispose: () => void }) | null>(() => {
    if (!enabled || !editor || !fileName) return null
    let mounted = true
    const current = (revision: string, mode: AdoptionMode): WritingSelectionTarget | null => {
      if (!mounted || editor.isDestroyed) return null
      const { from, to, head } = editor.state.selection
      const start = mode === 'insert' ? head : from
      const end = mode === 'insert' ? head : to
      return {
        projectId, fileName, revision, document: JSON.stringify(editor.getJSON()), draft: readDraft(),
        from: start, to: end,
        quote: { fileName, startLine: getLineNumber(editor.state.doc, start), endLine: getLineNumber(editor.state.doc, end), content: editor.state.doc.textBetween(start, end, '\n') },
      }
    }
    return {
      projectId, fileName,
      capture: async (mode) => {
        const target = current('', mode)
        if (!target || !await flush()) throw new Error('Manuscript draft could not be saved')
        const canonical = await readProjectFile(projectId, fileName)
        if (!mounted || canonical.project_id !== projectId || !canonical.revision
          || adoptionTargetFailure(current('', mode), target)) throw new Error('Manuscript changed while preparing the target')
        return { ...target, revision: canonical.revision }
      },
      apply: async (target, text, mode) => {
        let failure = adoptionTargetFailure(current(target.revision, mode), target)
        if (failure) return failure
        if (!await flush()) return 'saveFailed'
        const canonical = await readProjectFile(projectId, fileName)
        failure = adoptionTargetFailure(current(canonical.revision, mode), target)
        if (failure || canonical.project_id !== target.projectId) return failure || 'unavailable'
        // Keep adoption in the existing draft/revision save path and isolate its undo step.
        editor.view.dispatch(closeHistory(editor.state.tr))
        editor.commands.insertContentAt({ from: target.from, to: target.to }, text, { contentType: 'markdown' })
        editor.view.dispatch(closeHistory(editor.state.tr))
        return await flush() ? null : 'saveFailed'
      },
      activate: () => { mounted = true },
      // An async read must never outlive the foreground document that owned its coordinates.
      dispose: () => { mounted = false },
    }
  }, [editor, enabled, fileName, flush, projectId, readDraft])

  useEffect(() => {
    if (!adapter || !register) return
    adapter.activate()
    const unregister = register(adapter)
    return () => { unregister(); adapter.dispose() }
  }, [adapter, register])

  return useCallback(async (action: WritingSelectionAction) => {
    if (!adapter || !onQuoteSelection) return
    try {
      const target = await adapter.capture('replace')
      if (!target.quote.content.trim()) return
      rememberSelectionAction(target)
      selectionContext?.setLatestActionTarget(target)
      onQuoteSelection(target.quote)
      // Existing quick prompts default to fill: preserve author control over the instruction and paid submission.
      window.dispatchEvent(new CustomEvent(WRITING_AGENT_INIT_EVENT, { detail: { prompt: writingSelectionPrompt(t, action), autoSend: false } }))
      console.info('[writing/selection-actions] Prepared selection draft', { projectId, fileName, action })
    } catch (error) {
      console.warn('[writing/selection-actions] Could not prepare selection draft', { projectId, fileName, error })
      toast.error(t('editor.adoption.changed'))
    }
  }, [adapter, fileName, onQuoteSelection, projectId, selectionContext, t])
}

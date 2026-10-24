import { useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { Clipboard, Replace, TextCursorInput } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { toast } from '@/lib/toast'
import { useWritingSelection, WritingReplyContext } from './WritingSelectionContext'
import { extractReplyProse, type AdoptionFailure, type AdoptionMode, type WritingSelectionTarget } from './selection-actions'

/** An author-selected reply range takes precedence over a single explicitly marked prose block. */
export function WritingReplyAdoption({ messageId, content, children }: { messageId: string; content: string; children: ReactNode }) {
  const scope = useContext(WritingReplyContext)
  // The provider maps only assistant replies after the exact action turn. Do
  // not fall back to a pending editor target: that would expose replacement on
  // historical, unrelated, or unsent replies.
  const target = scope?.get(messageId)
  return scope ? <ReplyAdoption target={target} content={content}>{children}</ReplyAdoption> : children
}

function ReplyAdoption({ target, content, children }: { target?: WritingSelectionTarget; content: string; children: ReactNode }) {
  const bridge = useWritingSelection()
  const { t } = useTranslation()
  const bodyRef = useRef<HTMLDivElement>(null)
  const [selected, setSelected] = useState('')
  const [preview, setPreview] = useState<{ mode: AdoptionMode; target: WritingSelectionTarget; text: string } | null>(null)
  const [failure, setFailure] = useState<AdoptionFailure | null>(null)
  const [busy, setBusy] = useState(false)
  const effectiveTarget = target
  const prose = selected || extractReplyProse(content)

  const captureSelection = useCallback(() => {
    const selection = window.getSelection()
    const body = bodyRef.current?.querySelector('.chat-agent-message')
    const text = selection && body && selection.rangeCount && !selection.isCollapsed
      && body.contains(selection.anchorNode) && body.contains(selection.focusNode) ? selection.toString().trim() : ''
    setSelected(text)
  }, [])
  useEffect(() => {
    document.addEventListener('selectionchange', captureSelection)
    return () => document.removeEventListener('selectionchange', captureSelection)
  }, [captureSelection])

  const open = async (mode: AdoptionMode) => {
    if (!prose || busy) return
    setBusy(true)
    setFailure(null)
    try {
      const destination = mode === 'replace' ? effectiveTarget : await bridge?.editor?.capture('insert')
      if (!destination) { toast.error(t('editor.adoption.unavailable')); return }
      setPreview({ mode, target: destination, text: prose })
    } catch (error) {
      console.warn('[writing/reply-adoption] Could not prepare manuscript target', { mode, error })
      toast.error(t('editor.adoption.changed'))
    } finally { setBusy(false) }
  }

  const useCursor = async () => {
    if (!preview || !bridge?.editor) return
    setBusy(true)
    try {
      const destination = await bridge.editor.capture('insert')
      setPreview({ mode: 'insert', target: destination, text: preview.text })
      setFailure(null)
    } catch (error) {
      console.warn('[writing/reply-adoption] Could not prepare cursor fallback', { error })
      setFailure('unavailable')
    } finally { setBusy(false) }
  }

  const adopt = async () => {
    if (!preview || busy) return
    const editor = bridge?.editor
    if (!editor || editor.projectId !== preview.target.projectId || editor.fileName !== preview.target.fileName) {
      setFailure('unavailable')
      return
    }
    setBusy(true)
    try {
      const result = await editor.apply(preview.target, preview.text, preview.mode)
      if (result) {
        console.warn('[writing/reply-adoption] Adoption blocked', { path: preview.target.fileName, reason: result })
        setFailure(result)
        return
      }
      console.info('[writing/reply-adoption] Adopted reply prose', { path: preview.target.fileName, mode: preview.mode })
      toast.success(t('editor.adoption.done'))
      setPreview(null)
    } catch (error) {
      console.warn('[writing/reply-adoption] Adoption failed', { error })
      setFailure('saveFailed')
    } finally { setBusy(false) }
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(preview?.text || prose)
      toast.success(t('editor.adoption.copied'))
    } catch (error) {
      console.warn('[writing/reply-adoption] Copy failed', { error })
      toast.error(t('editor.adoption.copyFailed'))
    }
  }

  return <>
    <div ref={bodyRef} onPointerUp={captureSelection} onKeyUp={captureSelection}>{children}</div>
    {/* Ordinary replies stay uncluttered: the row appears once there is adoptable
        prose (an author selection or a marked block) or the reply answers a
        selection action, where the hint explains how to pick text. */}
    {prose || effectiveTarget ? <div className="mt-1 flex flex-wrap items-center gap-1" onPointerDown={event => event.preventDefault()}>
      <Button size="xs" variant="ghost" disabled={!prose || !bridge?.editor || busy} onClick={() => void open('insert')}><TextCursorInput />{t('editor.adoption.insert')}</Button>
      {effectiveTarget ? <Button size="xs" variant="ghost" disabled={!prose || busy} onClick={() => void open('replace')}><Replace />{t('editor.adoption.replace')}</Button> : null}
      <span className="basis-full text-[11px] text-muted-foreground">{t(selected ? 'editor.adoption.selected' : prose ? 'editor.adoption.proseBlock' : 'editor.adoption.selectHint')}</span>
    </div> : null}
    <Dialog open={Boolean(preview)} onOpenChange={open => { if (!open && !busy) setPreview(null) }}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t('editor.adoption.preview')}</DialogTitle>
          <DialogDescription>{t('editor.adoption.previewHint')}</DialogDescription>
        </DialogHeader>
        {preview ? <>
          <div className="min-w-0 space-y-2 text-sm">
            <p className="break-all font-medium">{preview.target.fileName}</p>
            <p>{t(preview.mode === 'replace' ? 'editor.adoption.replaceRange' : 'editor.adoption.insertRange', { start: preview.target.quote.startLine, end: preview.target.quote.endLine })}</p>
            {preview.mode === 'replace' ? <pre className="max-h-28 overflow-y-auto whitespace-pre-wrap break-words rounded border p-2 text-muted-foreground">{preview.target.quote.content}</pre> : null}
            <p className="font-medium">{t('editor.adoption.content')}</p>
            <pre className="max-h-56 overflow-y-auto whitespace-pre-wrap break-words rounded border p-2">{preview.text}</pre>
          </div>
          {failure ? <p role="alert" className="text-sm text-destructive">{t(`editor.adoption.${failure}`)}</p> : null}
          <DialogFooter className="flex-wrap">
            <Button variant="outline" onClick={() => void copy()}><Clipboard />{t('editor.adoption.copy')}</Button>
            {failure && bridge?.editor ? <Button variant="outline" disabled={busy} onClick={() => void useCursor()}>{t('editor.adoption.useCursor')}</Button> : null}
            <Button disabled={busy || Boolean(failure)} onClick={() => void adopt()}>{t('editor.adoption.confirm')}</Button>
          </DialogFooter>
        </> : null}
      </DialogContent>
    </Dialog>
  </>
}

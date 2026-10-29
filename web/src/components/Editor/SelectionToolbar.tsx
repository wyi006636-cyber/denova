import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { Editor } from '@tiptap/react'
import { MessageCirclePlus, Quote, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { writingSelectionActions, type WritingSelectionAction } from '@/features/writing/selection-actions'

/**
 * 选区浮动工具条，定位在光标（选区 head 端）旁边。
 *
 * ProseMirror 在失焦后仍会保留内部选区；只有编辑器保持焦点时，这个选区才是用户
 * 当前可见、可操作的选区。工具条自身阻止 pointerdown 抢走焦点，确保点击操作仍能
 * 使用原选区。
 */
export function SelectionToolbar({ editor, mode = 'quote', onAction, onQuote, onWritingAction }: {
  editor: Editor; mode?: 'quote' | 'comment'; onAction: () => void
  onQuote?: () => void
  onWritingAction?: (action: WritingSelectionAction) => void | Promise<void>
}) {
  const { t } = useTranslation()
  const [coords, setCoords] = useState<{ top: number; left: number; maxWidth: number } | null>(null)
  const toolbarRef = useRef<HTMLDivElement>(null)
  const [pending, setPending] = useState(false)

  const updatePosition = useCallback(() => {
    const { from, to, head } = editor.state.selection
    if (!editor.isFocused || from === to) {
      setCoords(null)
      return
    }
    try {
      const headCoords = editor.view.coordsAtPos(head)
      const containerEl = editor.view.dom.closest('.relative') as HTMLElement | null
      if (!containerEl) { setCoords(null); return }

      const containerRect = containerEl.getBoundingClientRect()
      const scrollTop = containerEl.scrollTop
      // A relative ancestor can be wider than the actual browser viewport (for
      // example while a mobile pane is being resized). Use the intersection
      // with the viewport as the clipping rectangle for the floating toolbar.
      const visibleLeft = Math.max(containerRect.left, 0)
      const visibleRight = Math.min(containerRect.right, window.innerWidth)
      const visibleTop = Math.max(containerRect.top, 0)
      const visibleBottom = Math.min(containerRect.bottom, window.innerHeight)
      const availableWidth = Math.max(0, visibleRight - visibleLeft - 8)
      const toolbarWidth = Math.min(toolbarRef.current?.offsetWidth ?? availableWidth, availableWidth)

      // coordsAtPos returns viewport coordinates, while absolute positioning is
      // relative to the scrolling surface; convert before clamping to its
      // visible intersection.
      const minLeft = visibleLeft - containerRect.left + 4
      const maxLeft = visibleRight - containerRect.left - toolbarWidth - 4
      const preferredLeft = headCoords.left - containerRect.left
      const left = Math.min(Math.max(preferredLeft, minLeft), Math.max(minLeft, maxLeft))

      const toolbarHeight = toolbarRef.current?.offsetHeight ?? 32
      const visibleTopInContent = Math.max(scrollTop, scrollTop + visibleTop - containerRect.top)
      const visibleBottomInContent = scrollTop + visibleBottom - containerRect.top
      const minTop = visibleTopInContent + 4
      const maxTop = visibleBottomInContent - toolbarHeight - 4
      let top = headCoords.bottom - containerRect.top + scrollTop + 4
      if (top > maxTop) {
        top = headCoords.top - containerRect.top + scrollTop - toolbarHeight - 4
      }
      // Keep the toolbar in the visible vertical range when there is enough
      // room. If a host is shorter than the wrapped toolbar, minTop is the
      // only honest placement and the surface itself remains scrollable.
      if (maxTop >= minTop) top = Math.min(Math.max(top, minTop), maxTop)
      else top = minTop

      const next = { top, left, maxWidth: availableWidth }
      setCoords((previous) => (
        previous && previous.top === next.top && previous.left === next.left && previous.maxWidth === next.maxWidth
          ? previous
          : next
      ))
    } catch {
      setCoords(null)
    }
  }, [editor])

  useEffect(() => {
    updatePosition()
    editor.on('focus', updatePosition)
    editor.on('blur', updatePosition)
    editor.on('selectionUpdate', updatePosition)
    editor.on('update', updatePosition)
    // Re-measure when the surface changes size and when the viewport or scroll position changes.
    const observer = new ResizeObserver(updatePosition)
    const surface = editor.view.dom.closest('.relative')
    if (surface) observer.observe(surface)
    window.addEventListener('resize', updatePosition)
    window.addEventListener('scroll', updatePosition, true)
    return () => {
      editor.off('focus', updatePosition)
      editor.off('blur', updatePosition)
      editor.off('selectionUpdate', updatePosition)
      editor.off('update', updatePosition)
      observer.disconnect()
      window.removeEventListener('resize', updatePosition)
      window.removeEventListener('scroll', updatePosition, true)
    }
  }, [editor])

  useLayoutEffect(() => {
    if (!coords || !toolbarRef.current) return
    // The first position pass happens before this toolbar exists. Observe the
    // mounted element and remeasure after wrapping so the measured width and
    // height are the values used for both horizontal and vertical clamping.
    const observer = new ResizeObserver(updatePosition)
    observer.observe(toolbarRef.current)
    const frame = requestAnimationFrame(updatePosition)
    return () => {
      observer.disconnect()
      cancelAnimationFrame(frame)
    }
  }, [coords, updatePosition])

  if (!coords) return null

  return (
    // Keep every action available on narrow screens: wrapping the existing
    // buttons preserves one-tap access without hiding secondary actions in a menu.
    <div
      ref={toolbarRef}
      role="toolbar"
      aria-label={t('editor.selection.actions')}
      className="absolute z-30 box-border flex w-max min-w-0 max-w-[24rem] flex-wrap items-center gap-1 rounded-md border border-[var(--nova-border)] bg-[var(--nova-menu-bg)] px-1.5 py-1 shadow-xl"
      style={{ top: coords.top, left: coords.left, maxWidth: coords.maxWidth }}
      onPointerDown={(event) => event.preventDefault()}
    >
      <Button
        type="button"
        variant="ghost" size="xs" disabled={pending}
        onClick={onAction}
      >
        {mode === 'comment' ? <MessageCirclePlus className="h-3.5 w-3.5" /> : <Quote className="h-3.5 w-3.5" />}
        <span>{t(mode === 'comment' ? 'editor.review.addComment' : 'editor.quoteSelection')}</span>
      </Button>
      {onQuote ? <Button type="button" variant="ghost" size="xs" disabled={pending} onClick={onQuote}>
        <Quote />{t('editor.quoteSelection')}
      </Button> : null}
      {onWritingAction ? writingSelectionActions.map(action => <Button
        key={action} type="button" variant="ghost" size="xs" disabled={pending}
        onClick={async () => {
          setPending(true)
          try { await onWritingAction(action) } finally { setPending(false) }
        }}
      >
        {action === 'polish' ? <Sparkles /> : null}{t(`editor.selection.${action}`)}
      </Button>) : null}
    </div>
  )
}

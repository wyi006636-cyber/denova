import { useEffect, useRef, useState } from 'react'
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
  const [coords, setCoords] = useState<{ top: number; left: number } | null>(null)
  const toolbarRef = useRef<HTMLDivElement>(null)
  const [pending, setPending] = useState(false)

  useEffect(() => {
    const updatePosition = () => {
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
        const toolbarWidth = toolbarRef.current?.offsetWidth ?? 100
        // coordsAtPos 返回视口坐标，需加上 scrollTop 转换为容器内容区域坐标
        let top = headCoords.bottom - containerRect.top + scrollTop + 4
        let left = headCoords.left - containerRect.left
        // 防止溢出右侧
        const maxLeft = containerRect.width - toolbarWidth - 8
        if (left > maxLeft) left = maxLeft
        if (left < 4) left = 4
        // 如果下方空间不够（相对当前可见区域），改为显示在光标行上方
        const toolbarHeight = toolbarRef.current?.offsetHeight ?? 32
        const visibleBottom = scrollTop + containerRect.height
        if (top + toolbarHeight > visibleBottom) {
          top = headCoords.top - containerRect.top + scrollTop - toolbarHeight - 4
        }
        setCoords({ top: Math.max(scrollTop, top), left })
      } catch {
        setCoords(null)
      }
    }
    updatePosition()
    editor.on('focus', updatePosition)
    editor.on('blur', updatePosition)
    editor.on('selectionUpdate', updatePosition)
    editor.on('update', updatePosition)
    // Re-measure after the wrapped toolbar mounts and when the viewport or scroll position changes.
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

  if (!coords) return null

  return (
    <div
      ref={toolbarRef}
      role="toolbar"
      aria-label={t('editor.selection.actions')}
      className="absolute z-30 flex w-max max-w-[min(24rem,calc(100%-0.5rem))] flex-wrap items-center gap-1 rounded-md border border-[var(--nova-border)] bg-[var(--nova-menu-bg)] px-1.5 py-1 shadow-xl"
      style={{ top: coords.top, left: coords.left }}
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

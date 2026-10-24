import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import type { AdoptionFailure, AdoptionMode, WritingSelectionTarget } from './selection-actions'

/** The foreground manuscript owns every read, conflict check, edit and undo transaction. */
export interface WritingAdoptionEditor {
  projectId: string
  fileName: string
  capture: (mode: AdoptionMode) => Promise<WritingSelectionTarget>
  apply: (target: WritingSelectionTarget, text: string, mode: AdoptionMode) => Promise<AdoptionFailure | null>
}

interface WritingSelectionBridge {
  editor: WritingAdoptionEditor | null
  register: (editor: WritingAdoptionEditor) => () => void
  latestActionTarget: WritingSelectionTarget | null
  setLatestActionTarget: (target: WritingSelectionTarget | null) => void
}
const WritingSelectionContext = createContext<WritingSelectionBridge | null>(null)

export function WritingSelectionProvider({ projectId, fileName, enabled, children }: {
  projectId: string; fileName: string | null; enabled: boolean; children: ReactNode
}) {
  const [editor, setEditor] = useState<WritingAdoptionEditor | null>(null)
  const [latestActionTarget, setLatestActionTarget] = useState<WritingSelectionTarget | null>(null)
  useEffect(() => {
    setLatestActionTarget(current => current && enabled && current.projectId === projectId && current.fileName === fileName ? current : null)
  }, [enabled, fileName, projectId])
  const register = useCallback((next: WritingAdoptionEditor) => {
    setEditor(next)
    return () => setEditor(current => current === next ? null : current)
  }, [])
  const value = useMemo(() => ({
    editor: enabled && editor?.projectId === projectId && editor.fileName === fileName ? editor : null,
    register,
    latestActionTarget: enabled && latestActionTarget?.projectId === projectId && latestActionTarget.fileName === fileName ? latestActionTarget : null,
    setLatestActionTarget,
  }), [editor, enabled, fileName, latestActionTarget, projectId, register])
  return <WritingSelectionContext.Provider value={value}>{children}</WritingSelectionContext.Provider>
}

export function useWritingSelection() { return useContext(WritingSelectionContext) }

/** Scoped to a Writing conversation, so shared Game and configuration messages have no adoption UI. */
export const WritingReplyContext = createContext<ReadonlyMap<string, WritingSelectionTarget> | null>(null)

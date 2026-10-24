import { useEffect } from 'react'
import { create } from 'zustand'

export interface ExecutableDraftEntry {
  hasPending: boolean
  discard: () => void | Promise<void>
}

interface ExecutableDraftGuardState {
  entries: Record<string, ExecutableDraftEntry>
}

/**
 * Shared registry for executable configuration drafts. Pages own their
 * autosave lane and baseline; this registry only exposes pending state and the
 * page-owned discard operation to the workbench navigation guard.
 */
export const useExecutableDraftGuard = create<ExecutableDraftGuardState>(() => ({ entries: {} }))

export function registerExecutableDraft(key: string, entry: ExecutableDraftEntry): void {
  useExecutableDraftGuard.setState((state) => ({ entries: { ...state.entries, [key]: entry } }))
}

export function unregisterExecutableDraft(key: string): void {
  useExecutableDraftGuard.setState((state) => {
    const entries = { ...state.entries }
    delete entries[key]
    return { entries }
  })
}

export function hasPendingExecutableDraft(key: string): boolean {
  return Boolean(useExecutableDraftGuard.getState().entries[key]?.hasPending)
}

export function discardExecutableDraft(key: string): void | Promise<void> | undefined {
  return useExecutableDraftGuard.getState().entries[key]?.discard?.()
}

export function useExecutableDraftEntry(
  key: string,
  hasPending: boolean,
  discard: () => void | Promise<void>,
): void {
  useEffect(() => {
    registerExecutableDraft(key, { hasPending, discard })
    return () => unregisterExecutableDraft(key)
  }, [discard, hasPending, key])

  useEffect(() => {
    if (!hasPending || typeof window === 'undefined') return
    const preventDraftLoss = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', preventDraftLoss)
    return () => window.removeEventListener('beforeunload', preventDraftLoss)
  }, [hasPending])
}

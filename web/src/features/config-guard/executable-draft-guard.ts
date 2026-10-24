import { useEffect } from 'react'
import { create } from 'zustand'

export interface ExecutableDraftFlushFailure {
  ok: false
  error?: unknown
}

export interface ExecutableDraftFlushSuccess {
  ok: true
}

export type ExecutableDraftFlushResult =
  | void
  | boolean
  | ExecutableDraftFlushSuccess
  | ExecutableDraftFlushFailure

export interface ExecutableDraftEntry {
  /** Flushes a pending save and waits for an in-flight save to settle. */
  flush: () => ExecutableDraftFlushResult | Promise<ExecutableDraftFlushResult>
  /** Reads the current draft state at navigation time. */
  isDirty: () => boolean
  /** Reads whether a save is currently in flight for beforeunload handling. */
  isSaving?: () => boolean
  /** Allows a page to expose its existing save/validation error to the dialog. */
  getError?: () => unknown
  /** Returns whether beforeunload should synchronously show the browser prompt. */
  shouldPromptBeforeUnload?: () => boolean
  discard: () => void | Promise<void>
}

interface ExecutableDraftGuardState {
  entries: Record<string, ExecutableDraftEntry>
}

/** Shared registry for executable configuration drafts. Pages own their
 * autosave lane and baseline; this registry only coordinates flush/discard
 * callbacks with workbench navigation.
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

export function getExecutableDraftEntry(key: string): ExecutableDraftEntry | undefined {
  return useExecutableDraftGuard.getState().entries[key]
}

/** Reads the live page callback instead of a boolean captured during render. */
export function hasPendingExecutableDraft(key: string): boolean {
  const entry = getExecutableDraftEntry(key)
  return Boolean(entry?.isDirty())
}

export async function flushExecutableDraft(key: string): Promise<{ ok: true } | { ok: false; error?: unknown }> {
  const entry = getExecutableDraftEntry(key)
  if (!entry || !entry.isDirty()) return { ok: true }
  try {
    const result = await entry.flush()
    if (result === false) return { ok: false, error: entry.getError?.() }
    if (typeof result === 'object' && result !== null && 'ok' in result && result.ok === false) {
      return { ok: false, error: result.error ?? entry.getError?.() }
    }
    return { ok: true }
  } catch (error) {
    return { ok: false, error: error ?? entry.getError?.() }
  }
}

export function discardExecutableDraft(key: string): void | Promise<void> {
  return getExecutableDraftEntry(key)?.discard?.()
}

export function useExecutableDraftEntry(key: string, entry: ExecutableDraftEntry): void {
  useEffect(() => {
    registerExecutableDraft(key, entry)
    return () => unregisterExecutableDraft(key)
  }, [entry, key])

  useEffect(() => {
    if (typeof window === 'undefined') return
    const preventDraftLoss = (event: BeforeUnloadEvent) => {
      const current = getExecutableDraftEntry(key)
      if (!current || !current.isDirty()) return

      // A browser cannot await this promise. Trigger the same page-owned flush
      // once, then only block when the page says the draft cannot be safely
      // persisted synchronously (or a previous save already failed).
      try {
        void Promise.resolve(current.flush()).catch((error) => {
          console.error(`[config-guard] beforeunload flush failed for ${key}`, error)
        })
      } catch (error) {
        console.error(`[config-guard] beforeunload flush threw for ${key}`, error)
      }

      const shouldPrompt = current.shouldPromptBeforeUnload
        ? current.shouldPromptBeforeUnload()
        : !current.isSaving?.()
      if (!shouldPrompt) return
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', preventDraftLoss)
    return () => window.removeEventListener('beforeunload', preventDraftLoss)
  }, [key])
}

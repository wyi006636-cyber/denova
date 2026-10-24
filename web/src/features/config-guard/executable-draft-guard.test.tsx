import { renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  discardExecutableDraft,
  flushExecutableDraft,
  hasPendingExecutableDraft,
  registerExecutableDraft,
  unregisterExecutableDraft,
  useExecutableDraftEntry,
} from './executable-draft-guard'

describe('executable draft guard registry', () => {
  afterEach(() => {
    unregisterExecutableDraft('test')
  })

  it('flushes a debounce-period edit before navigation without opening a discard prompt', async () => {
    let dirty = true
    const flush = vi.fn(async () => {
      dirty = false
      return true
    })
    registerExecutableDraft('test', {
      flush,
      isDirty: () => dirty,
      shouldPromptBeforeUnload: () => false,
      discard: vi.fn(),
    })

    expect(hasPendingExecutableDraft('test')).toBe(true)
    await expect(flushExecutableDraft('test')).resolves.toEqual({ ok: true })
    expect(flush).toHaveBeenCalledTimes(1)
    expect(hasPendingExecutableDraft('test')).toBe(false)
  })

  it('reads dirty state through a live callback instead of a render-time snapshot', () => {
    let dirty = false
    registerExecutableDraft('test', {
      flush: vi.fn(),
      isDirty: () => dirty,
      discard: vi.fn(),
    })

    expect(hasPendingExecutableDraft('test')).toBe(false)
    dirty = true
    expect(hasPendingExecutableDraft('test')).toBe(true)
  })

  it('returns the flush failure so navigation can show the existing save error', async () => {
    const failure = new Error('revision conflict')
    registerExecutableDraft('test', {
      flush: vi.fn(async () => ({ ok: false as const, error: failure })),
      isDirty: () => true,
      getError: () => failure,
      shouldPromptBeforeUnload: () => true,
      discard: vi.fn(),
    })

    await expect(flushExecutableDraft('test')).resolves.toEqual({ ok: false, error: failure })
  })

  it('does not block beforeunload for a pending autosave, but still triggers one best-effort flush', () => {
    const flush = vi.fn(async () => true)
    const view = renderHook(() => useExecutableDraftEntry('test', {
      flush,
      isDirty: () => true,
      isSaving: () => false,
      shouldPromptBeforeUnload: () => false,
      discard: vi.fn(),
    }))

    const pendingEvent = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(pendingEvent)
    expect(pendingEvent.defaultPrevented).toBe(false)
    expect(flush).toHaveBeenCalledTimes(1)
    view.unmount()
  })

  it('blocks beforeunload for a draft that cannot be saved or has already failed', () => {
    const view = renderHook(() => useExecutableDraftEntry('test', {
      flush: vi.fn(async () => false),
      isDirty: () => true,
      isSaving: () => false,
      shouldPromptBeforeUnload: () => true,
      discard: vi.fn(),
    }))

    const failedEvent = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(failedEvent)
    expect(failedEvent.defaultPrevented).toBe(true)
    view.unmount()
  })

  it('propagates discard failures for the workbench to report and keep navigation blocked', async () => {
    const failure = new Error('reload failed')
    registerExecutableDraft('test', {
      flush: vi.fn(),
      isDirty: () => true,
      discard: vi.fn(async () => { throw failure }),
    })

    await expect(discardExecutableDraft('test')).rejects.toThrow('reload failed')
  })
})

import { renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  discardExecutableDraft,
  hasPendingExecutableDraft,
  registerExecutableDraft,
  unregisterExecutableDraft,
  useExecutableDraftEntry,
} from './executable-draft-guard'

describe('executable draft guard registry', () => {
  afterEach(() => {
    unregisterExecutableDraft('test')
  })

  it('delegates discard to the page-owned autosave baseline', async () => {
    const discard = vi.fn(async () => undefined)
    registerExecutableDraft('test', { hasPending: true, discard })

    expect(hasPendingExecutableDraft('test')).toBe(true)
    await discardExecutableDraft('test')
    expect(discard).toHaveBeenCalledTimes(1)
  })

  it('uses the standard beforeunload signal only while a draft is pending', () => {
    const discard = vi.fn()
    const view = renderHook(({ pending }: { pending: boolean }) => useExecutableDraftEntry('test', pending, discard), {
      initialProps: { pending: true },
    })

    const pendingEvent = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(pendingEvent)
    expect(pendingEvent.defaultPrevented).toBe(true)

    view.rerender({ pending: false })
    const cleanEvent = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(cleanEvent)
    expect(cleanEvent.defaultPrevented).toBe(false)
    view.unmount()
  })
})

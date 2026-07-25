import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useWorkspaceStore } from './workspace-store'

describe('useWorkspaceStore', () => {
  beforeEach(() => {
    window.localStorage.clear()
    useWorkspaceStore.setState({
      mode: 'ide',
      selectedProjectId: undefined,
      selectedChapterId: undefined,
      rightPanel: 'ai',
      bottomPanel: null,
      commandOpen: false,
    })
  })

  it('updates selectedChapterId', () => {
    useWorkspaceStore.getState().setSelectedChapterId('chapters/ch01.md')

    expect(useWorkspaceStore.getState().selectedChapterId).toBe('chapters/ch01.md')
  })

  it('keeps the bottom panel closed by default', () => {
    expect(useWorkspaceStore.getInitialState().bottomPanel).toBeNull()
  })

  it('persists the visible top-level mode and writing-side panel', () => {
    useWorkspaceStore.getState().setMode('interactive')
    useWorkspaceStore.getState().setMode('agents')
    useWorkspaceStore.getState().setRightPanel('versions')

    expect(window.localStorage.getItem('nova:mode')).toBe('agents')
    expect(window.localStorage.getItem('nova:content-mode')).toBe('interactive')
    expect(window.localStorage.getItem('nova:right-panel')).toBe('versions')

    useWorkspaceStore.getState().setRightPanel(null)
    expect(window.localStorage.getItem('nova:right-panel')).toBeNull()
  })

  it('changes content mode only through explicit writing and game mode actions', () => {
    useWorkspaceStore.getState().setMode('interactive')

    for (const sharedMode of ['quality', 'skills', 'agents', 'automations'] as const) {
      useWorkspaceStore.getState().setMode(sharedMode)
      expect(window.localStorage.getItem('nova:content-mode')).toBe('interactive')
    }

    useWorkspaceStore.getState().setMode('ide')
    expect(window.localStorage.getItem('nova:content-mode')).toBe('ide')
    useWorkspaceStore.getState().setMode('interactive')
    expect(window.localStorage.getItem('nova:content-mode')).toBe('interactive')
  })

  it('restores a shared visible mode separately from its content return mode', async () => {
    window.localStorage.setItem('nova:mode', 'quality')
    window.localStorage.setItem('nova:content-mode', 'interactive')
    vi.resetModules()
    const { useWorkspaceStore: reloadedStore } = await import('./workspace-store')

    expect(reloadedStore.getInitialState().mode).toBe('quality')
    expect(window.localStorage.getItem('nova:content-mode')).toBe('interactive')
  })

  it('migrates the legacy change-review right panel back to the Agent panel', async () => {
    window.localStorage.setItem('nova:right-panel', 'review')
    vi.resetModules()
    const { useWorkspaceStore: reloadedStore } = await import('./workspace-store')

    expect(reloadedStore.getInitialState().rightPanel).toBe('ai')
  })
})

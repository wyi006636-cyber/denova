import type { ReactNode } from 'react'
import { fireEvent, render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import App from '@/App'
import { useWorkspaceStore } from '@/stores/workspace-store'
import { WorkbenchShell } from './WorkbenchShell'

const responsiveState = vi.hoisted(() => ({ mobile: false }))

vi.mock('@/hooks/useIsMobile', () => ({
  useIsMobile: () => responsiveState.mobile,
}))

vi.mock('@/components/layout/workspace-layout', () => ({
  WorkspaceLayout: ({ topBar, activityBar, main }: { topBar: ReactNode; activityBar: ReactNode; main: ReactNode }) => (
    <section data-testid="quality-desktop-shell">{topBar}{activityBar}{main}</section>
  ),
}))

vi.mock('@/components/layout/workspace-mobile-layout', () => ({
  WorkspaceMobileLayout: ({ topBar, main, activityItems }: {
    topBar: ReactNode
    main: ReactNode
    activityItems: Array<{ id: string; label: string; active: boolean; onClick: () => void }>
  }) => (
    <section data-testid="quality-mobile-shell">
      {topBar}
      <nav>
        {activityItems.map((item) => (
          <button
            key={item.id}
            type="button"
            data-mobile-activity-id={item.id}
            aria-label={item.label}
            aria-current={item.active ? 'page' : undefined}
            onClick={item.onClick}
          />
        ))}
      </nav>
      {main}
    </section>
  ),
}))

vi.mock('@/features/messages/MessageCenter', () => ({
  MessageCenterButton: () => null,
}))

vi.mock('@/lib/api', () => ({
  getAutomationInbox: vi.fn(() => new Promise<never>(() => {})),
  getActiveAutomationRuns: vi.fn(() => new Promise<never>(() => {})),
  getLoreItems: vi.fn().mockResolvedValue([]),
  importCharacterCard: vi.fn(),
  previewCharacterCard: vi.fn(),
  setChapterConfirmed: vi.fn(),
  switchWorkspace: vi.fn(),
}))

vi.mock('@/features/settings/api', () => ({
  checkForUpdate: vi.fn(),
  fetchSettings: vi.fn(() => new Promise<never>(() => {})),
}))

vi.mock('@/hooks/useWorkspace', () => ({
  useWorkspace: () => ({
    tree: [],
    loading: false,
    selectedFile: null,
    fileContent: '',
    workspace: '',
    workspaceLoaded: false,
    summary: null,
    books: [],
    bookSortMode: 'recent',
    selectFile: vi.fn(),
    clearSelectedFile: vi.fn(),
    saveFileContent: vi.fn(),
    createItem: vi.fn(),
    deleteItem: vi.fn(),
    renameItem: vi.fn(),
    copyItem: vi.fn(),
    moveItem: vi.fn(),
    refresh: vi.fn(),
    refreshSummary: vi.fn(),
    refreshAfterAgentFileChange: vi.fn(),
    refreshAll: vi.fn(),
    refreshBooks: vi.fn(),
    setWorkspace: vi.fn(),
  }),
}))

vi.mock('@/hooks/useAgentChat', () => ({
  useAgentChat: () => ({
    messages: [],
    sessions: [],
    activeSessionId: '',
    isStreaming: false,
    activityContent: '',
    references: [],
    loreReferences: [],
    styleScenes: [],
    textSelections: [],
    planMode: false,
    setPlanMode: vi.fn(),
    togglePlanMode: vi.fn(),
    send: vi.fn(),
    analyzeContext: vi.fn(),
    submitPlanQuestion: vi.fn(),
    approveProposedPlan: vi.fn(),
    exitPlanMode: vi.fn(),
    stop: vi.fn(),
    loadSessions: vi.fn(),
    loadHistory: vi.fn(),
    resumeActiveChat: vi.fn(),
    createChatSession: vi.fn(),
    switchChatSession: vi.fn(),
    renameChatSession: vi.fn(),
    deleteChatSession: vi.fn(),
    addReference: vi.fn(),
    removeReference: vi.fn(),
    addLoreReference: vi.fn(),
    removeLoreReference: vi.fn(),
    addStyleScene: vi.fn(),
    removeStyleScene: vi.fn(),
    addTextSelection: vi.fn(),
    removeTextSelection: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-workspace-hotkeys', () => ({
  useWorkspaceHotkeys: vi.fn(),
}))

vi.mock('@/components/workbench/ModeRouter', () => ({
  ModeRouter: ({ mode, booksReturnMode, onSetMode }: {
    mode: string
    booksReturnMode: 'ide' | 'interactive'
    onSetMode: (mode: 'ide' | 'interactive') => void
  }) => (
    <section data-testid="quality-app-mode-router" data-mode={mode} data-return-mode={booksReturnMode}>
      <button type="button" data-testid="quality-app-return" onClick={() => onSetMode(booksReturnMode)}>
        return
      </button>
    </section>
  ),
}))

vi.mock('@/features/motion/motion-preferences', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/features/motion/motion-preferences')>()
  return {
    ...original,
    NovaMotionProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  }
})

vi.mock('@/components/common/command-palette', () => ({
  CommandPalette: () => null,
}))

vi.mock('@/components/workbench/CharacterCardImportDialog', () => ({
  CharacterCardImportDialog: () => null,
}))

vi.mock('@/components/RemoteAccessLogin', () => ({
  RemoteAccessLogin: () => null,
}))

vi.mock('@/features/onboarding/OnboardingGuide', () => ({
  OnboardingGuide: () => null,
}))

describe('Quality Harness App refresh restoration boundary', () => {
  it.each([
    ['ide', 'quality'],
    ['interactive', 'quality'],
  ] as const)('restores the %s return mode through App while the %s shared view is visible', (contentMode, visibleMode) => {
    window.localStorage.clear()
    window.localStorage.setItem('nova:content-mode', contentMode)
    useWorkspaceStore.setState({
      mode: visibleMode,
      selectedProjectId: undefined,
      selectedChapterId: undefined,
      rightPanel: null,
      bottomPanel: null,
      commandOpen: false,
    })

    render(<App />)

    const router = document.querySelector<HTMLElement>('[data-testid="quality-app-mode-router"]')
    expect(router).toHaveAttribute('data-mode', 'quality')
    expect(router).toHaveAttribute('data-return-mode', contentMode)

    fireEvent.click(document.querySelector<HTMLButtonElement>('[data-testid="quality-app-return"]')!)
    expect(useWorkspaceStore.getState().mode).toBe(contentMode)
    expect(window.localStorage.getItem('nova:content-mode')).toBe(contentMode)
  })
})

describe('Quality Harness shared navigation safety boundaries', () => {
  beforeEach(() => {
    responsiveState.mobile = false
    window.localStorage.clear()
    window.localStorage.setItem('nova:content-mode', 'interactive')
    useWorkspaceStore.setState({
      mode: 'interactive',
      selectedProjectId: undefined,
      selectedChapterId: undefined,
      rightPanel: null,
      bottomPanel: null,
      commandOpen: false,
    })
  })

  it('preserves content mode across every shared desktop menu and changes it only through mode buttons', () => {
    render(<QualityHarnessNavigation />)
    expectDesktopActive('story')

    for (const id of ['quality', 'skills', 'agents', 'automations'] as const) {
      clickDesktopActivity(id)
      expect(window.localStorage.getItem('nova:content-mode')).toBe('interactive')
      expectDesktopActive(id)
    }

    clickDesktopActivity('versions')
    expect(window.localStorage.getItem('nova:content-mode')).toBe('interactive')
    expectDesktopActive('versions')

    clickDesktopActivity('versions')
    expectDesktopActive('story')

    clickDesktopActivity('skills')
    clickDesktopActivity('skills')
    expect(useWorkspaceStore.getState().mode).toBe('interactive')
    expectDesktopActive('story')

    clickExplicitMode('ide')
    expect(window.localStorage.getItem('nova:content-mode')).toBe('ide')
    expectDesktopActive('writing')

    clickExplicitMode('interactive')
    expect(window.localStorage.getItem('nova:content-mode')).toBe('interactive')
    expectDesktopActive('story')
  })

  it.each(['ide', 'interactive'] as const)('enters and exits Quality from %s without changing content mode', (contentMode) => {
    window.localStorage.setItem('nova:content-mode', contentMode)
    useWorkspaceStore.setState({ mode: contentMode, rightPanel: null })
    render(<QualityHarnessNavigation />)

    clickDesktopActivity('quality')
    expect(useWorkspaceStore.getState().mode).toBe('quality')
    expect(window.localStorage.getItem('nova:content-mode')).toBe(contentMode)
    expectDesktopActive('quality')

    clickDesktopActivity('quality')
    expect(useWorkspaceStore.getState().mode).toBe(contentMode)
    expect(window.localStorage.getItem('nova:content-mode')).toBe(contentMode)
    expectDesktopActive(contentMode === 'ide' ? 'writing' : 'story')
  })

  it('keeps exactly one mobile menu active and returns from a restored shared menu', () => {
    responsiveState.mobile = true
    useWorkspaceStore.setState({ mode: 'agents', rightPanel: null })
    render(<QualityHarnessNavigation />)

    expect(mobileActiveIDs()).toEqual(['agents'])
    expectPressedMode('interactive')

    clickMobileActivity('skills')
    expect(window.localStorage.getItem('nova:content-mode')).toBe('interactive')
    expect(mobileActiveIDs()).toEqual(['skills'])

    clickMobileActivity('skills')
    expect(useWorkspaceStore.getState().mode).toBe('interactive')
    expect(mobileActiveIDs()).toEqual(['story'])
    expectPressedMode('interactive')

    clickMobileActivity('quality')
    expect(window.localStorage.getItem('nova:content-mode')).toBe('interactive')
    expect(mobileActiveIDs()).toEqual(['quality'])
  })
})

function QualityHarnessNavigation() {
  const mode = useWorkspaceStore((state) => state.mode)
  const rightPanel = useWorkspaceStore((state) => state.rightPanel)
  const setMode = useWorkspaceStore((state) => state.setMode)
  const setRightPanel = useWorkspaceStore((state) => state.setRightPanel)
  const booksReturnMode = window.localStorage.getItem('nova:content-mode') === 'interactive' ? 'interactive' : 'ide'
  return (
    <WorkbenchShell
      mode={mode}
      booksReturnMode={booksReturnMode}
      currentBookName="Quality Harness"
      workspace="C:/quality-harness"
      books={[]}
      appVersion="test"
      summary={null}
      isStreaming={false}
      projectVisible={false}
      activityBarExpanded
      rightPanel={rightPanel}
      settingsOpen={false}
      interactiveSubmode="story"
      sidebar={null}
      main={<div>workspace</div>}
      rightPanelContent={null}
      onSetMode={setMode}
      onToggleActivityBarExpanded={vi.fn()}
      onSetInteractiveSubmode={vi.fn()}
      onSetRightPanel={setRightPanel}
      onToggleSettings={vi.fn()}
      onCloseSettings={vi.fn()}
      onQuickSwitchBook={vi.fn().mockResolvedValue(true)}
    />
  )
}

function clickDesktopActivity(id: string) {
  const button = document.querySelector<HTMLButtonElement>(`[data-activity-id="${id}"]`)
  expect(button).not.toBeNull()
  fireEvent.click(button!)
}

function expectDesktopActive(id: string) {
  const active = Array.from(document.querySelectorAll<HTMLButtonElement>('[data-activity-id].is-active'))
  expect(active).toHaveLength(1)
  expect(active[0]).toHaveAttribute('data-activity-id', id)
}

function clickMobileActivity(id: string) {
  const button = document.querySelector<HTMLButtonElement>(`[data-mobile-activity-id="${id}"]`)
  expect(button).not.toBeNull()
  fireEvent.click(button!)
}

function mobileActiveIDs() {
  return Array.from(document.querySelectorAll<HTMLButtonElement>('[data-mobile-activity-id][aria-current="page"]'))
    .map((button) => button.dataset.mobileActivityId)
}

function clickExplicitMode(mode: 'ide' | 'interactive') {
  const button = document.querySelector<HTMLButtonElement>(`[data-onboarding-anchor="mode-${mode}"]`)
  expect(button).not.toBeNull()
  fireEvent.click(button!)
}

function expectPressedMode(mode: 'ide' | 'interactive') {
  const button = document.querySelector<HTMLButtonElement>(`[data-onboarding-anchor="mode-${mode}"]`)
  expect(button).toHaveAttribute('aria-pressed', 'true')
}

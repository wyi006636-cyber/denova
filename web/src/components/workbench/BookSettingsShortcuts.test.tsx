import { beforeEach, describe, expect, it } from 'vitest'
import { isBookSettingPath, readPinnedPaths } from './BookSettingsShortcuts'

describe('book settings shortcut persistence', () => {
  beforeEach(() => window.localStorage.clear())

  it('adds the story ledger and casebook when reading a pinned list from the previous storage version', () => {
    window.localStorage.setItem('nova.outline.pinned-settings:/book', JSON.stringify({
      version: 3,
      paths: ['setting/outline.md', 'CREATOR.md', 'setting/progress.md', 'ideas.md', '@lore-tab'],
    }))

    expect(readPinnedPaths('/book')).toEqual([
      'setting/outline.md',
      'CREATOR.md',
      'setting/progress.md',
      'ideas.md',
      '@lore-tab',
      'setting/story-ledger.md',
      'setting/case-index.md',
    ])
  })

  it.each([2, 3, 4])('migrates version %s pins to the casebook entry', (version) => {
    window.localStorage.setItem('nova.outline.pinned-settings:/book', JSON.stringify({
      version,
      paths: ['setting/outline.md', 'CREATOR.md', 'setting/progress.md', 'ideas.md', '@lore-tab', 'setting/story-ledger.md'],
    }))
    expect(readPinnedPaths('/book')).toContain('setting/case-index.md')
  })

  it('keeps the fixed casebook entry while excluding case files and references from discovered settings', () => {
    const chapterPlans = new Set<string>()
    expect(isBookSettingPath('references/foo.md', chapterPlans)).toBe(false)
    expect(isBookSettingPath('setting/cases/c-one.md', chapterPlans)).toBe(false)
    expect(isBookSettingPath('setting/case-index.md', chapterPlans)).toBe(true)
    expect(isBookSettingPath('setting/case-index.md', chapterPlans)).toBe(true)
  })
})

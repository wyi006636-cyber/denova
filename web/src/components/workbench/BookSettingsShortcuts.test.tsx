import { beforeEach, describe, expect, it } from 'vitest'
import { readPinnedPaths } from './BookSettingsShortcuts'

describe('book settings shortcut persistence', () => {
  beforeEach(() => window.localStorage.clear())

  it('adds the story ledger when reading a pinned list from the previous storage version', () => {
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
    ])
  })
})

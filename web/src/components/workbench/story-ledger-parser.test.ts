import { describe, expect, it } from 'vitest'
import {
  extractChapterPrefix,
  findChapterPrefixMatches,
  filterStoryLedger,
  parseChapterGoals,
  parseStoryLedger,
  patchLedgerEntry,
} from './story-ledger-parser'

describe('story ledger parser', () => {
  it('parses Chinese and English chapter goal labels', () => {
    const goals = parseChapterGoals(`### ch00012 夜探\n\n- 目标：拿到旧印\n- Conflict: the keeper exposes him\n- Plant: t-old-seal\n- Payoff:\n- Boundary: do not reveal the owner\n- Expected words: 3000\n- Hook: the seal burns`)
    expect(goals.get('ch00012')).toMatchObject({
      title: '夜探',
      goal: '拿到旧印',
      conflict: 'the keeper exposes him',
      plant: 't-old-seal',
      boundary: 'do not reveal the owner',
      wordTarget: '3000',
      hook: 'the seal burns',
    })
  })

  it('computes four statuses without treating pending confirmation as recovered', () => {
    const entries = parseStoryLedger(`# 故事线与伏笔\n\n## t-planned Planned\n- Type: 故事线\n- Summary: waits\n- Planned recovery: ch00018\n- Actual recovery:\n- Deprecated: 否\n\n## t-planted Planted\n- 种类：伏笔\n- 一句话：seeded\n- 埋设章：ch00003\n- 待确认：ch00009\n- 弃用：否\n\n## t-recovered Recovered\n- 种类：伏笔\n- 一句话：done\n- 实际回收：ch00010\n\n## t-deprecated Deprecated\n- 种类：故事线\n- 一句话：dropped\n- 埋设章：ch00002\n- 弃用：是`)
    expect(entries.map((entry) => entry.status)).toEqual(['planned', 'planted', 'recovered', 'deprecated'])
    expect(filterStoryLedger(entries, 'planted')).toHaveLength(1)
  })

  it('changes only the target field lines when confirming recovery', () => {
    const source = `# 故事线与伏笔\r\n\r\n## t-old-seal 旧印\r\n\r\n- 种类：伏笔\r\n- 一句话：能开门\r\n- 埋设章：ch00003\r\n- 计划回收：ch00018\r\n- 实际回收：\r\n- 弃用：否\r\n- 待确认：ch00012\r\n\r\n## 归档\r\n`
    const entry = parseStoryLedger(source)[0]
    const next = patchLedgerEntry(source, entry, 'confirm', 'ch00012')
    expect(next).toContain('- 实际回收：ch00012')
    expect(next).toContain('- 待确认：')
    expect(next.split('\r\n').filter((line, index) => line !== source.split('\r\n')[index])).toEqual([
      '- 实际回收：ch00012',
      '- 待确认：',
    ])
  })

  it('inserts missing Chinese fields at the end of the entry block', () => {
    const source = `# 故事线与伏笔\n\n## t-missing 缺字段\n- 种类：伏笔\n- 一句话：等待确认\n- 埋设章：ch00003\n\n## t-next 下一条\n- 种类：故事线\n`
    const entry = parseStoryLedger(source)[0]
    const next = patchLedgerEntry(source, entry, 'confirm', 'ch00012')
    expect(next).toContain('- 实际回收：ch00012')
    expect(next).toContain('- 待确认：')
    expect(next.indexOf('- 实际回收：ch00012')).toBeLessThan(next.indexOf('## t-next'))
    expect(parseStoryLedger(next)[0]).toMatchObject({ actualRecovery: 'ch00012', pending: '' })
  })

  it('inserts missing English fields and uses English deprecation values', () => {
    const source = `# Story ledger\n\n## t-missing Dropped\n- Type: thread\n- Summary: keep language\n- Planted chapter: ch00003\n`
    const entry = parseStoryLedger(source)[0]
    const next = patchLedgerEntry(source, entry, 'deprecate')
    expect(next).toContain('- Deprecated: yes')
    expect(parseStoryLedger(next)[0].deprecated).toBe('yes')
  })

  it('ends entries at archive headings and keeps archived entries out of the default filter', () => {
    const source = `# Story ledger\n\n## t-active Active\n- Type: thread\n\n## Archive\n\n## t-old Old\n- Type: thread\n- Deprecated: yes\n`
    const entries = parseStoryLedger(source)
    expect(entries.map((entry) => entry.archived)).toEqual([false, true])
    expect(filterStoryLedger(entries, 'all').map((entry) => entry.id)).toEqual(['t-active'])
    expect(filterStoryLedger(entries, 'archived').map((entry) => entry.id)).toEqual(['t-old'])
  })

  it('keeps malformed entry locations unchanged instead of writing through them', () => {
    const source = `# Story ledger\n\n## t-real Real\n- Type: thread\n`
    const [entry] = parseStoryLedger(source)
    expect(patchLedgerEntry(source, { ...entry, startLine: 999 }, 'deprecate')).toBe(source)
    expect(patchLedgerEntry('not markdown', entry, 'deprecate')).toBe('not markdown')
  })

  it('does not invent a chapter prefix for legacy filenames', () => {
    expect(extractChapterPrefix('chapters/001-legacy.md')).toBeNull()
    expect(extractChapterPrefix('chapters/ch00012-opening.md')).toBe('ch00012')
  })

  it('returns every duplicate chapter prefix so the UI can avoid guessing a destination', () => {
    const matches = findChapterPrefixMatches([
      { path: 'chapters/ch00012-a.md' },
      { path: 'chapters/ch00012-b.md' },
      { path: 'chapters/ch00013.md' },
    ], 'pending at ch00012')
    expect(matches.map((chapter) => chapter.path)).toEqual(['chapters/ch00012-a.md', 'chapters/ch00012-b.md'])
  })
})

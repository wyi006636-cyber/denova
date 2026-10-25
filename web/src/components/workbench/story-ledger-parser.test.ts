import { describe, expect, it } from 'vitest'
import {
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
})

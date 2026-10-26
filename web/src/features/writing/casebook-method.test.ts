import { describe, expect, it } from 'vitest'
import { caseMethodPrompt } from './casebook-method'

describe('caseMethodPrompt', () => {
  const labels = { direction: '方向', scenario: '适用场景', method: '方法', avoid: '不要搬用' }

  it('keeps only the reusable fields for collected cases', () => {
    const text = caseMethodPrompt({
      status: '已收藏', direction: '开篇', scenario: '章首', method: '先写异常', avoid: '不要复制原作',
    }, labels)
    expect(text).toContain('方法：先写异常')
    expect(text).not.toContain('references/sources')
    expect(text).not.toContain('原文摘录')
  })

  it('returns empty text for pending cases', () => {
    expect(caseMethodPrompt({ status: '待确认', direction: '开篇', scenario: '', method: '待确认', avoid: '' }, labels)).toBe('')
  })

  it('uses an ASCII separator for the English interface', () => {
    expect(caseMethodPrompt({
      status: '已收藏', direction: 'opening', scenario: 'chapter start', method: 'show the anomaly', avoid: 'do not copy the source',
    }, { direction: 'Direction', scenario: 'Use when', method: 'Method', avoid: 'Do not copy', separator: ':' })).toBe([
      'Direction: opening',
      'Use when: chapter start',
      'Method: show the anomaly',
      'Do not copy: do not copy the source',
    ].join('\n'))
  })
})

import { describe, expect, it } from 'vitest'
import {
  STYLE_FINGERPRINT_LIMITS,
  computeStyleFingerprint,
} from './style-fingerprint'

describe('style fingerprint', () => {
  it('keeps empty and punctuation-only text out of prose distributions', () => {
    const empty = computeStyleFingerprint([{ content: '' }])
    expect(empty.chapterCount).toBe(0)
    expect(empty.characterCount).toBe(0)
    expect(empty.sentenceLength.sampleCount).toBe(0)

    const punctuation = computeStyleFingerprint([{ content: '……！！——。' }])
    expect(punctuation.chapterCount).toBe(1)
    expect(punctuation.sentenceLength.sampleCount).toBe(0)
    expect(punctuation.paragraphLength.sampleCount).toBe(0)
  })

  it('uses character lengths and P50/P90 quantiles for Chinese sentences', () => {
    const result = computeStyleFingerprint([{ content: '甲乙。甲乙丙丁。甲乙丙丁戊己。' }])
    expect(result.sentenceLength).toMatchObject({ p50: 4, p90: 6, unit: 'characters', sampleCount: 3 })
  })

  it('uses word counts for English sentences', () => {
    const result = computeStyleFingerprint([{ content: 'One two. One two three four. One two three four five six.' }])
    expect(result.sentenceLength).toMatchObject({ p50: 4, p90: 6, unit: 'words', sampleCount: 3 })
  })

  it('counts dialogue paragraphs and consecutive dialogue runs', () => {
    const result = computeStyleFingerprint([{ content: '「甲」。\n\n「乙」。\n\n叙述。\n\n“丙”。\n\n“丁”。' }])
    expect(result.dialogueParagraphCount).toBe(4)
    expect(result.paragraphCount).toBe(5)
    expect(result.dialogueParagraphRatio).toBe(0.8)
    expect(result.dialogueRunAverage).toBe(2)
    expect(result.dialogueRunLongest).toBe(2)
  })

  it('caps multi-chapter input at twelve chapters and 200k characters', () => {
    const chapters = Array.from({ length: 14 }, (_, index) => ({ path: `chapters/${index}.md`, content: `第${index}章正文。` }))
    const capped = computeStyleFingerprint(chapters)
    expect(capped.chapterCount).toBe(12)
    expect(capped.truncatedByChapters).toBe(true)

    const huge = computeStyleFingerprint([{ content: '甲'.repeat(STYLE_FINGERPRINT_LIMITS.maxCharacters + 100) }])
    expect(huge.characterCount).toBe(STYLE_FINGERPRINT_LIMITS.maxCharacters)
    expect(huge.truncatedByCharacters).toBe(true)
  })

  it('omits function-word phrases from repeated phrase candidates', () => {
    const result = computeStyleFingerprint([{
      content: '一个人在门口。一个人在门口。一个人在门口。红色旗帜。红色旗帜。红色旗帜。',
    }])
    expect(result.repeatedPhrases.some(item => item.phrase === '一个')).toBe(false)
    expect(result.repeatedPhrases.some(item => item.phrase === '红色旗帜')).toBe(true)
    expect(result.repeatedPhrases[0]?.count).toBeGreaterThanOrEqual(3)
  })

  it('keeps 200k-character statistics in the intended fast path', () => {
    const source = '清晨的雾气沿着石阶退去，守门人检查了旧锁。'.repeat(10000).slice(0, 200_000)
    const result = computeStyleFingerprint([{ content: source }])
    expect(result.characterCount).toBe(200_000)
    expect(result.elapsedMs).toBeLessThan(300)
  })
})

import { describe, expect, it } from 'vitest'
import {
  STYLE_FINGERPRINT_LIMITS,
  computeStyleFingerprint,
  styleFingerprintToMarkdown,
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
    const alphabet = [...'甲乙丙丁戊己庚辛壬癸子丑寅卯辰巳午未申酉戌亥春夏秋冬风雨雷电山川湖海']
    let source = ''
    for (let index = 0; source.length < STYLE_FINGERPRINT_LIMITS.maxCharacters; index++) source += alphabet[(index * 37 + index * index) % alphabet.length]
    source = source.slice(0, STYLE_FINGERPRINT_LIMITS.maxCharacters)
    const result = computeStyleFingerprint([{ content: source }])
    expect(result.characterCount).toBe(200_000)
    expect(result.elapsedMs).toBeLessThan(300)
  })

  it('removes ATX headings and chooses one sentence unit for mixed prose', () => {
    const result = computeStyleFingerprint([{ content: '# Chapter 001\n\n沈宁说：「在下不会退。」\n\n红色旗帜在风中展开。红色旗帜在风中展开。' }])
    expect(result.characterCount).toBe(32)
    expect(result.sentenceLength).toMatchObject({ p50: 9, p90: 9, unit: 'characters', sampleCount: 3 })
    expect(result.dialogueParagraphRatio).toBe(0.5)
    expect(styleFingerprintToMarkdown(result)).toContain('按字数统计')
  })

  it('uses the body majority language for mixed English and Chinese sentences', () => {
    const result = computeStyleFingerprint([{ content: '他推开门。屋里没有人。OK.' }])
    expect(result.sentenceLength.unit).toBe('characters')
    expect(result.sentenceLength.p50).toBe(4)
    expect(result.sentenceLength.p90).toBe(5)
  })

  it('uses a conventional median and interpolated P90 for even samples', () => {
    const result = computeStyleFingerprint([{ content: '甲乙。甲乙丙丁。甲乙丙丁戊己。甲乙丙丁戊己庚辛。' }])
    expect(result.sentenceLength).toMatchObject({ p50: 5, p90: 7, sampleCount: 4 })
  })

  it('does not split common English abbreviations into a sentence', () => {
    const result = computeStyleFingerprint([{ content: 'Mr. Smith went to the old house. He did not stay.' }])
    expect(result.sentenceLength).toMatchObject({ sampleCount: 2, unit: 'words', p50: 6, p90: 7 })
  })

  it('counts useful repeated phrases without overlapping fragments', () => {
    const result = computeStyleFingerprint([{ content: [
      '红色旗帜立在门口。'.repeat(3),
      '旧门锁生了锈。'.repeat(3),
      '他看了看天。'.repeat(3),
      '这不是出路。'.repeat(3),
      '哈哈哈哈。',
    ].join('') }])
    expect(result.repeatedPhrases.some(item => item.phrase === '看了看' && item.count === 3)).toBe(true)
    expect(result.repeatedPhrases.some(item => item.phrase === '一个')).toBe(false)
    expect(result.repeatedPhrases.some(item => item.phrase === '色旗帜')).toBe(false)
    expect(result.repeatedPhrases.some(item => item.phrase === '哈哈')).toBe(false)
  })

  it('localizes copied Markdown', () => {
    const result = computeStyleFingerprint([{ content: 'One two three.' }], { includeRepeatedPhrases: false })
    expect(styleFingerprintToMarkdown(result, 'en')).toContain('Scope:')
    expect(styleFingerprintToMarkdown(result, 'en')).toContain('(words)')
  })
})

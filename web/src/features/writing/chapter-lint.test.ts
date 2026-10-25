import { describe, expect, it } from 'vitest'
import {
  CHAPTER_LINT_LIMITS,
  parseBannedWords,
  runChapterLint,
  runChapterLintWithBannedWords,
} from './chapter-lint'

describe('chapter lint', () => {
  it('parses comments, suggestions and invalid banned-word lines', () => {
    expect(parseBannedWords('# words\n秘密 -> 隐情\n\n无效 -> 太 -> 多\n秘密')).toEqual({
      words: [{ word: '秘密', suggestion: '隐情' }],
      invalidLines: [4],
    })
  })

  it('detects all first-release categories in Chinese text', () => {
    const report = runChapterLintWithBannedWords(
      '这是秘密，秘密，秘密。。[[不存在]]微微一笑。微微一笑。微微一笑。（未闭合',
      '秘密 -> 隐情',
      { loreNames: ['存在'] },
    )
    expect(report.counts.banned).toBe(3)
    expect(report.counts.ai).toBeGreaterThan(0)
    expect(report.counts.repeat).toBeGreaterThan(0)
    expect(report.counts.punctuation).toBeGreaterThan(0)
    expect(report.counts.reference).toBe(1)
  })

  it('uses the English lexicon only for English books', () => {
    const english = runChapterLint('It is worth noting that she paused.', { locale: 'en' })
    expect(english.counts.ai).toBe(1)
    const chinese = runChapterLint('It is worth noting that她停下。', { locale: 'zh' })
    expect(chinese.counts.ai).toBe(0)
  })

  it('supports category switches and empty text', () => {
    expect(runChapterLint('', { bannedWords: [{ word: 'x' }], enabled: { banned: false } }).issues).toEqual([])
    expect(runChapterLint('秘密', { bannedWords: [{ word: '秘密' }], enabled: { banned: false } }).counts.banned).toBe(0)
  })

  it('handles 100k characters within the local check budget', () => {
    const source = '这是一个正常句子。'.repeat(10000)
    const started = performance.now()
    const report = runChapterLint(source, { enabled: { ai: false, repeat: false } })
    const elapsed = performance.now() - started
    expect(report.issues.length).toBeLessThanOrEqual(CHAPTER_LINT_LIMITS.maxIssues)
    expect(elapsed).toBeLessThan(300)
  })
})


import { describe, expect, it } from 'vitest'
import {
  CHAPTER_LINT_LIMITS,
  parseBannedWords,
  runChapterLint,
  runChapterLintWithBannedWords,
} from './chapter-lint'

const only = (category: 'banned' | 'ai' | 'repeat' | 'punctuation' | 'length' | 'reference') => ({
  banned: category === 'banned',
  ai: category === 'ai',
  repeat: category === 'repeat',
  punctuation: category === 'punctuation',
  length: category === 'length',
  reference: category === 'reference',
})

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
    expect(report.counts.banned).toBe(1)
    expect(report.issues.find(issue => issue.category === 'banned')?.occurrences).toHaveLength(3)
    expect(report.issues.find(issue => issue.category === 'banned')?.message.params).toMatchObject({ count: 3 })
    expect(report.counts.ai).toBeGreaterThan(0)
    const aiIssue = report.issues.find(issue => issue.category === 'ai')
    expect(aiIssue?.occurrences).toHaveLength(3)
    expect(aiIssue?.message.params).toMatchObject({ count: 3 })
    expect(report.counts.repeat).toBeGreaterThan(0)
    expect(report.counts.punctuation).toBeGreaterThan(0)
    expect(report.counts.reference).toBe(1)
    expect(report.issues.find(issue => issue.category === 'banned')?.message.key).toBe('editor.lint.issue.banned')
  })

  it('chooses the AI lexicon from chapter content, independently of UI locale', () => {
    const english = runChapterLint('It is worth noting that she paused.', { locale: 'zh' })
    expect(english.counts.ai).toBe(1)
    const chinese = runChapterLint('这是中文正文。It is worth noting that她停下。', { locale: 'en' })
    expect(chinese.counts.ai).toBe(0)
    expect(runChapterLint('这是中文正文，微微一笑。', { locale: 'en' }).counts.ai).toBeGreaterThan(0)
  })

  it('keeps standard ellipses, English periods and decimals quiet', () => {
    const english = runChapterLint('An English sentence. Another one. The value is 3.14 and ...', { enabled: only('punctuation') })
    expect(english.counts.punctuation).toBe(0)
    const chinese = runChapterLint('他说……然后继续。', { enabled: only('punctuation') })
    expect(chinese.counts.punctuation).toBe(0)
  })

  it('reports malformed ellipses only in their local language context', () => {
    const report = runChapterLint('他说…又说...最后。。。还有....', { enabled: only('punctuation') })
    expect(report.counts.punctuation).toBeGreaterThanOrEqual(4)
    const english = runChapterLint('English text .. and …. Four dots ....', { enabled: only('punctuation') })
    expect(english.counts.punctuation).toBe(2)
  })

  it('aggregates meaningful repeats, keeps the longest phrase and excludes lore terms', () => {
    const repeated = runChapterLint('红色的旗帜在风中展开。红色的旗帜在风中展开。红色的旗帜在风中展开。', { enabled: only('repeat') })
    expect(repeated.counts.repeat).toBe(1)
    const issue = repeated.issues[0]
    expect(issue.message).toMatchObject({ key: 'editor.lint.issue.repeat', params: { count: 3 } })
    expect(issue.occurrences).toHaveLength(3)
    expect(issue.text.length).toBeGreaterThanOrEqual(4)
    const names = runChapterLint('张三到了。张三看向窗外。张三没有说话。', { enabled: only('repeat'), loreTerms: ['张三'] })
    expect(names.counts.repeat).toBe(0)
    const aliases = runChapterLint('[[别名]]已经出现。', { enabled: only('reference'), loreTerms: ['人物', '别名'] })
    expect(aliases.counts.reference).toBe(0)
  })

  it('does not flag contractions or paragraph-style Chinese dialogue quotes', () => {
    const report = runChapterLint('I don’t know.\n“第一段没有后引号\n“第二段也没有后引号\n第三段”', { enabled: only('punctuation') })
    expect(report.counts.punctuation).toBe(0)
    const broken = runChapterLint('句中“开引号没有闭合', { enabled: only('punctuation') })
    expect(broken.counts.punctuation).toBe(1)
  })

  it('uses words for English sentence length and characters for Chinese', () => {
    const english = runChapterLint(`${'word '.repeat(41)}end.`, { enabled: only('length') })
    expect(english.counts.length).toBeGreaterThan(0)
    const chinese = runChapterLint(`${'这是一个很长的中文句子，用来验证按字符统计句子长度的阈值'.repeat(8)}。`, { enabled: only('length') })
    expect(chinese.counts.length).toBeGreaterThan(0)
  })

  it('supports category switches and empty text', () => {
    expect(runChapterLint('', { bannedWords: [{ word: 'x' }], enabled: { banned: false } }).issues).toEqual([])
    expect(runChapterLint('秘密', { bannedWords: [{ word: '秘密' }], enabled: { banned: false } }).counts.banned).toBe(0)
  })

  it('does not report the reviewed normal-language false positives', () => {
    expect(runChapterLint('他挡住！！', { enabled: only('punctuation') }).issues).toEqual([])
    expect(runChapterLint('你吃饭吗？？？', { enabled: only('punctuation') }).issues).toEqual([])
    expect(runChapterLint('数字是 1,200两，随后继续。', { enabled: only('punctuation') }).issues).toEqual([])
    expect(runChapterLint('他走了十分钟。', { enabled: only('ai') }).issues).toEqual([])
    expect(runChapterLint('The secretary passed the class and kept the password.', {
      bannedWords: [{ word: 'ass' }, { word: 'pass' }],
      enabled: only('banned'),
    }).issues).toEqual([])
    expect(runChapterLint('She was not ready. She was not late. She was not afraid.', { enabled: only('repeat') }).issues).toEqual([])
    expect(runChapterLint('In a way, the job was simple.', { enabled: only('ai') }).issues).toEqual([])
  })

  it('keeps errors ahead of a capped info list and reports truncation', () => {
    const repeated = Array.from({ length: 600 }, (_, index) => {
      const phrase = String.fromCharCode(0x4e00 + index) + String.fromCharCode(0x5200 + index) + String.fromCharCode(0x6000 + index)
      return `${phrase}。${phrase}。${phrase}。`
    }).join('')
    const report = runChapterLint(`${repeated}[[Missing]]`, { loreTerms: [] })
    expect(report.resultsTruncated).toBe(true)
    expect(report.issuesTotal).toBeGreaterThan(CHAPTER_LINT_LIMITS.maxIssues)
    expect(report.issues.some(issue => issue.category === 'reference')).toBe(true)
    expect(report.truncatedCategories).toContain('repeat')
  })

  it('handles 100k and 200k varied Chinese characters with 150 lore terms', () => {
    const sentences = [
      '清晨的雾气沿着石阶退去，守门人检查了旧锁。',
      '雨声敲在屋檐上，旅人把地图折好放回口袋。',
      '厨房传来锅铲声，孩子在窗边数着缓慢经过的车。',
      '山谷的信号忽明忽暗，队伍决定先在桥下休息。',
      '她写下新的路线，又删掉了一个不确定的地名。',
      '午后的阳光穿过百叶窗，桌面留下整齐的亮斑。',
      '远处的钟声响了三次，市场开始收拾散落的木箱。',
      '船靠岸时风向变了，水手抬头确认云层的厚度。',
      '他把证据按时间排好，发现两份记录来自同一天。',
      '夜班结束前，值守员在日志里补上最后一行。',
    ]
    const source = Array.from({ length: 5000 }, (_, index) => sentences[index % sentences.length]).join('\n').slice(0, 100000)
    const loreTerms = Array.from({ length: 150 }, (_, index) => `资料名${index}号人物`)
    const started = performance.now()
    const report = runChapterLint(source, { loreTerms })
    const elapsed = performance.now() - started
    expect(source.length).toBe(100000)
    expect(report.issues.length).toBeLessThanOrEqual(CHAPTER_LINT_LIMITS.maxIssues)
    expect(elapsed).toBeLessThan(300)

    const source200k = `${source}\n${source}`
    const started200k = performance.now()
    const report200k = runChapterLint(source200k, { loreTerms })
    const elapsed200k = performance.now() - started200k
    expect(source200k.length).toBe(200001)
    expect(report200k.issues.length).toBeLessThanOrEqual(CHAPTER_LINT_LIMITS.maxIssues)
    // Keep the 200k figure visible for future regressions without making a
    // noisy machine-specific wall-clock promise.
    console.info(`chapter lint 200k: ${elapsed200k.toFixed(1)}ms`)
  })
})

import { describe, expect, it } from 'vitest'
import { parseVoiceRules, runVoiceLint } from './voice-lint'

describe('voice lint', () => {
  const character = {
    name: '沈宁',
    aliases: ['阿宁'],
    content: '## 说话方式\n\n- 口头禅：嗯\n- 禁忌说法：在下、鄙人\n- 示例：在下只是举例，不应成为检查要求。',
  }

  it('parses Chinese and English voice sections and ignores examples', () => {
    expect(parseVoiceRules(character.content).avoid).toEqual(['在下', '鄙人'])
    expect(parseVoiceRules('## Voice\n\n- Avoid: sir; formal connectors\n- Example: sir')).toEqual({
      avoid: ['sir', 'formal connectors'],
    })
    expect(parseVoiceRules('## 资料\n\n- 禁忌说法：在下')).toEqual({ avoid: [] })
    expect(parseVoiceRules('## 说话方式（可选）\n\n- 禁忌说法：「在下」、鄙人')).toEqual({ avoid: ['在下', '鄙人'] })
  })

  it('warns when an attributed character uses an avoided phrase', () => {
    const result = runVoiceLint('沈宁说：「在下不会退。」', [character])
    expect(result.parsedCharacters).toBe(1)
    expect(result.issues).toHaveLength(1)
    expect(result.issues[0]).toMatchObject({ category: 'voice', severity: 'warning', text: '在下' })
  })

  it('recognizes aliases and speaker attribution after the quote', () => {
    const result = runVoiceLint('『鄙人不去。』，阿宁答道。', [character])
    expect(result.issues[0]?.severity).toBe('warning')
  })

  it('does not report avoided words in narration outside quotes', () => {
    const result = runVoiceLint('沈宁想起在下这个旧称，但没有开口。', [character])
    expect(result.issues).toHaveLength(0)
  })

  it('downgrades a quote with an unknown speaker to information', () => {
    const result = runVoiceLint('「在下只是路过。」', [character])
    expect(result.issues).toHaveLength(1)
    expect(result.issues[0]?.severity).toBe('info')
    expect(result.issues[0]?.message.key).toBe('editor.lint.issue.voiceUnknown')
  })

  it('keeps one information finding when several characters share the phrase', () => {
    const result = runVoiceLint('「在下只是路过。」', [
      character,
      { name: '顾言', content: '## Voice\n\n- Avoid: 在下' },
    ])
    expect(result.issues).toHaveLength(1)
  })

  it('skips old entries without a voice section', () => {
    const result = runVoiceLint('沈宁说：「在下不会退。」', [{ ...character, content: '# 沈宁\n\n没有口吻约定。' }])
    expect(result.parsedCharacters).toBe(0)
    expect(result.issues).toEqual([])
  })

  it('uses all named characters for attribution, even when one has no avoid list', () => {
    const result = runVoiceLint('张三说：「在下不走」', [
      { name: '张三', content: '# 张三\n\n没有口吻约定。' },
      { name: '李四', content: '## 说话方式\n\n- 禁忌说法：在下' },
    ])
    expect(result.issues).toEqual([])
  })

  it('chooses the name closest to the speech verb', () => {
    const result = runVoiceLint('张三看着李四说：「在下不走」', [
      { name: '张三', content: '## 说话方式\n\n- 禁忌说法：在下' },
      { name: '李四', content: '## 说话方式\n\n- 禁忌说法：哼' },
    ])
    expect(result.issues).toEqual([])
  })

  it('accepts English commas and ASCII double quotes and respects Latin word boundaries', () => {
    const john = { name: 'John', content: '## Voice\n\n- Avoid: art' }
    expect(runVoiceLint('John said, “do not start”', [john]).issues).toEqual([])
    expect(runVoiceLint('John said “do not start”', [john]).issues).toEqual([])
    expect(runVoiceLint('John said “the art of leaving”', [john]).issues[0]).toMatchObject({ severity: 'warning', text: 'art' })
    expect(runVoiceLint('“the art of leaving,” John said.', [john]).issues[0]).toMatchObject({ severity: 'warning', text: 'art' })
    expect(runVoiceLint('John said, "the art"', [john]).issues[0]).toMatchObject({ severity: 'warning', text: 'art' })
    expect(runVoiceLint('John said, "do not start"', [john]).issues).toEqual([])
  })

  it('keeps quoted and unquoted avoid terms on the same field', () => {
    expect(parseVoiceRules('## 说话方式\n\n- 禁忌说法：「在下」、鄙人')).toEqual({ avoid: ['在下', '鄙人'] })
    expect(parseVoiceRules('## Voice\n\n- Avoid: “sir”; formal connectors')).toEqual({ avoid: ['sir', 'formal connectors'] })
    expect(parseVoiceRules('## 说话方式\n\n- 禁忌说法：不会说「在下」；不用书面连接词')).toEqual({ avoid: ['在下', '书面连接词'] })
    expect(parseVoiceRules('## Voice\n\n- Avoid: Avoid “sir”; formal connectors')).toEqual({ avoid: ['sir', 'formal connectors'] })
  })

  it('keeps the longer overlapping name closest to the speech verb', () => {
    const result = runVoiceLint('沈宁说：「在下不走」', [
      { name: '沈宁', content: '## 说话方式\n\n- 禁忌说法：在下' },
      { name: '宁', content: '## 说话方式\n\n- 禁忌说法：哼' },
    ])
    expect(result.issues).toHaveLength(1)
    expect(result.issues[0]).toMatchObject({ severity: 'warning', text: '在下', message: { params: { name: '沈宁' } } })
    expect(runVoiceLint('沈宁说：「哼」', [
      { name: '沈宁', content: '## 说话方式\n\n- 禁忌说法：在下' },
      { name: '宁', content: '## 说话方式\n\n- 禁忌说法：哼' },
    ]).issues).toEqual([])
  })

  it('matches Latin names case-insensitively without matching inside another word', () => {
    const son = { name: 'Son', content: '## Voice\n\n- Avoid: art' }
    expect(runVoiceLint('son said, "the art"', [son]).issues[0]).toMatchObject({ severity: 'warning', text: 'art' })
    expect(runVoiceLint('Johnson said, "the art"', [son]).issues[0]?.severity).toBe('info')
  })
})

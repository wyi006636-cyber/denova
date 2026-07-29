import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { WorkspaceChangeGroupSummary } from '@/features/changes/types'
import type { AgentUIMessage } from '@/lib/agent-ui'
import { deriveFanqieProgress, FanqieProgressStatus } from './FanqieProgressStatus'

describe('FanqieProgressStatus', () => {
  it('covers idea, proposal, outline, chapter writing, and Diff review from existing Agent state', () => {
    expect(progress([])).toMatchObject({ stage: 'idea' })

    expect(progress([assistant('<!-- fanqie-progress: proposal -->')])).toMatchObject({
      stage: 'proposal',
    })

    expect(progress([assistant('<!-- fanqie-progress: outline total=8 -->')])).toMatchObject({
      stage: 'outline',
      totalChapters: 8,
    })

    expect(progress([
      assistant('<!-- fanqie-progress: outline total=8 -->'),
      tool('write_file', 'input-streaming', { file_path: 'chapters/ch00003-转折.md' }),
    ], { isStreaming: true })).toMatchObject({
      stage: 'writing',
      chapter: 3,
      totalChapters: 8,
      files: ['chapters/ch00003-转折.md'],
      operation: 'write',
    })

    expect(progress([
      assistant('<!-- fanqie-progress: chapter chapter=1 total=8 path=chapters/ch00001.md -->'),
    ], {
      changeGroups: [changeGroup('pending', ['setting/outline.md', 'chapters/ch00001.md'])],
    })).toMatchObject({
      stage: 'diff',
      chapter: 1,
      totalChapters: 8,
      files: ['setting/outline.md', 'chapters/ch00001.md'],
    })
  })

  it('shows the author what is happening, which file is changing, and what to do next', () => {
    const state = progress([
      assistant('<!-- fanqie-progress: outline total=8 -->'),
      tool('edit_file', 'input-streaming', { file_path: 'chapters/ch00003-转折.md' }),
    ], { isStreaming: true })

    render(<FanqieProgressStatus progress={state} />)

    expect(screen.getByRole('status', { name: '番茄短篇创作进度' })).toHaveTextContent('正在修改第 3/8 章')
    expect(screen.getByText('chapters/ch00003-转折.md')).toBeVisible()
    expect(screen.getByText(/等待修改完成/)).toBeVisible()
  })

  it('shows the current state, every changed file, and the full next action without truncation', () => {
    render(<FanqieProgressStatus progress={{
      stage: 'accepted',
      files: ['chapters/ch00001-第一章-热牛奶.md', 'setting/outline.md'],
      chapter: 1,
      totalChapters: 8,
    }} />)

    const status = screen.getByRole('status', { name: '番茄短篇创作进度' })
    expect(within(status).getByText('当前')).toBeVisible()
    expect(within(status).getByText('chapters/ch00001-第一章-热牛奶.md', { exact: true })).toBeVisible()
    expect(within(status).getByText('setting/outline.md', { exact: true })).toBeVisible()
    expect(within(status).getByText('让 Agent 继续下一章，或先对本章做局部修改。')).not.toHaveClass('truncate')
  })

  it('moves past Diff review after the author accepts or rejects the chapter change', () => {
    const messages = [assistant('<!-- fanqie-progress: chapter chapter=1 total=8 path=chapters/ch00001.md -->')]

    expect(progress(messages, { changeGroups: [changeGroup('accepted', ['chapters/ch00001.md'])] })).toMatchObject({
      stage: 'accepted',
      chapter: 1,
      totalChapters: 8,
    })
    expect(progress(messages, { changeGroups: [changeGroup('rejected', ['chapters/ch00001.md'])] })).toMatchObject({
      stage: 'rejected',
      chapter: 1,
      totalChapters: 8,
    })
  })
})

function progress(messages: AgentUIMessage[], options: {
  isStreaming?: boolean
  changeGroups?: WorkspaceChangeGroupSummary[]
} = {}) {
  return deriveFanqieProgress({
    messages,
    isStreaming: options.isStreaming ?? false,
    changeGroups: options.changeGroups ?? [],
  })
}

function assistant(text: string): AgentUIMessage {
  return {
    id: `assistant-${text}`,
    role: 'assistant',
    parts: [{ type: 'text', text }],
  } as AgentUIMessage
}

function tool(name: string, state: string, input: Record<string, unknown>): AgentUIMessage {
  return {
    id: `tool-${name}`,
    role: 'assistant',
    parts: [{ type: 'dynamic-tool', toolName: name, toolCallId: `call-${name}`, state, input }],
  } as AgentUIMessage
}

function changeGroup(reviewStatus: WorkspaceChangeGroupSummary['review_status'], paths: string[]): WorkspaceChangeGroupSummary {
  return {
    id: `group-${reviewStatus}`,
    created_at: '2026-07-29T12:00:00Z',
    review_status: reviewStatus,
    apply_state: 'applied',
    paths,
    pending_edit_count: reviewStatus === 'pending' ? paths.length : 0,
  }
}

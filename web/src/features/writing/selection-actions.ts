import type { TFunction } from 'i18next'
import type { TextSelection } from '@/lib/api'
import type { AgentUIMessage } from '@/lib/agent-ui'

export const writingSelectionActions = ['continue', 'polish', 'humanize', 'rewrite', 'custom'] as const
export type WritingSelectionAction = typeof writingSelectionActions[number]

/** Ephemeral editor coordinates; never serialized as Project identity or a recovery record. */
export interface WritingSelectionTarget {
  projectId: string
  fileName: string
  revision: string
  document: string
  draft: string
  from: number
  to: number
  quote: TextSelection
}
export type AdoptionMode = 'insert' | 'replace'
export type AdoptionFailure = 'unavailable' | 'changed' | 'saveFailed'

/**
 * Resolve the pending action target only when the submitted attachment is the
 * exact quote that initiated the action. The composer can clone attachments as
 * it crosses hosted Agent surfaces, so compare their wire fields instead of
 * relying on object identity or hidden metadata.
 */
export function selectionActionOrigin(
  selections: TextSelection[],
  pendingTarget?: WritingSelectionTarget | null,
): WritingSelectionTarget | undefined {
  if (!pendingTarget || selections.length !== 1) return undefined
  const quote = selections[0]
  return quote.fileName === pendingTarget.quote.fileName
    && quote.startLine === pendingTarget.quote.startLine
    && quote.endLine === pendingTarget.quote.endLine
    && quote.content === pendingTarget.quote.content
    ? pendingTarget
    : undefined
}

export function writingSelectionPrompt(t: TFunction, action: WritingSelectionAction): string {
  return `${t(`editor.selection.${action}Prompt`)}\n\n${t('editor.selection.scopePrompt')}`
}

/** Only explicitly marked prose may be adopted without an author's text selection. */
export function extractReplyProse(reply: string): string {
  const blocks = [...reply.matchAll(/^(`{3,}|~{3,})(?:manuscript|prose|text|markdown)\s*\r?\n([\s\S]*?)^\1\s*$/gm)]
  return blocks.length === 1 ? blocks[0][2].trim() : ''
}

export function adoptionTargetFailure(current: WritingSelectionTarget | null, target: WritingSelectionTarget): AdoptionFailure | null {
  if (!current || current.projectId !== target.projectId || current.fileName !== target.fileName) return 'unavailable'
  if (current.revision !== target.revision || current.document !== target.document || current.draft !== target.draft) return 'changed'
  return null
}

/** Associate only the replies after the originating command, stopping at the next user turn. */
export function replySelectionTargets(messages: AgentUIMessage[], commands: ReadonlyMap<string, WritingSelectionTarget>) {
  const targets = new Map<string, WritingSelectionTarget>()
  let target: WritingSelectionTarget | undefined
  for (const message of messages) {
    if (message.role === 'user') {
      // A reply is eligible only when its immediately preceding user turn is
      // the command that carried the selection attachment. Unknown/history
      // turns intentionally clear the target instead of guessing from scope.
      target = commands.get(message.id)
    }
    if (message.role === 'assistant' && target && !message.metadata?.subagent) targets.set(message.id, target)
  }
  return targets
}

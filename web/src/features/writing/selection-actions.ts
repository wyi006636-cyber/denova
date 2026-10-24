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
type SelectionWithOrigin = TextSelection & { __writingSelectionTarget?: WritingSelectionTarget }

export type AdoptionMode = 'insert' | 'replace'
export type AdoptionFailure = 'unavailable' | 'changed' | 'saveFailed'

// Attach provenance to the existing composer attachment without changing its wire schema.
const selectionOrigins = new WeakMap<TextSelection, WritingSelectionTarget>()
let latestSelectionOrigin: WritingSelectionTarget | undefined
const GLOBAL_SELECTION_KEY = '__denovaWritingSelectionTarget'
export function rememberSelectionAction(target: WritingSelectionTarget) {
  selectionOrigins.set(target.quote, target)
  Object.defineProperty(target.quote, '__writingSelectionTarget', { value: target, enumerable: false, configurable: true })
  latestSelectionOrigin = target
  ;(globalThis as typeof globalThis & { [GLOBAL_SELECTION_KEY]?: WritingSelectionTarget })[GLOBAL_SELECTION_KEY] = target
}
export function latestSelectionActionTarget(): WritingSelectionTarget | undefined {
  return latestSelectionOrigin || (globalThis as typeof globalThis & { [GLOBAL_SELECTION_KEY]?: WritingSelectionTarget })[GLOBAL_SELECTION_KEY]
}
export function clearLatestSelectionActionTarget() {
  latestSelectionOrigin = undefined
  delete (globalThis as typeof globalThis & { [GLOBAL_SELECTION_KEY]?: WritingSelectionTarget })[GLOBAL_SELECTION_KEY]
}
export function selectionActionOrigin(selections: TextSelection[]): WritingSelectionTarget | undefined {
  // A request containing several action selections has no unambiguous replacement target.
  if (selections.length > 1) return undefined
  const exact = selections.length === 1
    ? selectionOrigins.get(selections[0]) || (selections[0] as SelectionWithOrigin).__writingSelectionTarget
    : undefined
  if (exact) {
    clearLatestSelectionActionTarget()
    return exact
  }
  // Composer context is bridged through an effect and can briefly clone the
  // TextSelection object. Consume the one-shot fallback from the same action.
  const fallback = latestSelectionActionTarget()
  clearLatestSelectionActionTarget()
  return fallback
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
  let userTurns = 0
  for (const message of messages) {
    if (message.role === 'user') {
      userTurns += 1
      // Some transports replace the optimistic command id while hydrating the
      // canonical transcript. The latest user turn is the only safe fallback;
      // subsequent turns deliberately clear it.
      target = commands.get(message.id) || (userTurns === 1 && commands.size === 1 ? [...commands.values()][0] : undefined)
    }
    if (message.role === 'assistant' && target && !message.metadata?.subagent) targets.set(message.id, target)
  }
  return targets
}

import type { CaseEntry } from '@/lib/api-client/casebook'

export interface CaseMethodLabels {
  direction: string
  scenario: string
  method: string
  avoid: string
  separator?: ':' | '：'
}

/** Returns only the reusable method fields for an explicitly collected case. */
export function caseMethodPrompt(item: Pick<CaseEntry, 'status' | 'direction' | 'scenario' | 'method' | 'avoid'>, labels: CaseMethodLabels): string {
  if (item.status !== '已收藏') return ''
  const separator = labels.separator ?? '：'
  return [
    [labels.direction, item.direction],
    [labels.scenario, item.scenario],
    [labels.method, item.method],
    [labels.avoid, item.avoid],
  ].filter(([, value]) => value?.trim()).map(([label, value]) => `${label}${separator}${separator === ':' ? ' ' : ''}${value.trim()}`).join('\n')
}

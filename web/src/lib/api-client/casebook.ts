import { jsonHeaders, requestJSON } from '@/lib/api-client/client'
import { assertProjectScope } from '@/lib/api-client/project-scope'

export type CaseStatus = '待确认' | '已收藏'

export interface CaseEntry {
  id: string
  title: string
  status: CaseStatus
  direction: string
  scenario: string
  method: string
  avoid: string
  source: string
  location: string
  quote: string
  prefix?: string
  suffix?: string
  path?: string
  revision?: string
  source_exists?: boolean
  registered?: boolean
  issue?: string
}

export interface CaseIndexEntry {
  id: string
  direction: string
  method: string
  status: CaseStatus
}

export interface CasebookSnapshot {
  project_id: string
  index: { content: string; revision: string }
  entries: CaseIndexEntry[]
  cases: CaseEntry[]
}

export interface CasebookToggleRequest {
  path: string
  case_revision?: string
  index_revision?: string
  revision?: string
  collected: boolean
}

export interface CasebookToggleResponse {
  project_id: string
  case: CaseEntry
  index: { content: string; revision: string }
  collected: boolean
}

function casebookURL(projectId: string, suffix = '') {
  return `/api/projects/${encodeURIComponent(projectId)}/book/cases${suffix}`
}

export async function getCasebook(projectId: string): Promise<CasebookSnapshot> {
  const response = await requestJSON<CasebookSnapshot>(casebookURL(projectId))
  assertProjectScope(projectId, response.project_id, 'Casebook response')
  return { ...response, entries: response.entries ?? [], cases: response.cases ?? [] }
}

export async function toggleCasebookCase(projectId: string, request: CasebookToggleRequest): Promise<CasebookToggleResponse> {
  const response = await requestJSON<CasebookToggleResponse>(casebookURL(projectId, request.collected ? '/favorite' : '/unfavorite'), {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify(request),
  })
  assertProjectScope(projectId, response.project_id, 'Casebook toggle response')
  return response
}

import { jsonHeaders, requestJSON } from './client'

export type ShortFictionProfileID = 'fanqie_short'

export interface ShortFictionGenerateRequest {
  workspace: string
  profile_id: ShortFictionProfileID
  target_path: string
  base_revision: string
  brief: string
}

/** Complete integrity-bound preview retained by the client until explicit confirmation. */
export interface ShortFictionCandidate {
  profile_id: ShortFictionProfileID
  profile_version: 'fanqie-short-v1'
  candidate_id: string
  workspace: string
  target_path: string
  base_revision: string
  brief: string
  source: string
  locale: string
  preview_markdown: string
  model_profile_id: string
  model: string
}

export interface ShortFictionConfirmRequest {
  candidate: ShortFictionCandidate
}

export type ShortFictionConfirmationStatus = 'written' | 'written_checkpoint_failed'
export type ShortFictionCheckpointStatus = 'created' | 'failed'

export interface ShortFictionConfirmationCheckpoint {
  version_id: string
  source: string
  path: string
  revision: string
}

interface ShortFictionConfirmationResultBase {
  candidate_id: string
  write_revision: string
  change_group_id: string
  change_set_id: string
  workspace_mutated: true
  retryable: false
}

export type ShortFictionConfirmationResult =
  | ShortFictionConfirmationResultBase & {
    status: 'written'
    checkpoint_status: 'created'
    checkpoint: ShortFictionConfirmationCheckpoint
  }
  | ShortFictionConfirmationResultBase & {
    status: 'written_checkpoint_failed'
    checkpoint_status: 'failed'
    checkpoint?: never
  }

export type ShortFictionErrorCode =
  | 'invalid_request'
  | 'candidate_mismatch'
  | 'invalid_source'
  | 'invalid_profile'
  | 'oversized'
  | 'generation_empty'
  | 'candidate_too_large'
  | 'generation_failed'
  | 'invalid_edit'
  | 'revision_conflict'
  | 'durability_pending'
  | 'workspace_conflict'
  | 'internal_error'

export interface ShortFictionErrorDetails extends Record<string, unknown> {
  workspace_mutated: boolean
  recovery_pending?: boolean
  retryable?: false
  target_path?: string
  recovery_target_path?: string
  write_revision?: string
  max_bytes?: number
}

export interface ShortFictionErrorResponse {
  error: string
  code: ShortFictionErrorCode
  details: ShortFictionErrorDetails
}

export async function generateShortFictionCandidate(request: ShortFictionGenerateRequest, locale: string) {
  return requestJSON<ShortFictionCandidate>('/api/short-fiction/candidates', {
    method: 'POST',
    headers: { ...jsonHeaders, 'X-Denova-Locale': locale },
    body: JSON.stringify(request),
  })
}

export async function confirmShortFictionCandidate(request: ShortFictionConfirmRequest, locale: string) {
  return requestJSON<ShortFictionConfirmationResult>('/api/short-fiction/candidates/confirm', {
    method: 'POST',
    headers: { ...jsonHeaders, 'X-Denova-Locale': locale },
    body: JSON.stringify(request),
  })
}

import {
  parseQualityMigrationPreview,
  parseQualityProfileDetail,
  parseQualityProfileSummaries,
  parseQualityProject,
} from '@/features/quality/contract-guards'
import type {
  QualityMigrationPreviewDTO,
  QualityMigrationPreviewRequestDTO,
  QualityProfileDetailDTO,
  QualityProfileSummaryDTO,
  QualityProjectDTO,
} from '@/features/quality/types'
import { jsonHeaders, requestJSON } from './client'

export async function getQualityProfiles(): Promise<QualityProfileSummaryDTO[]> {
  return parseQualityProfileSummaries(await requestJSON<unknown>('/api/quality/profiles'))
}

export async function getQualityProfile(profileID: string): Promise<QualityProfileDetailDTO> {
  return parseQualityProfileDetail(await requestJSON<unknown>(`/api/quality/profiles/${encodeURIComponent(profileID)}`))
}

export async function getQualityProject(): Promise<QualityProjectDTO> {
  return parseQualityProject(await requestJSON<unknown>('/api/quality/project'))
}

export async function previewQualityProjectMigration(
  request: QualityMigrationPreviewRequestDTO = {},
): Promise<QualityMigrationPreviewDTO> {
  return parseQualityMigrationPreview(await requestJSON<unknown>('/api/quality/project/migration-preview', {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify(request),
  }))
}

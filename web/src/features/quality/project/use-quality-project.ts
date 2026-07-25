import { useMutation, useQueries, useQuery } from '@tanstack/react-query'
import {
  getQualityProfile,
  getQualityProfiles,
  getQualityProject,
  previewQualityProjectMigration,
} from '@/lib/api-client/quality-projects'
import type { QualityProfileID } from '../types'

export const qualityProjectKeys = {
  all: ['quality-project'] as const,
  profiles: ['quality-project', 'profiles'] as const,
  profile: (profileID: QualityProfileID) => ['quality-project', 'profiles', profileID] as const,
  project: ['quality-project', 'current'] as const,
}

/** Starts independent catalog and workspace reads together. */
export function useQualityProjectOverview() {
  const [profiles, project] = useQueries({
    queries: [
      { queryKey: qualityProjectKeys.profiles, queryFn: getQualityProfiles },
      { queryKey: qualityProjectKeys.project, queryFn: getQualityProject },
    ],
  })
  return { profiles, project }
}

export function useQualityProfileDetail(profileID: QualityProfileID | null) {
  return useQuery({
    queryKey: profileID ? qualityProjectKeys.profile(profileID) : [...qualityProjectKeys.profiles, 'none'],
    queryFn: () => getQualityProfile(profileID as QualityProfileID),
    enabled: profileID !== null,
  })
}

export function useQualityMigrationPreview() {
  return useMutation({ mutationFn: () => previewQualityProjectMigration({}) })
}

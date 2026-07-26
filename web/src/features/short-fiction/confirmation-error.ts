import { APIError } from '@/lib/api-client/client'

interface DurabilityPendingFailure {
  workspaceMutated: boolean
  recoveryTargetPath?: string
}

/** Treats durability_pending as non-retryable even if optional details are malformed. */
export function getDurabilityPendingFailure(error: unknown): DurabilityPendingFailure | null {
  if (!(error instanceof APIError) || error.code !== 'durability_pending') return null
  const recoveryTargetPath = typeof error.details?.recovery_target_path === 'string'
    && error.details.recovery_target_path !== ''
    ? error.details.recovery_target_path
    : undefined
  return {
    workspaceMutated: error.details?.workspace_mutated === true,
    recoveryTargetPath,
  }
}

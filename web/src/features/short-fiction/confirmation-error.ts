import { APIError } from '@/lib/api-client/client'

interface DurabilityPendingFailure {
  workspaceMutated: boolean
}

/** Treats durability_pending as non-retryable even if optional details are malformed. */
export function getDurabilityPendingFailure(error: unknown): DurabilityPendingFailure | null {
  if (!(error instanceof APIError) || error.code !== 'durability_pending') return null
  return { workspaceMutated: error.details?.workspace_mutated === true }
}

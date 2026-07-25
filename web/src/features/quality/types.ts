export const QUALITY_PROFILE_IDS = ['long_serial', 'fanqie_short', 'zhihu_salt_short'] as const

export type QualityProfileID = typeof QUALITY_PROFILE_IDS[number]
export type QualityCompatibilityMode = 'managed_v1' | 'safe_read_open'
export type QualityManagedMutation = 'allowed' | 'blocked'
export type QualityWorkspaceKind = 'new' | 'current_denova' | 'legacy_nova'
export type QualityContractFailureKind = 'unsupported' | 'malformed'

export interface QualitySummaryText {
  zh: string
  en: string
}

export interface QualityLocalizedText {
  'zh-CN': string
  en: string
}

export interface QualityContract {
  kind: string
  version: 'v1'
  issued_at?: string
}

export interface QualityProvenance {
  source_id: string
  source_kind: string
  source_ref: string
  observed_at: string
  effective_from: string
  recorded_at: string
}

export interface QualitySpecMetadataDTO {
  contract_version: 'v1'
  spec_id: string
  revision: number
  sha256: string
}

/** GET /api/quality/profiles item. */
export interface QualityProfileSummaryDTO {
  profile_id: QualityProfileID
  contract_version: 'v1'
  source_sha256: string
  quality_spec: QualitySpecMetadataDTO
  access_mode: 'read_only_catalog'
  summary: QualitySummaryText
}

export interface QualityProfileSetting {
  id: string
  value: unknown
  provenance: QualityProvenance
  author_override_policy: {
    allowed: boolean
    allowed_scopes: string[]
    requires_explicit_confirmation: boolean
    unsupported_value_policy: string
  }
}

export interface QualityGoal {
  id: string
  contract: QualityContract
  description: QualityLocalizedText
  source: QualityProvenance
  purpose: QualityLocalizedText
  scope: {
    profile_ids: QualityProfileID[]
    operation_ids: string[]
    artifact_types: string[]
  }
  priority: 'must' | 'should' | 'could'
  evidence_requirement: {
    kind: string
    description: QualityLocalizedText
    minimum_count: number
    accepted_sources: string[]
  }
  value_contract: {
    type: string
    allowed_values: Array<string | number | boolean>
    unknown_value_policy: string
  }
  allowed_override_scopes: string[]
}

export interface QualityResolvedGoal {
  goal_id: string
  value: string | number | boolean
  winning_layer: 'profile_defaults' | 'project_overrides' | 'task_overrides' | 'operation_confirmation'
  provenance_chain: unknown[]
  author_confirmation_id: string
}

export interface QualitySpec {
  contract: QualityContract
  spec_id: string
  revision: number
  profile_id: QualityProfileID
  goal_catalog: QualityGoal[]
  layers: Record<string, unknown>
  candidate_changes: unknown[]
  resolution: {
    merge_order: string[]
    unknown_or_unsupported_value_policy: string
    validator_contract: string
    validated_at: string
    resolved_goals: QualityResolvedGoal[]
  }
}

export interface QualityPublicProfile {
  contract: QualityContract
  profile_id: QualityProfileID
  display_name: QualityLocalizedText
  engine_contract: {
    engine_id: string
    contract_version: 'v1'
    implementation_branching: string
  }
  profile_provenance: QualityProvenance
  identity_policy: {
    unknown_profile_id: string
    silent_fallback: boolean
    model_mutation: string
  }
  settings: {
    required_artifacts: QualityProfileSetting[]
    required_capabilities: QualityProfileSetting[]
    candidate_policy: QualityProfileSetting[]
    review_rubric: QualityProfileSetting[]
    export_config: QualityProfileSetting[]
  }
  walkthrough: {
    operation_id: string
    artifact_ref: string
    description: QualityLocalizedText
    evaluation_focus: string[]
  }
  quality_spec: QualitySpec
}

/** GET /api/quality/profiles/:profile_id response. */
export interface QualityProfileDetailDTO extends QualityProfileSummaryDTO {
  profile: QualityPublicProfile
}

export interface QualityFeatureDTO {
  id: string
  version: string
  required: boolean
}

export interface QualityIssueDTO {
  code: string
  path?: string
  field?: string
  blocking: boolean
}

/** GET /api/quality/project response. */
export interface QualityProjectDTO {
  resource_id: 'current'
  active_root?: string
  mode: QualityCompatibilityMode
  managed_mutation: QualityManagedMutation
  marker: {
    present: boolean
    schema_version?: number
    features: QualityFeatureDTO[]
  }
  issues: QualityIssueDTO[]
  issue_truncation: { total: number; returned: number; truncated: boolean }
  unknown_optional_features: string[]
  legacy_conflicts: string[]
}

/** POST /api/quality/project/migration-preview request. */
export interface QualityMigrationPreviewRequestDTO {
  offset?: number
  limit?: number
}

export interface QualityPageResult<T> {
  items: T[]
  total: number
  truncated: boolean
}

/** POST /api/quality/project/migration-preview response. */
export interface QualityMigrationPreviewDTO {
  resource_id: 'current'
  digest: string
  workspace_kind: QualityWorkspaceKind
  source_root?: string
  target_root: string
  current_schema_version: number
  target_schema_version: number
  features: QualityFeatureDTO[]
  compatibility: QualityProjectDTO
  totals: { entries: number; operations: number; conflicts: number }
  page: { offset: number; limit: number }
  entries: QualityPageResult<{
    source: string
    destination: string
    node_type: string
    source_category: string
    destination_category: string
    size: number
    sha256: string
  }>
  operations: QualityPageResult<{ kind: string; source?: string; destination: string }>
  conflicts: QualityPageResult<{ code: string; path?: string; destination?: string; field?: string }>
}

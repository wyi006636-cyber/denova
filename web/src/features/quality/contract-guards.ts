import {
  QUALITY_PROFILE_IDS,
  type QualityContractFailureKind,
  type QualityFeatureDTO,
  type QualityGoal,
  type QualityIssueDTO,
  type QualityMigrationPreviewDTO,
  type QualityPageResult,
  type QualityProfileDetailDTO,
  type QualityProfileID,
  type QualityProfileSetting,
  type QualityProfileSummaryDTO,
  type QualityProjectDTO,
  type QualityProvenance,
  type QualityResolvedGoal,
} from './types'

const PROFILE_IDS = new Set<string>(QUALITY_PROFILE_IDS)
const WINNING_LAYERS = new Set(['profile_defaults', 'project_overrides', 'task_overrides', 'operation_confirmation'])

/** Stable, payload-free error used when a public Quality response cannot be rendered safely. */
export class QualityContractError extends Error {
  readonly kind: QualityContractFailureKind

  constructor(kind: QualityContractFailureKind) {
    super(kind === 'unsupported' ? 'quality response contract is unsupported' : 'quality response is malformed')
    this.name = 'QualityContractError'
    this.kind = kind
  }
}

export function parseQualityProfileSummaries(value: unknown): QualityProfileSummaryDTO[] {
  if (!Array.isArray(value)) malformed()
  if (value.length === 0) return []
  const result = value.map(parseProfileSummary)
  if (result.length !== QUALITY_PROFILE_IDS.length) malformed()
  const ids = new Set(result.map((item) => item.profile_id))
  if (ids.size !== QUALITY_PROFILE_IDS.length || QUALITY_PROFILE_IDS.some((id) => !ids.has(id))) malformed()
  return result
}

export function parseQualityProfileDetail(value: unknown): QualityProfileDetailDTO {
  const record = asRecord(value)
  const summary = parseProfileSummary(record)
  const profile = asRecord(record.profile)
  assertContract(profile.contract, 'denova.quality-profile')
  const profileID = asProfileID(profile.profile_id)
  if (profileID !== summary.profile_id) malformed()
  assertLocalizedText(profile.display_name)
  const engine = asRecord(profile.engine_contract)
  asString(engine.engine_id)
  assertV1(engine.contract_version)
  asString(engine.implementation_branching)
  assertProvenance(profile.profile_provenance)
  const identityPolicy = asRecord(profile.identity_policy)
  asString(identityPolicy.unknown_profile_id)
  asBoolean(identityPolicy.silent_fallback)
  asString(identityPolicy.model_mutation)

  const settings = asRecord(profile.settings)
  for (const key of ['required_artifacts', 'required_capabilities', 'candidate_policy', 'review_rubric', 'export_config']) {
    asArray(settings[key]).forEach(assertProfileSetting)
  }
  const walkthrough = asRecord(profile.walkthrough)
  asString(walkthrough.operation_id)
  asString(walkthrough.artifact_ref)
  assertLocalizedText(walkthrough.description)
  asStringArray(walkthrough.evaluation_focus)

  const spec = asRecord(profile.quality_spec)
  assertContract(spec.contract, 'denova.quality-spec')
  const specID = asString(spec.spec_id)
  const revision = asNonNegativeInteger(spec.revision, 1)
  if (asProfileID(spec.profile_id) !== profileID) malformed()
  asArray(spec.goal_catalog).forEach(assertQualityGoal)
  asRecord(spec.layers)
  asArray(spec.candidate_changes)
  const resolution = asRecord(spec.resolution)
  asStringArray(resolution.merge_order)
  asString(resolution.unknown_or_unsupported_value_policy)
  asString(resolution.validator_contract)
  asString(resolution.validated_at)
  asArray(resolution.resolved_goals).forEach(assertResolvedGoal)

  if (summary.quality_spec.spec_id !== specID || summary.quality_spec.revision !== revision) malformed()
  if (summary.quality_spec.contract_version !== asRecord(spec.contract).version) malformed()
  return value as QualityProfileDetailDTO
}

export function parseQualityProject(value: unknown): QualityProjectDTO {
  const record = asRecord(value)
  if (record.resource_id !== 'current') malformed()
  optionalString(record.active_root)
  if (record.mode !== 'managed_v1' && record.mode !== 'safe_read_open') malformed()
  if (record.managed_mutation !== 'allowed' && record.managed_mutation !== 'blocked') malformed()
  const marker = asRecord(record.marker)
  asBoolean(marker.present)
  optionalInteger(marker.schema_version)
  asArray(marker.features).forEach(assertFeature)
  asArray(record.issues).forEach(assertIssue)
  assertTruncation(record.issue_truncation)
  asStringArray(record.unknown_optional_features)
  asStringArray(record.legacy_conflicts)
  return value as QualityProjectDTO
}

export function parseQualityMigrationPreview(value: unknown): QualityMigrationPreviewDTO {
  const record = asRecord(value)
  if (record.resource_id !== 'current') malformed()
  asString(record.digest)
  if (record.workspace_kind !== 'new' && record.workspace_kind !== 'current_denova' && record.workspace_kind !== 'legacy_nova') malformed()
  optionalString(record.source_root)
  asString(record.target_root)
  asNonNegativeInteger(record.current_schema_version)
  asNonNegativeInteger(record.target_schema_version)
  asArray(record.features).forEach(assertFeature)
  parseQualityProject(record.compatibility)
  const totals = asRecord(record.totals)
  asNonNegativeInteger(totals.entries)
  asNonNegativeInteger(totals.operations)
  asNonNegativeInteger(totals.conflicts)
  const page = asRecord(record.page)
  asNonNegativeInteger(page.offset)
  asNonNegativeInteger(page.limit, 1)
  assertPage(record.entries, assertPreviewEntry)
  assertPage(record.operations, assertPreviewOperation)
  assertPage(record.conflicts, assertPreviewConflict)
  return value as QualityMigrationPreviewDTO
}

function parseProfileSummary(value: unknown): QualityProfileSummaryDTO {
  const record = asRecord(value)
  const profileID = asProfileID(record.profile_id)
  assertV1(record.contract_version)
  asString(record.source_sha256)
  const spec = asRecord(record.quality_spec)
  assertV1(spec.contract_version)
  asString(spec.spec_id)
  asNonNegativeInteger(spec.revision, 1)
  asString(spec.sha256)
  if (record.access_mode !== 'read_only_catalog') malformed()
  const summary = asRecord(record.summary)
  asString(summary.zh)
  asString(summary.en)
  return { ...record, profile_id: profileID } as unknown as QualityProfileSummaryDTO
}

function assertContract(value: unknown, kind: string) {
  const contract = asRecord(value)
  if (contract.kind !== kind) malformed()
  assertV1(contract.version)
  optionalString(contract.issued_at)
}

function assertV1(value: unknown) {
  if (typeof value !== 'string') malformed()
  if (value !== 'v1') unsupported()
}

function assertLocalizedText(value: unknown) {
  const record = asRecord(value)
  asString(record['zh-CN'])
  asString(record.en)
}

function assertProvenance(value: unknown): asserts value is QualityProvenance {
  const record = asRecord(value)
  for (const key of ['source_id', 'source_kind', 'source_ref', 'observed_at', 'effective_from', 'recorded_at']) asString(record[key])
}

function assertProfileSetting(value: unknown): asserts value is QualityProfileSetting {
  const record = asRecord(value)
  asString(record.id)
  assertJsonValue(record.value)
  assertProvenance(record.provenance)
  const policy = asRecord(record.author_override_policy)
  asBoolean(policy.allowed)
  asStringArray(policy.allowed_scopes)
  asBoolean(policy.requires_explicit_confirmation)
  asString(policy.unsupported_value_policy)
}

function assertQualityGoal(value: unknown): asserts value is QualityGoal {
  const goal = asRecord(value)
  asString(goal.id)
  assertContract(goal.contract, 'denova.quality-goal')
  assertLocalizedText(goal.description)
  assertProvenance(goal.source)
  assertLocalizedText(goal.purpose)
  const scope = asRecord(goal.scope)
  asArray(scope.profile_ids).forEach(asProfileID)
  asStringArray(scope.operation_ids)
  asStringArray(scope.artifact_types)
  if (goal.priority !== 'must' && goal.priority !== 'should' && goal.priority !== 'could') malformed()
  const evidence = asRecord(goal.evidence_requirement)
  asString(evidence.kind)
  assertLocalizedText(evidence.description)
  asNonNegativeInteger(evidence.minimum_count, 1)
  asStringArray(evidence.accepted_sources)
  const valueContract = asRecord(goal.value_contract)
  asString(valueContract.type)
  asArray(valueContract.allowed_values).forEach((item) => {
    if (typeof item !== 'string' && typeof item !== 'number' && typeof item !== 'boolean') malformed()
  })
  asString(valueContract.unknown_value_policy)
  asStringArray(goal.allowed_override_scopes)
}

function assertResolvedGoal(value: unknown): asserts value is QualityResolvedGoal {
  const goal = asRecord(value)
  asString(goal.goal_id)
  if (typeof goal.value !== 'string' && typeof goal.value !== 'number' && typeof goal.value !== 'boolean') malformed()
  if (typeof goal.winning_layer !== 'string' || !WINNING_LAYERS.has(goal.winning_layer)) malformed()
  asArray(goal.provenance_chain)
  asString(goal.author_confirmation_id)
}

function assertFeature(value: unknown): asserts value is QualityFeatureDTO {
  const feature = asRecord(value)
  asString(feature.id)
  asString(feature.version)
  asBoolean(feature.required)
}

function assertIssue(value: unknown): asserts value is QualityIssueDTO {
  const issue = asRecord(value)
  asString(issue.code)
  optionalString(issue.path)
  optionalString(issue.field)
  asBoolean(issue.blocking)
}

function assertTruncation(value: unknown) {
  const truncation = asRecord(value)
  asNonNegativeInteger(truncation.total)
  asNonNegativeInteger(truncation.returned)
  asBoolean(truncation.truncated)
}

function assertPage<T>(value: unknown, assertItem: (item: unknown) => asserts item is T): asserts value is QualityPageResult<T> {
  const page = asRecord(value)
  asArray(page.items).forEach(assertItem)
  asNonNegativeInteger(page.total)
  asBoolean(page.truncated)
}

function assertPreviewEntry(value: unknown): asserts value is QualityMigrationPreviewDTO['entries']['items'][number] {
  const entry = asRecord(value)
  for (const key of ['source', 'destination', 'node_type', 'source_category', 'destination_category', 'sha256']) asString(entry[key])
  asNonNegativeInteger(entry.size)
}

function assertPreviewOperation(value: unknown): asserts value is QualityMigrationPreviewDTO['operations']['items'][number] {
  const operation = asRecord(value)
  asString(operation.kind)
  optionalString(operation.source)
  asString(operation.destination)
}

function assertPreviewConflict(value: unknown): asserts value is QualityMigrationPreviewDTO['conflicts']['items'][number] {
  const conflict = asRecord(value)
  asString(conflict.code)
  optionalString(conflict.path)
  optionalString(conflict.destination)
  optionalString(conflict.field)
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) malformed()
  return value as Record<string, unknown>
}

function asArray(value: unknown): unknown[] {
  if (!Array.isArray(value)) malformed()
  return value
}

function asString(value: unknown): string {
  if (typeof value !== 'string') malformed()
  return value
}

function optionalString(value: unknown) {
  if (value !== undefined) asString(value)
}

function asStringArray(value: unknown): string[] {
  return asArray(value).map(asString)
}

function asBoolean(value: unknown): boolean {
  if (typeof value !== 'boolean') malformed()
  return value
}

function asNonNegativeInteger(value: unknown, minimum = 0): number {
  if (typeof value !== 'number' || !Number.isInteger(value) || value < minimum) malformed()
  return value
}

function optionalInteger(value: unknown) {
  if (value !== undefined) asNonNegativeInteger(value)
}

function asProfileID(value: unknown): QualityProfileID {
  if (typeof value !== 'string' || !PROFILE_IDS.has(value)) malformed()
  return value as QualityProfileID
}

function assertJsonValue(value: unknown) {
  if (value === null || typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return
  if (Array.isArray(value)) {
    value.forEach(assertJsonValue)
    return
  }
  const record = asRecord(value)
  Object.values(record).forEach(assertJsonValue)
}

function malformed(): never {
  throw new QualityContractError('malformed')
}

function unsupported(): never {
  throw new QualityContractError('unsupported')
}

export function qualityProfileSummariesFixture() {
  return [
    profileSummary('long_serial', '长篇连载', 'Long serial'),
    profileSummary('fanqie_short', '番茄短篇', 'Fanqie short'),
    profileSummary('zhihu_salt_short', '盐选短篇', 'Zhihu Salt short'),
  ]
}

export function qualityProfileDetailFixture(profileID = 'long_serial', longText = '') {
  const summaries = qualityProfileSummariesFixture()
  const summary = summaries.find((item) => item.profile_id === profileID) ?? summaries[0]
  const label = summary.summary
  const goalID = profileID === 'long_serial' ? 'qg.serial.continuity' : `qg.${profileID}.opening`
  const description = longText || (profileID === 'long_serial' ? '与既有角色状态、设定和伏笔保持连续。' : '用清晰、可信的开篇建立阅读期待。')
  return {
    ...summary,
    profile: {
      contract: { kind: 'denova.quality-profile', version: 'v1', issued_at: '2026-07-21T01:00:00Z' },
      profile_id: summary.profile_id,
      display_name: { 'zh-CN': label.zh, en: label.en },
      engine_contract: {
        engine_id: 'denova.shared-quality-engine',
        contract_version: 'v1',
        implementation_branching: 'profile_data_only',
      },
      profile_provenance: provenance('approved_plan', 'ADR-PROFILE-001'),
      identity_policy: {
        unknown_profile_id: 'reject_explicitly',
        silent_fallback: false,
        model_mutation: 'candidate_only',
      },
      settings: {
        required_artifacts: [setting('artifacts.author_context', ['outline', 'manuscript'])],
        required_capabilities: [setting('capabilities.continuity', ['continuity.check'])],
        candidate_policy: [setting('candidates.default_count', 1)],
        review_rubric: [setting('review.story_consistency', ['state_consistency'])],
        export_config: [setting('export.author_document', { format: 'markdown' })],
      },
      walkthrough: {
        operation_id: 'plan_current_work',
        artifact_ref: 'current-work',
        description: { 'zh-CN': '以作者已确认的标准规划当前作品。', en: 'Plan the current work with author-confirmed standards.' },
        evaluation_focus: [goalID],
      },
      quality_spec: {
        contract: { kind: 'denova.quality-spec', version: 'v1', issued_at: '2026-07-21T01:10:00Z' },
        spec_id: summary.quality_spec.spec_id,
        revision: 1,
        profile_id: summary.profile_id,
        goal_catalog: [{
          id: goalID,
          contract: { kind: 'denova.quality-goal', version: 'v1' },
          description: { 'zh-CN': description, en: longText || 'Keep the work continuous with established character state, lore, and setups.' },
          source: provenance('profile_contract', 'ADR-PROFILE-001#profile-intent'),
          purpose: { 'zh-CN': '避免人物、因果与作品方向在创作过程中无依据漂移。', en: 'Prevent unsupported drift in characters, causality, and creative direction.' },
          scope: { profile_ids: [summary.profile_id], operation_ids: ['plan_current_work'], artifact_types: ['author_plan'] },
          priority: 'must',
          evidence_requirement: {
            kind: 'source_crosscheck',
            description: { 'zh-CN': '至少引用一处作品文本与一处设定依据。', en: 'Cite at least one manuscript span and one planning source.' },
            minimum_count: 2,
            accepted_sources: ['manuscript', 'outline', 'lore'],
          },
          value_contract: { type: 'enum', allowed_values: ['normal', 'strict'], unknown_value_policy: 'reject_explicitly' },
          allowed_override_scopes: ['project', 'task', 'operation_confirmation'],
        }],
        layers: {
          profile_defaults: [],
          project_overrides: [],
          task_overrides: [],
          operation_confirmation: { operation_id: 'plan_current_work', authorization: {}, overrides: [] },
        },
        candidate_changes: [],
        resolution: {
          merge_order: ['profile_defaults', 'project_overrides', 'task_overrides', 'operation_confirmation'],
          unknown_or_unsupported_value_policy: 'reject_explicitly',
          validator_contract: 'quality-spec-resolution-v1',
          validated_at: '2026-07-21T01:20:00Z',
          resolved_goals: [{
            goal_id: goalID,
            value: 'strict',
            winning_layer: 'project_overrides',
            provenance_chain: [],
            author_confirmation_id: 'confirm-author-001',
          }],
        },
      },
    },
  }
}

export function qualityProjectFixture(overrides: Record<string, unknown> = {}) {
  return {
    resource_id: 'current',
    active_root: '.denova',
    mode: 'managed_v1',
    managed_mutation: 'allowed',
    marker: {
      present: true,
      schema_version: 1,
      features: [
        { id: 'fts_projection', version: '1.0.0', required: false },
        { id: 'quality_harness', version: '1.0.0', required: true },
      ],
    },
    issues: [],
    issue_truncation: { total: 0, returned: 0, truncated: false },
    unknown_optional_features: [],
    legacy_conflicts: [],
    ...overrides,
  }
}

export function qualityPreviewFixture(overrides: Record<string, unknown> = {}) {
  return {
    resource_id: 'current',
    digest: 'c'.repeat(64),
    workspace_kind: 'legacy_nova',
    source_root: '.nova',
    target_root: '.denova',
    current_schema_version: 0,
    target_schema_version: 1,
    features: [{ id: 'quality_harness', version: '1.0.0', required: true }],
    compatibility: qualityProjectFixture({
      mode: 'safe_read_open',
      managed_mutation: 'blocked',
      issues: [{ code: 'workspace_schema_missing', path: '.denova/workspace.json', field: 'schema_version', blocking: true }],
      issue_truncation: { total: 1, returned: 1, truncated: false },
    }),
    totals: { entries: 1, operations: 2, conflicts: 1 },
    page: { offset: 0, limit: 100 },
    entries: {
      items: [{
        source: '.nova/profile.json',
        destination: '.denova/profile.json',
        node_type: 'file',
        source_category: 'formal',
        destination_category: 'formal',
        size: 128,
        sha256: 'd'.repeat(64),
      }],
      total: 1,
      truncated: false,
    },
    operations: {
      items: [
        { kind: 'copy_to_current_root', source: '.nova/profile.json', destination: '.denova/profile.json' },
        { kind: 'create_marker', destination: '.denova/workspace.json' },
      ],
      total: 2,
      truncated: false,
    },
    conflicts: {
      items: [{ code: 'destination_exists', destination: '.denova/profile.json', field: 'destination' }],
      total: 1,
      truncated: false,
    },
    ...overrides,
  }
}

function profileSummary(profileID: string, zh: string, en: string) {
  return {
    profile_id: profileID,
    contract_version: 'v1',
    source_sha256: 'a'.repeat(64),
    quality_spec: {
      contract_version: 'v1',
      spec_id: `qs-${profileID.replaceAll('_', '-')}`,
      revision: 1,
      sha256: 'b'.repeat(64),
    },
    access_mode: 'read_only_catalog',
    summary: { zh, en },
  }
}

function provenance(sourceKind: string, sourceRef: string) {
  return {
    source_id: 'profile-source-20260721',
    source_kind: sourceKind,
    source_ref: sourceRef,
    observed_at: '2026-07-21',
    effective_from: '2026-07-21',
    recorded_at: '2026-07-21T01:00:00Z',
  }
}

function setting(id: string, value: unknown) {
  return {
    id,
    value,
    provenance: provenance('approved_plan', 'ADR-PROFILE-001'),
    author_override_policy: {
      allowed: true,
      allowed_scopes: ['project', 'task'],
      requires_explicit_confirmation: true,
      unsupported_value_policy: 'reject_explicitly',
    },
  }
}

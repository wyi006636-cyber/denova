import { afterEach, describe, expect, it, vi } from 'vitest'
import { APIError } from './client'
import * as qualityProjects from './quality-projects'

describe('quality project API client', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('uses the four frozen read-only and preview endpoints exactly', async () => {
    const requests: Array<{ url: string; method: string; body: string | null }> = []
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      requests.push({
        url: String(input),
        method: init?.method ?? 'GET',
        body: typeof init?.body === 'string' ? init.body : null,
      })
      const url = String(input)
      if (url === '/api/quality/profiles') return jsonResponse(profileSummaries())
      if (url.startsWith('/api/quality/profiles/')) return jsonResponse(profileDetail())
      if (url === '/api/quality/project') return jsonResponse(projectFixture())
      return jsonResponse(previewFixture())
    }))

    await qualityProjects.getQualityProfiles()
    await qualityProjects.getQualityProfile('long serial/with slash')
    await qualityProjects.getQualityProject()
    await qualityProjects.previewQualityProjectMigration()

    expect(requests).toEqual([
      { url: '/api/quality/profiles', method: 'GET', body: null },
      { url: '/api/quality/profiles/long%20serial%2Fwith%20slash', method: 'GET', body: null },
      { url: '/api/quality/project', method: 'GET', body: null },
      { url: '/api/quality/project/migration-preview', method: 'POST', body: '{}' },
    ])
  })

  it('sends only the bounded migration preview paging body', async () => {
    const fetchMock = vi.fn(async () => jsonResponse(previewFixture()))
    vi.stubGlobal('fetch', fetchMock)

    await qualityProjects.previewQualityProjectMigration({ offset: 25, limit: 50 })

    expect(fetchMock).toHaveBeenCalledWith('/api/quality/project/migration-preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{"offset":25,"limit":50}',
    })
  })

  it('preserves APIError status and stable backend code', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({
      code: 'quality_no_workspace',
      message: 'No workspace is currently open.',
    }, 409)))

    const error = await qualityProjects.getQualityProject().catch((reason) => reason)

    expect(error).toBeInstanceOf(APIError)
    expect(error).toMatchObject({ status: 409, code: 'quality_no_workspace' })
  })

  it('does not accept a malformed non-string error code as a stable code', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ code: 409, message: ['unsafe'] }, 500)))

    const error = await qualityProjects.getQualityProfiles().catch((reason) => reason)

    expect(error).toBeInstanceOf(APIError)
    expect(error).toMatchObject({ status: 500, code: undefined })
  })

  it('exports no install, mutation, run, decision, preference, or finalization client', () => {
    expect(Object.keys(qualityProjects).sort()).toEqual([
      'getQualityProfile',
      'getQualityProfiles',
      'getQualityProject',
      'previewQualityProjectMigration',
    ])
  })
})

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function profileSummaries() {
  return ['long_serial', 'fanqie_short', 'zhihu_salt_short'].map((profileID) => ({
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
    summary: { zh: profileID, en: profileID },
  }))
}

function profileDetail() {
  return {
    ...profileSummaries()[0],
    profile: {
      contract: { kind: 'denova.quality-profile', version: 'v1', issued_at: '2026-07-21T01:00:00Z' },
      profile_id: 'long_serial',
      display_name: { 'zh-CN': '长篇连载', en: 'Long serial' },
      engine_contract: {
        engine_id: 'denova.shared-quality-engine',
        contract_version: 'v1',
        implementation_branching: 'profile_data_only',
      },
      profile_provenance: provenance(),
      identity_policy: {
        unknown_profile_id: 'reject_explicitly',
        silent_fallback: false,
        model_mutation: 'candidate_only',
      },
      settings: {
        required_artifacts: [setting('artifacts.chapter_context', ['chapters/0012.md'])],
        required_capabilities: [setting('capabilities.serial_continuity', ['continuity.check'])],
        candidate_policy: [setting('candidates.chapter_draft_count', 1)],
        review_rubric: [setting('review.serial_continuity', ['state_consistency'])],
        export_config: [setting('export.chapter_markdown', { format: 'markdown' })],
      },
      walkthrough: {
        operation_id: 'draft_chapter_12',
        artifact_ref: 'chapters/0012.md',
        description: { 'zh-CN': '起草第 12 章。', en: 'Draft chapter 12.' },
        evaluation_focus: ['qg.serial.continuity'],
      },
      quality_spec: {
        contract: { kind: 'denova.quality-spec', version: 'v1', issued_at: '2026-07-21T01:10:00Z' },
        spec_id: 'qs-long-serial',
        revision: 1,
        profile_id: 'long_serial',
        goal_catalog: [{
          id: 'qg.serial.continuity',
          contract: { kind: 'denova.quality-goal', version: 'v1' },
          description: { 'zh-CN': '保持连续。', en: 'Remain continuous.' },
          source: provenance(),
          purpose: { 'zh-CN': '避免状态漂移。', en: 'Prevent state drift.' },
          scope: { profile_ids: ['long_serial'], operation_ids: ['draft_chapter_12'], artifact_types: ['chapter_draft'] },
          priority: 'must',
          evidence_requirement: {
            kind: 'source_crosscheck',
            description: { 'zh-CN': '引用正文与设定。', en: 'Cite prose and lore.' },
            minimum_count: 2,
            accepted_sources: ['manuscript', 'lore'],
          },
          value_contract: { type: 'enum', allowed_values: ['normal', 'strict'], unknown_value_policy: 'reject_explicitly' },
          allowed_override_scopes: ['project', 'task', 'operation_confirmation'],
        }],
        layers: {
          profile_defaults: [],
          project_overrides: [],
          task_overrides: [],
          operation_confirmation: { operation_id: 'draft_chapter_12', authorization: {}, overrides: [] },
        },
        candidate_changes: [],
        resolution: {
          merge_order: ['profile_defaults', 'project_overrides', 'task_overrides', 'operation_confirmation'],
          unknown_or_unsupported_value_policy: 'reject_explicitly',
          validator_contract: 'quality-spec-resolution-v1',
          validated_at: '2026-07-21T01:20:00Z',
          resolved_goals: [{
            goal_id: 'qg.serial.continuity',
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

function provenance() {
  return {
    source_id: 'plan-p0-t04-20260721',
    source_kind: 'approved_plan',
    source_ref: 'ADR-PROFILE-001',
    observed_at: '2026-07-21',
    effective_from: '2026-07-21',
    recorded_at: '2026-07-21T01:00:00Z',
  }
}

function setting(id: string, value: unknown) {
  return {
    id,
    value,
    provenance: provenance(),
    author_override_policy: {
      allowed: true,
      allowed_scopes: ['project', 'task'],
      requires_explicit_confirmation: true,
      unsupported_value_policy: 'reject_explicitly',
    },
  }
}

function projectFixture() {
  return {
    resource_id: 'current',
    active_root: '.denova',
    mode: 'managed_v1',
    managed_mutation: 'allowed',
    marker: { present: true, schema_version: 1, features: [] },
    issues: [],
    issue_truncation: { total: 0, returned: 0, truncated: false },
    unknown_optional_features: [],
    legacy_conflicts: [],
  }
}

function previewFixture() {
  return {
    resource_id: 'current',
    digest: 'c'.repeat(64),
    workspace_kind: 'current_denova',
    source_root: '.denova',
    target_root: '.denova',
    current_schema_version: 1,
    target_schema_version: 1,
    features: [],
    compatibility: projectFixture(),
    totals: { entries: 0, operations: 0, conflicts: 0 },
    page: { offset: 0, limit: 100 },
    entries: { items: [], total: 0, truncated: false },
    operations: { items: [], total: 0, truncated: false },
    conflicts: { items: [], total: 0, truncated: false },
  }
}

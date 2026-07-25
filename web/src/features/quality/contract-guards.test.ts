import { describe, expect, it } from 'vitest'
import {
  parseQualityProfileDetail,
  parseQualityProfileSummaries,
  QualityContractError,
} from './contract-guards'

describe('quality contract guards', () => {
  it('accepts only the exhaustive v1 read-only Profile catalog', () => {
    const result = parseQualityProfileSummaries(profileSummaries())

    expect(result.map((item) => [item.profile_id, item.access_mode])).toEqual([
      ['long_serial', 'read_only_catalog'],
      ['fanqie_short', 'read_only_catalog'],
      ['zhihu_salt_short', 'read_only_catalog'],
    ])
  })

  it('accepts a detail only when Profile and nested QualitySpec contracts are exact v1', () => {
    const result = parseQualityProfileDetail(profileDetail())

    expect(result.profile.contract).toMatchObject({ kind: 'denova.quality-profile', version: 'v1' })
    expect(result.profile.quality_spec.contract).toMatchObject({ kind: 'denova.quality-spec', version: 'v1' })
    expect(result.profile.quality_spec.goal_catalog[0]).toMatchObject({
      id: 'qg.serial.continuity',
      priority: 'must',
    })
  })

  it.each([
    ['Profile summary', () => ({ ...profileSummaries()[0], contract_version: 'v2' })],
    ['nested QualitySpec summary', () => ({ ...profileSummaries()[0], quality_spec: { ...profileSummaries()[0].quality_spec, contract_version: 'v2' } })],
    ['Profile detail', () => {
      const detail = profileDetail()
      detail.profile.contract.version = 'v2'
      return detail
    }],
    ['nested QualitySpec detail', () => {
      const detail = profileDetail()
      detail.profile.quality_spec.contract.version = 'v2'
      return detail
    }],
  ])('classifies unknown/newer %s contracts as unsupported', (_name, fixture) => {
    const parse = _name.includes('detail') ? parseQualityProfileDetail : (value: unknown) => parseQualityProfileSummaries([value])

    expect(() => parse(fixture())).toThrowError(QualityContractError)
    try {
      parse(fixture())
    } catch (error) {
      expect(error).toMatchObject({ kind: 'unsupported' })
    }
  })

  it.each([
    ['non-array catalog', { profiles: profileSummaries() }],
    ['wrong source hash type', [{ ...profileSummaries()[0], source_sha256: 42 }]],
    ['wrong revision type', [{ ...profileSummaries()[0], quality_spec: { ...profileSummaries()[0].quality_spec, revision: '1' } }]],
    ['wrong access mode', [{ ...profileSummaries()[0], access_mode: 'installed' }]],
  ])('classifies malformed %s as a bounded contract error', (_name, fixture) => {
    expectMalformed(() => parseQualityProfileSummaries(fixture))
  })

  it('rejects a malformed author-facing goal instead of displaying partial contract data', () => {
    const detail = profileDetail()
    detail.profile.quality_spec.goal_catalog[0].purpose = { 'zh-CN': '有目的' } as never

    expectMalformed(() => parseQualityProfileDetail(detail))
  })
})

function expectMalformed(run: () => unknown) {
  expect(run).toThrowError(QualityContractError)
  try {
    run()
  } catch (error) {
    expect(error).toMatchObject({ kind: 'malformed' })
    expect((error as Error).message).not.toContain('source_sha256')
  }
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

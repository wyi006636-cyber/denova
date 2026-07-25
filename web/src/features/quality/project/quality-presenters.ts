import type { TFunction } from 'i18next'
import type { QualityLocalizedText, QualityProjectDTO, QualitySummaryText } from '../types'

export function localizedText(value: QualityLocalizedText | QualitySummaryText, language: string) {
  if (language.toLowerCase().startsWith('zh')) return 'zh-CN' in value ? value['zh-CN'] : value.zh
  return value.en
}

export function projectStatus(project: QualityProjectDTO, t: TFunction) {
  if (project.mode === 'managed_v1' && project.managed_mutation === 'allowed') {
    return { label: t('quality.project.status.ready'), tone: 'success' as const }
  }
  return { label: t('quality.project.status.readOnly'), tone: 'warning' as const }
}

export function humanizeToken(value: string) {
  return value.replace(/^qg\./, '').replace(/[._-]+/g, ' ').trim()
}

export function formatSettingValue(value: unknown): string {
  if (typeof value === 'string') return humanizeToken(value)
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  if (Array.isArray(value)) return value.map(formatSettingValue).join(' · ')
  if (value && typeof value === 'object') {
    return Object.entries(value)
      .map(([key, nested]) => `${humanizeToken(key)}: ${formatSettingValue(nested)}`)
      .join(' · ')
  }
  return '—'
}

export function shortDigest(value: string) {
  return value.length > 12 ? `${value.slice(0, 12)}…` : value
}

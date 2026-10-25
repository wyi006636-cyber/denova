import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, CheckCircle2, ChevronDown, ChevronRight, FileWarning, HeartPulse } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { getLoreHealth, type LoreHealthIssue, type LoreHealthReport } from '@/lib/api-client/lore'

export function LoreHealthDialog({ projectId, open, onOpenChange, onOpenFile }: { projectId: string; open: boolean; onOpenChange: (open: boolean) => void; onOpenFile?: (path: string) => void }) {
  const { t } = useTranslation()
  const [report, setReport] = useState<LoreHealthReport | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({ unused_items: true })
  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoading(true)
    setError('')
    void getLoreHealth(projectId).then(next => {
      if (!cancelled) setReport(next)
    }).catch(() => {
      if (!cancelled) setError('load-failed')
    }).finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [open, projectId])
  const groups = useMemo(() => report ? [
    { key: 'duplicate_names', title: t('lore.health.duplicate'), icon: AlertTriangle, items: report.duplicate_names },
    { key: 'empty_items', title: t('lore.health.empty'), icon: FileWarning, items: report.empty_items },
    { key: 'broken_references', title: t('lore.health.broken'), icon: AlertTriangle, items: report.broken_references },
    { key: 'unused_items', title: t('lore.health.unused'), icon: HeartPulse, items: report.unused_items },
  ] : [], [report, t])
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('lore.health.title')}</DialogTitle>
          <DialogDescription>{t('lore.health.description')}</DialogDescription>
        </DialogHeader>
        {loading ? <p className="py-8 text-center text-sm text-muted-foreground">{t('lore.health.loading')}</p> : null}
        {error ? <p className="rounded-md border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300">{t('lore.health.failed')}</p> : null}
        {report && !error ? (
          <div className="grid gap-3">
            <div className="flex flex-wrap items-center gap-2 rounded-md border bg-muted/30 p-3 text-xs text-muted-foreground">
              <span>{t('lore.health.scanned', { scanned: report.chapters_scanned, total: report.chapters_total })}</span>
              {report.results_truncated ? <span className="text-amber-300">{t('lore.health.truncated')}</span> : <span className="ml-auto inline-flex items-center gap-1 text-emerald-300"><CheckCircle2 className="size-3.5" />{t('lore.health.complete')}</span>}
            </div>
            {groups.map(group => <HealthGroup key={group.key} title={group.title} icon={group.icon} items={group.items} collapsed={collapsed[group.key] === true} onToggle={() => setCollapsed(current => ({ ...current, [group.key]: !current[group.key] }))} onOpenFile={onOpenFile} />)}
          </div>
        ) : null}
        <div className="flex justify-end"><Button type="button" variant="outline" onClick={() => onOpenChange(false)}>{t('common.close')}</Button></div>
      </DialogContent>
    </Dialog>
  )
}

function HealthGroup({ title, icon: Icon, items, collapsed, onToggle, onOpenFile }: { title: string; icon: typeof AlertTriangle; items: LoreHealthIssue[]; collapsed: boolean; onToggle: () => void; onOpenFile?: (path: string) => void }) {
  const { t } = useTranslation()
  return (
    <section className="rounded-md border p-3">
      <button type="button" className="mb-2 flex w-full items-center gap-2 text-left text-sm font-medium" onClick={onToggle}><span>{collapsed ? <ChevronRight className="size-3.5" /> : <ChevronDown className="size-3.5" />}</span><Icon className="size-4" />{title}<span className="rounded-full bg-muted px-1.5 text-xs">{items.length}</span></button>
      {!collapsed && (items.length ? <div className="grid gap-1.5">{items.map((item, index) => { const detail = item.kind === 'unused_item' ? t('lore.health.unusedDetail') : item.detail; return <div key={`${item.kind}:${item.path}:${item.line}:${index}`} className="rounded bg-muted/30 px-2 py-1.5 text-xs"><div className="font-medium">{item.name || item.reference || item.path}</div><div className="text-muted-foreground">{item.path ? `${item.path}${item.line ? `:${item.line}` : ''}` : detail}</div>{item.path && onOpenFile ? <Button type="button" variant="link" size="xs" className="h-auto px-0 text-xs" onClick={() => onOpenFile(item.path!)}>{t('lore.mentions.openFile')}</Button> : null}</div> })}</div> : <p className="text-xs text-muted-foreground">{t('lore.health.none')}</p>)}
    </section>
  )
}


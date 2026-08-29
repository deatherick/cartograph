// Per-project header — breadcrumb pattern adapted from Grafel's webui-v2
// TopBar (MIT License, see NOTICE.md), trimmed to what Cartograph actually
// has: no group/ref switcher (no multi-project registry yet, ADR-0012's
// documented gap), no health dot, no scope selector (Cartograph has one
// project per running ctxd today).
import { ChevronRight } from 'lucide-react'
import { Badge } from '@/components/ui'
import type { Stats } from '@/lib/api'

export function TopBar({ surfaceLabel, stats }: { surfaceLabel: string; stats: Stats | null }) {
  return (
    <header className="flex items-center justify-between h-14 shrink-0 px-4 border-b border-border bg-bg">
      <nav aria-label="Breadcrumb" className="flex items-center gap-1.5 text-md">
        <span className="text-text-3">Cartograph</span>
        <ChevronRight size={12} className="text-text-4" />
        <span className="font-mono text-text-2">{stats?.repo ?? '…'}</span>
        <ChevronRight size={12} className="text-text-4" />
        <span className="font-medium text-text">{surfaceLabel}</span>
      </nav>

      {stats && (
        <div className="flex items-center gap-2">
          <Badge tone="neutral">{stats.entities} entities</Badge>
          <Badge tone="neutral">{stats.edges} edges</Badge>
        </div>
      )}
    </header>
  )
}

// Per-project header — breadcrumb pattern originally adapted from
// Grafel's webui-v2 TopBar (MIT License, see NOTICE.md), restyled as
// part of this project's Observatory-inspired redesign (docs/adr): the
// current screen name now renders in the display face (matching every
// page's own <h1>) and the stat/status pills read their values in mono
// (Badge's own `mono` prop) so entity/edge counts read as DATA, not
// prose — the same "data gets the mono face" discipline the reference
// field guide's own type-pairing section describes.
import { ChevronRight } from 'lucide-react'
import { Badge } from '@/components/ui'
import type { Stats, Operations, Project } from '@/lib/api'

function timeAgo(iso: string): string {
  if (!iso) return 'never'
  const ms = Date.now() - new Date(iso).getTime()
  if (ms < 0 || Number.isNaN(ms)) return 'just now'
  const s = Math.round(ms / 1000)
  if (s < 5) return 'just now'
  if (s < 60) return `${s}s ago`
  const m = Math.round(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.round(m / 60)
  return `${h}h ago`
}

export function TopBar({
  surfaceLabel,
  stats,
  operations,
  projects,
  project,
  projectsLoading,
  onProjectChange,
}: {
  surfaceLabel: string
  stats: Stats | null
  operations: Operations | null
  projects: Project[]
  project: string
  projectsLoading: boolean
  onProjectChange: (name: string) => void
}) {
  return (
    <header className="flex items-center justify-between h-14 shrink-0 px-5 border-b border-border bg-bg gap-3">
      <nav aria-label="Breadcrumb" className="flex items-center gap-2 text-md min-w-0">
        <span className="text-text-4 shrink-0">Cartograph</span>
        <ChevronRight size={12} className="text-text-4 shrink-0" />
        {projects.length > 1 ? (
          <select
            aria-label="Project"
            value={project}
            onChange={(e) => onProjectChange(e.target.value)}
            className="font-mono text-text-2 bg-transparent border border-border rounded-md px-1.5 py-0.5 text-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
          >
            {projects.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
              </option>
            ))}
          </select>
        ) : (
          <span className="font-mono text-text-2">
            {projectsLoading ? '…' : (stats?.repo ?? project ?? '…')}
          </span>
        )}
        <ChevronRight size={12} className="text-text-4 shrink-0" />
        <span className="font-display font-semibold text-text truncate">{surfaceLabel}</span>
      </nav>

      <div className="flex items-center gap-2 shrink-0">
        {stats && (
          <>
            <Badge tone="neutral" mono>
              {stats.entities} entities
            </Badge>
            <Badge tone="neutral" mono>
              {stats.edges} edges
            </Badge>
          </>
        )}
        {operations && (
          <>
            <Badge tone={operations.Watching ? 'success' : 'neutral'}>
              {operations.Watching ? 'watching' : 'not watching'}
            </Badge>
            <Badge tone={operations.LastError ? 'danger' : 'neutral'} title={operations.LastError || undefined} mono>
              reindexed {timeAgo(operations.LastReindexAt)}
            </Badge>
          </>
        )}
      </div>
    </header>
  )
}

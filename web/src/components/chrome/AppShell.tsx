// Shared chrome — NavRail (left) + TopBar (top) + routed content —
// adapted from Grafel's webui-v2 AppShell (MIT License, see NOTICE.md).
import { Outlet, useLocation } from 'react-router-dom'
import { useState } from 'react'
import { NavRail } from './NavRail'
import { TopBar } from './TopBar'
import { api, type Stats, type Operations } from '@/lib/api'
import { useProject } from '@/lib/project-context'
import { usePoll } from '@/hooks/usePoll'

const SURFACE_LABELS: Record<string, string> = {
  '/': 'Overview',
  '/graph': 'Graph',
  '/impact': 'Git diff impact',
}

// Polling interval for the "live" feel (ADR-0019): short enough that
// editing a watched project's source and waiting a few seconds visibly
// updates the browser with no manual reload, long enough not to hammer
// the daemon on every render.
const POLL_MS = 3000

export function AppShell() {
  const location = useLocation()
  const { project, projects, setProject, loading: projectsLoading } = useProject()
  const [stats, setStats] = useState<Stats | null>(null)
  const [operations, setOperations] = useState<Operations | null>(null)

  usePoll(
    () => {
      if (!project) return
      api.stats(project).then(setStats).catch(() => setStats(null))
      api.operations(project).then(setOperations).catch(() => setOperations(null))
    },
    [project],
    POLL_MS,
  )

  const surfaceLabel = SURFACE_LABELS[location.pathname] ?? 'Cartograph'

  return (
    <div className="flex h-full w-full">
      <NavRail />
      <div className="flex flex-col flex-1 min-w-0">
        <TopBar
          surfaceLabel={surfaceLabel}
          stats={stats}
          operations={operations}
          projects={projects}
          project={project}
          projectsLoading={projectsLoading}
          onProjectChange={setProject}
        />
        <main className="flex-1 min-h-0 overflow-auto bg-bg">
          <Outlet context={{ stats, project }} />
        </main>
      </div>
    </div>
  )
}

// Shared chrome — NavRail (left) + TopBar (top) + routed content —
// adapted from Grafel's webui-v2 AppShell (MIT License, see NOTICE.md).
import { Outlet, useLocation } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { NavRail } from './NavRail'
import { TopBar } from './TopBar'
import { api, type Stats } from '@/lib/api'

const SURFACE_LABELS: Record<string, string> = {
  '/': 'Overview',
  '/graph': 'Graph',
  '/impact': 'Git diff impact',
}

export function AppShell() {
  const location = useLocation()
  const [stats, setStats] = useState<Stats | null>(null)

  useEffect(() => {
    api.stats().then(setStats).catch(() => setStats(null))
  }, [location.pathname])

  const surfaceLabel = SURFACE_LABELS[location.pathname] ?? 'Cartograph'

  return (
    <div className="flex h-full w-full">
      <NavRail />
      <div className="flex flex-col flex-1 min-w-0">
        <TopBar surfaceLabel={surfaceLabel} stats={stats} />
        <main className="flex-1 min-h-0 overflow-auto bg-bg">
          <Outlet context={{ stats }} />
        </main>
      </div>
    </div>
  )
}

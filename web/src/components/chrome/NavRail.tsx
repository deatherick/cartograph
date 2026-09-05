// Left nav rail — structure/interaction pattern (56px collapsed, hover to
// 220px, icon+label rows, theme toggle pinned at the foot) adapted from
// Grafel's webui-v2 NavRail (MIT License — see NOTICE.md); the screen
// list itself is entirely Cartograph's own (Overview/Graph/Impact/
// Duplicates — Grafel's Topology/Paths/Links/GraphQL/Infrastructure/
// Security/Taint/Dependency-Injection/Error-flow/Quality/Operations
// screens have no Cartograph equivalent yet and were not carried over).
import { NavLink } from 'react-router-dom'
import { LayoutDashboard, Waypoints, Target, Copy, Sun, Moon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTheme } from '@/lib/theme'

const SCREENS = [
  { to: '/', label: 'Overview', Icon: LayoutDashboard, end: true },
  { to: '/graph', label: 'Graph', Icon: Waypoints },
  { to: '/impact', label: 'Git diff impact', Icon: Target },
  { to: '/duplicates', label: 'Duplicates', Icon: Copy },
]

function rowClass(active: boolean) {
  return cn(
    'group/nav relative flex items-center h-9 rounded-md px-2.5 mx-2 gap-3',
    'text-text-2 transition-colors duration-[120ms]',
    active ? 'bg-surface text-text shadow-[var(--shadow-1)]' : 'hover:bg-surface-2',
  )
}

export function NavRail() {
  const { theme, toggle } = useTheme()

  return (
    <aside
      className={cn(
        'group/rail flex flex-col shrink-0 h-full bg-bg-soft border-r border-border',
        'w-14 hover:w-[200px] transition-[width] duration-[180ms] ease-[var(--ease-out)] overflow-hidden',
      )}
    >
      <div className="flex items-center h-14 px-4 gap-2.5 shrink-0">
        <BrandMark />
        <span className="font-semibold text-text whitespace-nowrap opacity-0 group-hover/rail:opacity-100 transition-opacity">
          Cartograph
        </span>
      </div>

      <nav aria-label="Screens" className="flex flex-col gap-0.5 py-1 flex-1 min-h-0">
        {SCREENS.map(({ to, label, Icon, end }) => (
          <NavLink key={to} to={to} end={end} className={({ isActive }) => rowClass(isActive)} title={label}>
            <Icon size={18} className="shrink-0" />
            <span className="flex-1 whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
              {label}
            </span>
          </NavLink>
        ))}
      </nav>

      <div className="shrink-0 flex flex-col gap-0.5 py-2">
        <button className={rowClass(false)} onClick={toggle} title={theme === 'dark' ? 'Light mode' : 'Dark mode'}>
          {theme === 'dark' ? <Sun size={18} className="shrink-0" /> : <Moon size={18} className="shrink-0" />}
          <span className="flex-1 text-left whitespace-nowrap text-md opacity-0 group-hover/rail:opacity-100 transition-opacity">
            {theme === 'dark' ? 'Light' : 'Dark'} mode
          </span>
        </button>
      </div>
    </aside>
  )
}

function BrandMark() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" className="shrink-0" aria-hidden>
      <defs>
        <linearGradient id="cg-lg" x1="0" x2="1" y1="0" y2="1">
          <stop offset="0" stopColor="var(--accent)" />
          <stop offset="1" stopColor="var(--accent-strong)" />
        </linearGradient>
      </defs>
      <rect x="3" y="3" width="18" height="18" rx="5" fill="url(#cg-lg)" opacity=".18" />
      <circle cx="7" cy="8" r="2" fill="var(--accent)" />
      <circle cx="17" cy="8" r="1.6" fill="var(--accent)" opacity=".7" />
      <circle cx="12" cy="17" r="2" fill="var(--accent-strong)" />
      <path d="M8.4 9.6L11 15.5M15.7 9.4l-2.9 6" stroke="var(--accent)" strokeWidth="1.3" fill="none" />
    </svg>
  )
}

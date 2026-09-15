// Left nav rail. Interaction pattern reconsidered as part of this
// project's Observatory-inspired redesign (docs/adr): a hover-to-expand
// icon rail is a nice trick but hides every label until a pointer
// happens to sit over it — Observatory's own reference nav (a persistent
// left `<nav class="toc">`, always-labeled) reads as more purposeful for
// a small, fixed screen count like Cartograph's own four. Labels are
// therefore always visible now; only the brand mark + a hairline border
// remain from the previous hover-expand version.
import { NavLink } from 'react-router-dom'
import { LayoutDashboard, Waypoints, Target, Copy, Route, Sun, Moon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTheme } from '@/lib/theme'

const SCREENS = [
  { to: '/', label: 'Overview', Icon: LayoutDashboard, end: true },
  { to: '/graph', label: 'Graph', Icon: Waypoints },
  { to: '/impact', label: 'Git diff impact', Icon: Target },
  { to: '/duplicates', label: 'Duplicates', Icon: Copy },
  // Exploratory (docs/adr/0030) — a real resolved path drawn as a
  // sequence diagram, not merged/promoted to "done" status yet.
  { to: '/sequence', label: 'Sequence', Icon: Route },
]

function rowClass(active: boolean) {
  return cn(
    'group/nav relative flex items-center h-9 rounded-md px-3 mx-2.5 gap-2.5',
    'text-text-2 text-sm transition-colors duration-[120ms]',
    active ? 'bg-surface text-text shadow-[var(--shadow-1)]' : 'hover:bg-surface-2',
  )
}

export function NavRail() {
  const { theme, toggle } = useTheme()

  return (
    <aside className="flex flex-col shrink-0 h-full w-[196px] bg-bg-soft border-r border-border">
      <div className="flex items-center h-14 px-4 gap-2.5 shrink-0 border-b border-border-soft">
        <BrandMark />
        <span className="font-display font-semibold text-text whitespace-nowrap tracking-tight">Cartograph</span>
      </div>

      <nav aria-label="Screens" className="flex flex-col gap-0.5 py-3 flex-1 min-h-0">
        <p className="eyebrow px-5 mb-1.5">Screens</p>
        {SCREENS.map(({ to, label, Icon, end }) => (
          <NavLink key={to} to={to} end={end} className={({ isActive }) => rowClass(isActive)} title={label}>
            {({ isActive }) => (
              <>
                <Icon size={16} strokeWidth={isActive ? 2.25 : 1.75} className="shrink-0" />
                <span className="flex-1 whitespace-nowrap">{label}</span>
              </>
            )}
          </NavLink>
        ))}
      </nav>

      <div className="shrink-0 flex flex-col gap-0.5 py-2.5 border-t border-border-soft">
        <button className={rowClass(false)} onClick={toggle} title={theme === 'dark' ? 'Light mode' : 'Dark mode'}>
          {theme === 'dark' ? <Sun size={16} className="shrink-0" /> : <Moon size={16} className="shrink-0" />}
          <span className="flex-1 text-left whitespace-nowrap">{theme === 'dark' ? 'Light' : 'Dark'} mode</span>
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
          <stop offset="1" stopColor="var(--accent2)" />
        </linearGradient>
      </defs>
      <rect x="3" y="3" width="18" height="18" rx="5" fill="url(#cg-lg)" opacity=".18" />
      <circle cx="7" cy="8" r="2" fill="var(--accent)" />
      <circle cx="17" cy="8" r="1.6" fill="var(--accent2)" opacity=".85" />
      <circle cx="12" cy="17" r="2" fill="var(--accent-strong)" />
      <path d="M8.4 9.6L11 15.5M15.7 9.4l-2.9 6" stroke="var(--accent)" strokeWidth="1.3" fill="none" />
    </svg>
  )
}

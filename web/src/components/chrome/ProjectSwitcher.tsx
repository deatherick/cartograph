// Searchable project switcher — TopBar's own context for the same
// predictive-search pattern ADR-0031 introduced for the Graph view
// ("el dropdown de proyectos en caso hubieran muchos"). Kept client-side
// only, unlike ADR-0031's server-backed api.suggest: ProjectProvider
// already loads the full project list once up front (project-context.tsx),
// so there's nothing to fetch — filtering an array already in memory is
// strictly faster and simpler than a debounced round-trip for what is, in
// practice, a short, bounded list (registered daemon projects, not
// entities in an indexed codebase).
import { useEffect, useMemo, useRef, useState } from 'react'
import { ChevronsUpDown } from 'lucide-react'
import type { Project } from '@/lib/api'
import { cn } from '@/lib/utils'

export function ProjectSwitcher({
  projects,
  project,
  onChange,
}: {
  projects: Project[]
  project: string
  onChange: (name: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [highlight, setHighlight] = useState(0)
  const rootRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const matches = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return projects
    return projects.filter((p) => p.name.toLowerCase().includes(q) || p.repo.toLowerCase().includes(q))
  }, [projects, query])

  // Close on an outside click — the input's own onBlur can't be used here
  // (unlike EntityGraphPanel's dropdown) since clicking the ChevronsUpDown
  // trigger to REOPEN would otherwise immediately blur-close it first.
  useEffect(() => {
    if (!open) return
    function onDocClick(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDocClick)
    return () => document.removeEventListener('mousedown', onDocClick)
  }, [open])

  function openDropdown() {
    setOpen(true)
    setQuery('')
    setHighlight(0)
    // Focus after the input actually mounts (it's conditionally rendered).
    requestAnimationFrame(() => inputRef.current?.focus())
  }

  function pick(name: string) {
    onChange(name)
    setOpen(false)
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setHighlight((i) => Math.min(i + 1, matches.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setHighlight((i) => Math.max(i - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (matches[highlight]) pick(matches[highlight].name)
    } else if (e.key === 'Escape') {
      setOpen(false)
    }
  }

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        aria-label="Project"
        onClick={() => (open ? setOpen(false) : openDropdown())}
        className="flex items-center gap-1 font-mono text-text-2 bg-transparent border border-border rounded-md px-1.5 py-0.5 text-md hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
      >
        {project || '…'}
        <ChevronsUpDown size={11} className="text-text-4 shrink-0" />
      </button>

      {open && (
        <div className="absolute top-full left-0 mt-1 z-30 w-64 rounded-md border border-border bg-surface shadow-[var(--shadow-2)] overflow-hidden">
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setHighlight(0)
            }}
            onKeyDown={onKeyDown}
            placeholder="Filter projects…"
            className="w-full h-8 px-2.5 text-sm bg-transparent border-b border-border-soft focus-visible:outline-none"
          />
          <ul className="max-h-64 overflow-y-auto">
            {matches.length === 0 && <li className="px-3 py-2.5 text-sm text-text-3">No project matches "{query}".</li>}
            {matches.map((p, i) => (
              <li key={p.name}>
                <button
                  type="button"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => pick(p.name)}
                  className={cn(
                    'flex flex-col items-start w-full px-3 py-1.5 text-left',
                    i === highlight ? 'bg-accent-soft' : 'hover:bg-surface-2',
                    p.name === project && 'font-medium',
                  )}
                >
                  <span className="text-sm text-text mono">{p.name}</span>
                  {p.repo !== p.name && <span className="text-xs text-text-4 truncate">{p.repo}</span>}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

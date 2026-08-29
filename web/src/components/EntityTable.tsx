// The entity browser: search + kind filter + pagination — what turns
// "Overview" from a static stats page into something you can actually use
// to reach any entity's detail. Built fresh for Cartograph (no Grafel
// equivalent to adapt — its screens work from a very different, much
// larger data model).
import { useMemo, useState } from 'react'
import type { Entity } from '@/lib/api'
import { kindSlot } from '@/lib/graph-colors'
import { Input } from '@/components/ui'
import { cn } from '@/lib/utils'

const PAGE_SIZE = 25

export function EntityTable({
  entities,
  selectedId,
  onSelect,
  kind: controlledKind,
  onKindChange,
}: {
  entities: Entity[]
  selectedId?: string
  onSelect: (e: Entity) => void
  /** Controlled kind filter — pass together with onKindChange so a
   * parent (Overview's clickable Kind cards) can drive the filter from
   * outside the table, not just the table's own dropdown. */
  kind?: string
  onKindChange?: (kind: string) => void
}) {
  const [query, setQuery] = useState('')
  const [internalKind, setInternalKind] = useState<string>('All')
  const kind = controlledKind ?? internalKind
  const setKind = onKindChange ?? setInternalKind
  const [page, setPage] = useState(0)

  const kinds = useMemo(() => {
    const set = new Set<string>()
    for (const e of entities) set.add(e.Kind)
    return ['All', ...Array.from(set).sort()]
  }, [entities])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return entities.filter((e) => {
      if (kind !== 'All' && e.Kind !== kind) return false
      if (q && !e.Name.toLowerCase().includes(q) && !e.Qualified.toLowerCase().includes(q)) return false
      return true
    })
  }, [entities, query, kind])

  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const clampedPage = Math.min(page, pageCount - 1)
  const pageItems = filtered.slice(clampedPage * PAGE_SIZE, (clampedPage + 1) * PAGE_SIZE)

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-2 p-3 border-b border-border-soft shrink-0">
        <Input
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setPage(0)
          }}
          placeholder="Search by name or qualified name…"
          className="max-w-xs"
        />
        {/* Kind filter — an explicit dropdown, not a flat text search, so
            it's always clear whether you're looking at every entity or
            just (say) Functions vs Methods vs Classes. */}
        <select
          value={kind}
          onChange={(e) => {
            setKind(e.target.value)
            setPage(0)
          }}
          className="h-8 px-2 rounded-md border border-border bg-surface text-text text-sm"
        >
          {kinds.map((k) => (
            <option key={k} value={k}>
              {k === 'All' ? 'All kinds' : k}
            </option>
          ))}
        </select>
        <span className="text-text-3 text-sm ml-auto">{filtered.length} matching</span>
      </div>

      <div className="flex-1 overflow-y-auto">
        <table className="w-full text-sm border-collapse">
          <thead className="sticky top-0 bg-bg-soft text-text-3 text-xs uppercase tracking-wide">
            <tr>
              <th className="text-left font-medium px-3 py-2">Kind</th>
              <th className="text-left font-medium px-3 py-2">Name</th>
              <th className="text-left font-medium px-3 py-2">Location</th>
            </tr>
          </thead>
          <tbody>
            {pageItems.map((e) => (
              <tr
                key={e.ID}
                onClick={() => onSelect(e)}
                className={cn(
                  'cursor-pointer border-b border-border-soft hover:bg-surface-2',
                  selectedId === e.ID && 'bg-accent-soft',
                )}
              >
                <td className="px-3 py-2">
                  <span className="inline-flex items-center gap-1.5">
                    <span className="size-2 rounded-full shrink-0" style={{ background: `var(--pastel-${kindSlot(e.Kind)})` }} />
                    {e.Kind}
                  </span>
                </td>
                <td className="px-3 py-2 font-medium text-text">{e.Name}</td>
                <td className="px-3 py-2 mono text-text-3 text-xs">
                  {e.Anchor.File}:{e.Anchor.StartLine}
                </td>
              </tr>
            ))}
            {pageItems.length === 0 && (
              <tr>
                <td colSpan={3} className="px-3 py-6 text-center text-text-4">
                  No entities match.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {pageCount > 1 && (
        <div className="flex items-center justify-center gap-3 p-2 border-t border-border-soft text-sm shrink-0">
          <button
            className="disabled:opacity-40 hover:text-accent"
            disabled={clampedPage === 0}
            onClick={() => setPage(clampedPage - 1)}
          >
            ← Prev
          </button>
          <span className="text-text-3">
            Page {clampedPage + 1} / {pageCount}
          </span>
          <button
            className="disabled:opacity-40 hover:text-accent"
            disabled={clampedPage >= pageCount - 1}
            onClick={() => setPage(clampedPage + 1)}
          >
            Next →
          </button>
        </div>
      )}
    </div>
  )
}

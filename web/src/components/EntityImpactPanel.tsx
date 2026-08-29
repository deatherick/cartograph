// Renders one entity's blast radius (internal/service.Impact via
// /api/impact) — direct callers, the full transitive closure, and
// covering tests. Takes an ALREADY-RESOLVED name+file (no search box, no
// ambiguity handling): the standalone Impact page's "by entity" tab
// resolves a free-text name first (its own ambiguity picker, same as
// EntityGraphPanel's), then renders this; Overview's Impact tab already
// has an exact selected row to pass in. One rendering, two entry points.
import { useEffect, useState } from 'react'
import { api, type Entity, type ImpactResult } from '@/lib/api'
import { Badge } from '@/components/ui'
import { useProject } from '@/lib/project-context'

export function EntityImpactPanel({ name, file }: { name: string; file: string }) {
  const { project } = useProject()
  const [result, setResult] = useState<ImpactResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    setError(null)
    api
      .impact(project, name, file)
      .then(setResult)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false))
  }, [name, file, project])

  if (loading) return <p className="p-4 text-text-3 text-sm">Analyzing…</p>
  if (error) return <p className="p-4 text-danger text-sm">{error}</p>
  if (!result) return null

  return (
    <div className="p-4 overflow-y-auto h-full">
      <div className="flex items-center gap-2 mb-3">
        <Badge tone="accent">{result.Target.Kind}</Badge>
        <span className="mono text-text text-sm truncate">{result.Target.Qualified}</span>
      </div>

      <EntitySection title={`Direct callers (${result.DirectCallers?.length ?? 0})`} entities={result.DirectCallers} />

      <div className="mt-4">
        <h3 className="text-xs font-semibold text-text-3 uppercase tracking-wide mb-1.5">
          Full transitive impact ({result.Transitive?.length ?? 0})
        </h3>
        {!result.Transitive || result.Transitive.length === 0 ? (
          <p className="text-text-4 text-sm">Nothing depends on this — safe to change in isolation.</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {result.Transitive.map((r, i) => (
              <li key={i} className="text-sm flex items-center gap-2">
                <Badge tone="neutral">depth {r.Depth}</Badge>
                <span className="mono text-text-2 truncate">{r.Entity.Qualified}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <EntitySection title={`Tests covering it (${result.CoveringTests?.length ?? 0})`} entities={result.CoveringTests} className="mt-4" />
    </div>
  )
}

export function EntitySection({ title, entities, className }: { title: string; entities: Entity[] | null; className?: string }) {
  return (
    <div className={className}>
      <h3 className="text-xs font-semibold text-text-3 uppercase tracking-wide mb-1.5">{title}</h3>
      {!entities || entities.length === 0 ? (
        <p className="text-text-4 text-sm">(none)</p>
      ) : (
        <ul className="flex flex-col gap-1">
          {entities.map((e) => (
            <li key={e.ID} className="text-sm mono text-text-2 truncate">
              {e.Qualified}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

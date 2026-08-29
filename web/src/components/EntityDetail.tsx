// Entity detail/inspector panel — fan-in/fan-out, source view, and quick
// links to the Graph/Impact views for the selected entity. Shared by
// Overview (the entity table + this panel side by side) so there is
// always a path from "browse everything" to "see one entity's detail",
// which is exactly what a static Overview was missing.
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type Entity, type Inspection } from '@/lib/api'
import { Badge, Button } from '@/components/ui'

export function EntityDetail({
  entity,
  allEntities,
  onSelect,
}: {
  entity: Entity
  allEntities: Entity[]
  onSelect?: (e: Entity) => void
}) {
  const navigate = useNavigate()
  const [insp, setInsp] = useState<Inspection | null>(null)
  const [source, setSource] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setInsp(null)
    setSource(null)
    setError(null)
    api
      .inspect(entity.Name, entity.Anchor.File)
      .then(setInsp)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [entity.ID, entity.Name, entity.Anchor.File])

  const byId = new Map(allEntities.map((e) => [e.ID, e]))

  if (error) return <p className="p-4 text-danger text-sm">{error}</p>
  if (!insp) return <p className="p-4 text-text-3 text-sm">Loading…</p>

  return (
    <div className="p-4 overflow-y-auto h-full">
      <div className="flex items-center gap-2 mb-1">
        <Badge tone="accent">{insp.Entity.Kind}</Badge>
        <h2 className="font-semibold text-text truncate">{insp.Entity.Name}</h2>
      </div>
      <p className="mono text-text-3 text-xs mb-3">
        {insp.Entity.Qualified} · {insp.Entity.Anchor.File}:{insp.Entity.Anchor.StartLine}-{insp.Entity.Anchor.EndLine}
      </p>

      <div className="flex gap-2 mb-4">
        <Button
          size="sm"
          variant="secondary"
          onClick={async () => setSource((await api.source(insp.Entity.Name, insp.Entity.Anchor.File)).source)}
        >
          Source
        </Button>
        <Button
          size="sm"
          variant="secondary"
          onClick={() =>
            navigate(
              `/graph?name=${encodeURIComponent(insp.Entity.Name)}&file=${encodeURIComponent(insp.Entity.Anchor.File)}`,
            )
          }
        >
          Graph
        </Button>
        <Button
          size="sm"
          variant="secondary"
          onClick={() =>
            navigate(
              `/impact?name=${encodeURIComponent(insp.Entity.Name)}&file=${encodeURIComponent(insp.Entity.Anchor.File)}`,
            )
          }
        >
          Impact
        </Button>
      </div>

      {source && <pre className="bg-surface-2 border border-border rounded-lg p-3 text-xs overflow-x-auto mb-4">{source}</pre>}

      <h3 className="text-xs font-semibold text-text-3 uppercase tracking-wide mb-1.5">
        Fan-in ({insp.FanIn?.length ?? 0})
      </h3>
      <EdgeList edges={insp.FanIn} endpoint="Src" byId={byId} onSelect={onSelect} />

      <h3 className="text-xs font-semibold text-text-3 uppercase tracking-wide mt-4 mb-1.5">
        Fan-out ({insp.FanOut?.length ?? 0})
      </h3>
      <EdgeList edges={insp.FanOut} endpoint="Dst" byId={byId} onSelect={onSelect} />
    </div>
  )
}

function EdgeList({
  edges,
  endpoint,
  byId,
  onSelect,
}: {
  edges: Inspection['FanIn']
  endpoint: 'Src' | 'Dst'
  byId: Map<string, Entity>
  onSelect?: (e: Entity) => void
}) {
  if (!edges || edges.length === 0) return <p className="text-text-4 text-sm">(none)</p>
  return (
    <ul className="flex flex-col gap-1">
      {edges.map((e, i) => {
        const target = byId.get(e[endpoint])
        return (
          <li key={i} className="text-sm">
            <span className="text-text-4 text-xs mr-1.5">{e.Kind}</span>
            {target && onSelect ? (
              <button className="text-text-2 hover:text-accent hover:underline" onClick={() => onSelect(target)}>
                {target.Name}
              </button>
            ) : (
              <span className="text-text-2">{target ? target.Name : e[endpoint]}</span>
            )}
          </li>
        )
      })}
    </ul>
  )
}

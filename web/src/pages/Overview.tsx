// Overview: one integrated workspace, not a static stats page and not a
// set of separate pages you bounce between. Kind cards are clickable
// (filter the table below to that kind); selecting a row shows
// Detail/Graph/Impact as TABS in the same view — the same graph and
// impact-analysis capability the standalone /graph and /impact routes
// have, embedded here so browsing, inspecting, and analyzing one entity
// never feels like separate, disconnected actions.
import { useEffect, useState } from 'react'
import { api, type Entity, type Stats } from '@/lib/api'
import { Card, Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui'
import { kindSlot } from '@/lib/graph-colors'
import { EntityTable } from '@/components/EntityTable'
import { EntityDetail } from '@/components/EntityDetail'
import { EntityGraphPanel } from '@/components/EntityGraphPanel'
import { EntityImpactPanel } from '@/components/EntityImpactPanel'
import { cn } from '@/lib/utils'
import { useProject } from '@/lib/project-context'
import { usePoll } from '@/hooks/usePoll'

export function Overview() {
  const { project } = useProject()
  const [stats, setStats] = useState<Stats | null>(null)
  const [entities, setEntities] = useState<Entity[]>([])
  const [selected, setSelected] = useState<Entity | null>(null)
  const [kind, setKind] = useState('All')
  const [tab, setTab] = useState<'detail' | 'graph' | 'impact'>('detail')
  const [error, setError] = useState<string | null>(null)

  // Polled, not one-shot: a watched project reindexes out from under this
  // page (ADR-0019/ADR-0018) and the table/kind counts should catch up
  // without a manual reload.
  usePoll(() => {
    if (!project) return
    api.stats(project).then(setStats).catch((e) => setError(e.message))
    api
      .graph(project)
      .then((g) => setEntities(g.Entities ?? []))
      .catch(() => {})
  }, [project])

  // Switching projects invalidates whatever was selected from the
  // previous one's entity table — never show a stale detail/graph/impact
  // panel for an entity that belongs to a different project's graph.
  useEffect(() => {
    setSelected(null)
    setKind('All')
  }, [project])

  if (error) {
    return (
      <div className="p-8 max-w-lg">
        <h1 className="text-xl font-semibold text-text mb-2">No index found</h1>
        <p className="text-text-3">{error}</p>
        <p className="text-text-4 text-sm mt-3">
          Run <code className="mono">ctx index &lt;path&gt;</code> or start <code className="mono">ctxd</code> against
          this repo first.
        </p>
      </div>
    )
  }

  if (!stats) return <div className="p-8 text-text-3">Loading…</div>

  const kinds = Object.entries(stats.byKind).sort((a, b) => b[1] - a[1])

  function selectEntity(e: Entity) {
    setSelected(e)
    setTab('detail')
  }

  return (
    <div className="flex flex-col h-full">
      <div className="p-4 border-b border-border-soft shrink-0">
        <div className="flex gap-3 flex-wrap">
          <Card className="px-4 py-2.5">
            <div className="text-xl font-bold text-text leading-none">{stats.entities}</div>
            <div className="text-text-3 text-xs mt-1">entities</div>
          </Card>
          <Card className="px-4 py-2.5">
            <div className="text-xl font-bold text-text leading-none">{stats.edges}</div>
            <div className="text-text-3 text-xs mt-1">resolved edges</div>
          </Card>
          {kinds.map(([k, count]) => (
            <button key={k} onClick={() => setKind(k === kind ? 'All' : k)} className="text-left">
              <Card
                className={cn(
                  'px-4 py-2.5 transition-colors hover:border-accent cursor-pointer',
                  kind === k && 'border-accent bg-accent-soft',
                )}
              >
                <div className="flex items-center gap-1.5">
                  <span className="size-2 rounded-full shrink-0" style={{ background: `var(--pastel-${kindSlot(k)})` }} />
                  <span className="text-xl font-bold text-text leading-none">{count}</span>
                </div>
                <div className="text-text-3 text-xs mt-1">{k}</div>
              </Card>
            </button>
          ))}
        </div>
        {kind !== 'All' && (
          <p className="text-text-4 text-xs mt-2">
            Filtering to <span className="text-text-2 font-medium">{kind}</span> — click the card again to clear.
          </p>
        )}
      </div>

      <div className="flex flex-1 min-h-0">
        <div className="flex-1 min-w-0 border-r border-border-soft">
          <EntityTable entities={entities} selectedId={selected?.ID} onSelect={selectEntity} kind={kind} onKindChange={setKind} />
        </div>
        <div className="w-[440px] shrink-0 flex flex-col">
          {selected ? (
            <Tabs value={tab} onValueChange={(v) => setTab(v as typeof tab)} className="flex flex-col h-full">
              <TabsList className="shrink-0 mx-3 mt-2">
                <TabsTrigger value="detail">Detail</TabsTrigger>
                <TabsTrigger value="graph">Graph</TabsTrigger>
                <TabsTrigger value="impact">Impact</TabsTrigger>
              </TabsList>
              {/* Radix's TabsContent only renders its children when its
                  tab is active (nothing is mounted-but-hidden by default,
                  unlike some other tab libraries) — an inactive Graph tab
                  never mounts React Flow into a zero-size container, and
                  an unselected tab never fires its data fetch. */}
              <TabsContent value="detail" className="flex-1 min-h-0">
                <EntityDetail entity={selected} allEntities={entities} onSelect={selectEntity} />
              </TabsContent>
              <TabsContent value="graph" className="flex-1 min-h-0">
                {/* key forces a fresh panel per entity, so its own internal
                    center/history state doesn't leak from the previously
                    selected row. */}
                <EntityGraphPanel key={selected.ID} initialName={selected.Name} initialFile={selected.Anchor.File} />
              </TabsContent>
              <TabsContent value="impact" className="flex-1 min-h-0 overflow-y-auto">
                <EntityImpactPanel key={selected.ID} name={selected.Name} file={selected.Anchor.File} />
              </TabsContent>
            </Tabs>
          ) : (
            <p className="p-4 text-text-3 text-sm">Select a row to inspect it.</p>
          )}
        </div>
      </div>
    </div>
  )
}

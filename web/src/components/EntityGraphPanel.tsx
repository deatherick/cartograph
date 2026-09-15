// The navigable graph explorer's actual logic — click a node to make it
// the new center (re-fetch its neighborhood, refit the camera), search,
// ambiguous-name picker, breadcrumb history. Extracted into a component
// (not a page) so it can render EITHER as its own route (pages/GraphPage.tsx,
// for free-form exploration) OR embedded inline as a tab next to an
// entity's Detail/Impact view (pages/Overview.tsx) — the same graph
// capability either way, not two different implementations.
//
// Built on @xyflow/react (React Flow — the same MIT-licensed library
// Grafel's own webui-v2 uses for its DAG views, see NOTICE.md) with dagre
// for layout, NOT @cosmos.gl/graph (an earlier attempt, replaced after
// real usage found it fatal for this project's scale: cosmos.gl is a
// GPU point-renderer built for graphs with thousands of nodes and
// deliberately renders NO per-node text — every node showed as an
// unlabeled circle, "sin títulos ni nada distinguible". Cartograph's
// graphs are always a bounded 2-hop neighborhood (typically 10-40 nodes)
// — exactly React Flow's scale, where each node is a real DOM element
// that can show a name, a kind badge, and a color, and ships with pan/
// zoom/click handling and a stable, deterministic (non-physics) layout
// via dagre — no risk of the "simulation explodes, camera zooms into
// nothing" failure mode cosmos.gl's force simulation had.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  Handle,
  Position,
  useNodesState,
  useEdgesState,
  useReactFlow,
  ReactFlowProvider,
  type Node,
  type Edge,
  type NodeProps,
  type OnNodesChange,
  type OnEdgesChange,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import dagre from 'dagre'
import { ArrowLeft, Search as SearchIcon, Waypoints, ListTree, X, ChevronUp, ChevronDown } from 'lucide-react'
import { api, type Entity, type Inspection } from '@/lib/api'
import { useProject } from '@/lib/project-context'
import { useAppliedTheme } from '@/lib/theme'
import { kindSlot, KIND_LEGEND } from '@/lib/graph-colors'
import { Button, Input, Pill } from '@/components/ui'
import { cn } from '@/lib/utils'

interface HistoryEntry {
  name: string
  file: string
}

interface NodeData {
  label: string
  kind: string
  isCenter: boolean
  // dimmed/matched are set client-side by the in-canvas node finder
  // below (NodeFinder) — never refetched, purely a visual overlay on
  // whatever nodes are already loaded (found missing: the toolbar's own
  // search re-fetches a whole new neighborhood, which isn't what you
  // want when you already see the node you're after somewhere in a
  // 30-node graph, just not AT a glance — "hace falta un buscador dentro
  // de los nodos").
  dimmed?: boolean
  matched?: boolean
  [key: string]: unknown
}

const NODE_WIDTH = 160
const NODE_HEIGHT = 44

function EntityNode({ data }: NodeProps) {
  const d = data as unknown as NodeData
  return (
    <div
      className="rounded-lg border px-3 py-2 bg-surface transition-[opacity,box-shadow,border-color] duration-[120ms]"
      style={{
        borderColor: d.matched ? 'var(--accent)' : d.isCenter ? 'var(--accent)' : 'var(--border)',
        borderWidth: d.matched || d.isCenter ? 2 : 1,
        width: NODE_WIDTH,
        opacity: d.dimmed ? 0.35 : 1,
        boxShadow: d.matched ? '0 0 0 3px var(--accent-ring), var(--shadow-2)' : 'var(--shadow-1)',
      }}
    >
      <Handle type="target" position={Position.Top} style={{ opacity: 0 }} />
      <div className="flex items-center gap-1.5 mb-0.5">
        <span className="size-2 rounded-full shrink-0" style={{ background: `var(--pastel-${kindSlot(d.kind)})` }} />
        <span className="text-text-4 text-[10px] uppercase tracking-wide">{d.kind}</span>
      </div>
      <div className="text-sm font-medium text-text truncate" title={d.label}>
        {d.label}
      </div>
      <Handle type="source" position={Position.Bottom} style={{ opacity: 0 }} />
    </div>
  )
}

const nodeTypes = { entity: EntityNode }

function layout(nodes: Node[], edges: Edge[]): Node[] {
  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: 'TB', nodesep: 40, ranksep: 70 })
  g.setDefaultEdgeLabel(() => ({}))
  for (const n of nodes) g.setNode(n.id, { width: NODE_WIDTH, height: NODE_HEIGHT })
  for (const e of edges) g.setEdge(e.source, e.target)
  dagre.layout(g)
  return nodes.map((n) => {
    const pos = g.node(n.id)
    return { ...n, position: { x: pos.x - NODE_WIDTH / 2, y: pos.y - NODE_HEIGHT / 2 } }
  })
}

export function EntityGraphPanel({ initialName = '', initialFile = '' }: { initialName?: string; initialFile?: string }) {
  const { project } = useProject()
  const [searchInput, setSearchInput] = useState(initialName)
  const [center, setCenter] = useState<HistoryEntry | null>(initialName ? { name: initialName, file: initialFile } : null)
  const [history, setHistory] = useState<HistoryEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [candidates, setCandidates] = useState<Entity[] | null>(null)
  const [view, setView] = useState<'graph' | 'tree'>('graph')
  // Fan-in/fan-out for the CURRENT center, kept alongside the graph's
  // 2-hop data so the Tree view has something to render — the same
  // underlying relationships shown a different way (a real request: "el
  // grafo es perfecto, pero me gustaria tambien un arbol o algo en texto"),
  // not a second, separately-fetched view of different data.
  const [inspection, setInspection] = useState<Inspection | null>(null)

  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([])
  // Keyed by entity ID (== React Flow node id) so onNodeClick can resolve
  // a click straight back to that entity's exact file — no ambiguity risk,
  // no extra round-trip to /api/find.
  const entitiesRef = useRef<Map<string, Entity>>(new Map())

  const navigateTo = useCallback((name: string, file: string) => {
    setCenter((prev) => {
      // Guard against re-centering on the entity that's ALREADY the
      // center — found from real usage: the center node is itself one of
      // the rendered nodes (so its own relationships are visible), and a
      // self-loop (an entity that calls/references itself) or simply
      // clicking the already-centered node otherwise pushed an identical
      // {name,file} onto history every time, growing it without bound
      // ("fileEntry / fileEntry / fileEntry / ..."). Same name+file is
      // treated as "already here" — a true no-op, not a new history entry.
      if (prev && prev.name === name && prev.file === file) return prev
      if (prev) setHistory((h) => [...h, prev])
      return { name, file }
    })
  }, [])

  const goBack = useCallback(() => {
    setHistory((h) => {
      if (h.length === 0) return h
      const next = h.slice(0, -1)
      setCenter(h[h.length - 1])
      return next
    })
  }, [])

  // A genuine project switch (not the initial '' -> real-name resolution
  // on mount) invalidates whatever center/history/candidates came from
  // the previous project's graph.
  const prevProjectRef = useRef(project)
  useEffect(() => {
    if (prevProjectRef.current && prevProjectRef.current !== project) {
      setCenter(null)
      setHistory([])
      setCandidates(null)
      setError(null)
    }
    prevProjectRef.current = project
  }, [project])

  useEffect(() => {
    if (!center || !project) return
    let cancelled = false
    setLoading(true)
    setError(null)

    ;(async () => {
      try {
        const insp = await api.inspect(project, center.name, center.file)
        const related = await api.related(project, center.name, center.file, 2)
        if (cancelled) return

        const entities: Entity[] = [insp.Entity]
        const seen = new Set([insp.Entity.ID])
        const rawEdges: Array<{ src: string; dst: string; kind: string }> = []
        for (const r of related ?? []) {
          if (!seen.has(r.Entity.ID)) {
            seen.add(r.Entity.ID)
            entities.push(r.Entity)
          }
          if (r.Via?.Src && r.Via?.Dst) rawEdges.push({ src: r.Via.Src, dst: r.Via.Dst, kind: r.Via.Kind })
        }

        entitiesRef.current = new Map(entities.map((e) => [e.ID, e]))

        const flowNodes: Node[] = entities.map((e) => ({
          id: e.ID,
          type: 'entity',
          position: { x: 0, y: 0 },
          data: { label: e.Name, kind: e.Kind, isCenter: e.ID === insp.Entity.ID } satisfies NodeData,
        }))
        const flowEdges: Edge[] = rawEdges
          // Self-loops (an entity referencing/calling itself) add no
          // navigational value in a top-down dagre layout and can confuse
          // it — dropped here, not sent to the layout engine at all.
          .filter((e) => e.src !== e.dst && seen.has(e.src) && seen.has(e.dst))
          .map((e, i) => ({
            id: `e${i}`,
            source: e.src,
            target: e.dst,
            label: e.kind,
            style: { stroke: 'var(--cg-graph-edge)' },
            labelStyle: { fill: 'var(--text-3)', fontSize: 10 },
          }))

        setNodes(layout(flowNodes, flowEdges))
        setEdges(flowEdges)
        setInspection(insp)
      } catch (e) {
        if (cancelled) return
        const message = e instanceof Error ? e.message : String(e)
        // Entering with just a name (a direct URL, a stale link) can still
        // land on an ambiguous one — recover with the same picker
        // onSearchSubmit uses, instead of surfacing the raw service error.
        if (message.includes('is ambiguous across')) {
          try {
            const matches = await api.find(project, center.name)
            if (matches.length > 1) {
              setCandidates(matches)
              setCenter(null)
              return
            }
          } catch {
            // fall through to the raw error below
          }
        }
        setError(message)
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()

    return () => {
      cancelled = true
    }
  }, [center, project, setNodes, setEdges])

  // Live autocomplete for the toolbar search (ADR-0031): typing a partial,
  // approximate name — not knowing it exactly, the real complaint this
  // answers ("asume que uno se sabe los nombres exactos de las
  // funciones") — surfaces real matching entities as you type, via
  // api.suggest's substring search, instead of requiring Enter + an exact
  // or fully-disambiguated name before anything happens.
  const [suggestions, setSuggestions] = useState<Entity[]>([])
  const [suggestOpen, setSuggestOpen] = useState(false)
  const [suggestIndex, setSuggestIndex] = useState(-1)
  const [kindFilter, setKindFilter] = useState('')
  // Set right before a programmatic setSearchInput (picking a suggestion,
  // or submitting the plain exact-match form) so the effect below doesn't
  // treat that as a fresh keystroke and immediately reopen the dropdown
  // over the graph it just navigated to — a real bug found live: picking
  // "grepFixture" via Enter re-triggered this effect on the resulting
  // searchInput change, and the single now-exact match reopened the
  // dropdown right on top of the freshly-loaded canvas.
  const skipNextSuggestRef = useRef(false)

  useEffect(() => {
    if (skipNextSuggestRef.current) {
      skipNextSuggestRef.current = false
      return
    }
    const q = searchInput.trim()
    if (!q) {
      setSuggestions([])
      setSuggestOpen(false)
      return
    }
    let cancelled = false
    // Debounced, not fired on every keystroke — a real neighborhood of
    // thousands of entities makes an unthrottled call-per-keystroke
    // wasteful and prone to out-of-order responses; 150ms is short enough
    // to still feel live.
    const timer = setTimeout(() => {
      api
        .suggest(project, q, kindFilter)
        .then((matches) => {
          if (cancelled) return
          setSuggestions(matches)
          setSuggestOpen(true)
          setSuggestIndex(-1)
        })
        .catch(() => {
          // A failed suggest call shouldn't block the older exact-match
          // submit path below — just show no suggestions.
          if (cancelled) return
          setSuggestions([])
        })
    }, 150)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [searchInput, kindFilter, project])

  const pickSuggestion = useCallback(
    (e: Entity) => {
      setSuggestOpen(false)
      setSuggestions([])
      skipNextSuggestRef.current = true
      setSearchInput(e.Name)
      navigateTo(e.Name, e.Anchor.File)
    },
    [navigateTo],
  )

  const onSearchKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (!suggestOpen || suggestions.length === 0) return
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSuggestIndex((i) => Math.min(i + 1, suggestions.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSuggestIndex((i) => Math.max(i - 1, 0))
      } else if (e.key === 'Enter' && suggestIndex >= 0) {
        // A specific suggestion is highlighted — pick it directly and
        // skip the older submit handler's own exact/ambiguous-name path
        // below entirely (this is already a specific, resolved entity).
        e.preventDefault()
        pickSuggestion(suggestions[suggestIndex])
      } else if (e.key === 'Escape') {
        setSuggestOpen(false)
      }
    },
    [suggestOpen, suggestions, suggestIndex, pickSuggestion],
  )

  const onSearchSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault()
      const name = searchInput.trim()
      if (!name) return
      setError(null)
      setCandidates(null)
      setSuggestOpen(false)
      try {
        const matches = await api.find(project, name)
        if (matches.length === 0) {
          setError(`No entity named "${name}" found.`)
        } else if (matches.length === 1) {
          navigateTo(matches[0].Name, matches[0].Anchor.File)
        } else {
          setCandidates(matches)
        }
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e))
      }
    },
    [searchInput, project, navigateTo],
  )

  const pickCandidate = useCallback(
    (e: Entity) => {
      setCandidates(null)
      navigateTo(e.Name, e.Anchor.File)
    },
    [navigateTo],
  )

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      // node.id is the entity's ID, resolved straight back to its exact
      // file from the last successful fetch — no ambiguity risk, no
      // extra round-trip.
      const entity = entitiesRef.current.get(node.id)
      if (entity) navigateTo(entity.Name, entity.Anchor.File)
    },
    [navigateTo],
  )

  const proOptions = useMemo(() => ({ hideAttribution: true }), [])

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-2 px-3 py-2 border-b border-border-soft shrink-0">
        {history.length > 0 && (
          <Button variant="ghost" size="sm" onClick={goBack} title="Back">
            <ArrowLeft size={14} />
          </Button>
        )}
        <div className="relative flex-1 max-w-md">
          <form onSubmit={onSearchSubmit} className="flex items-center gap-2">
            <SearchIcon size={14} className="text-text-4 shrink-0" />
            <Input
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
              onKeyDown={onSearchKeyDown}
              onFocus={() => suggestions.length > 0 && setSuggestOpen(true)}
              // A short delay, not an immediate close: a plain onBlur
              // would fire before a click on a dropdown item registers,
              // making suggestions unclickable by mouse (only usable via
              // ArrowDown/Enter). onMouseDown on each item below also
              // guards this, but the delay covers Tab-away too.
              onBlur={() => setTimeout(() => setSuggestOpen(false), 120)}
              placeholder="Entity name to center the graph on… (partial names OK)"
              className="h-8"
            />
          </form>

          {suggestOpen && (
            <div className="absolute top-full left-0 right-0 mt-1 z-20 rounded-md border border-border bg-surface shadow-[var(--shadow-2)] overflow-hidden">
              <div className="flex flex-wrap gap-1 px-2 py-1.5 border-b border-border-soft">
                <Pill active={kindFilter === ''} onMouseDown={(e) => e.preventDefault()} onClick={() => setKindFilter('')} className="h-6 px-2 text-xs">
                  All kinds
                </Pill>
                {KIND_LEGEND.map((kind) => (
                  <Pill
                    key={kind}
                    active={kindFilter === kind}
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => setKindFilter((k) => (k === kind ? '' : kind))}
                    className="h-6 px-2 text-xs"
                  >
                    <span className="size-1.5 rounded-full shrink-0" style={{ background: `var(--pastel-${kindSlot(kind)})` }} />
                    {kind}
                  </Pill>
                ))}
              </div>

              {suggestions.length === 0 ? (
                <p className="px-3 py-2.5 text-sm text-text-3">
                  No entity matches "{searchInput.trim()}"{kindFilter ? ` among ${kindFilter}s` : ''}.
                </p>
              ) : (
                <ul>
                  {suggestions.map((s, i) => (
                    <li key={s.ID}>
                      <button
                        type="button"
                        onMouseDown={(e) => e.preventDefault()}
                        onClick={() => pickSuggestion(s)}
                        className={cn(
                          'flex items-center gap-2 w-full px-3 py-1.5 text-left text-sm',
                          i === suggestIndex ? 'bg-accent-soft' : 'hover:bg-surface-2',
                        )}
                      >
                        <span className="size-2 rounded-full shrink-0" style={{ background: `var(--pastel-${kindSlot(s.Kind)})` }} />
                        <span className="text-text-4 text-[10px] uppercase tracking-wide shrink-0">{s.Kind}</span>
                        <span className="text-text font-medium truncate">{s.Name}</span>
                        <span className="text-text-4 mono text-xs truncate ml-auto">{s.Anchor.File}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
        <div className="flex items-center border border-border rounded-md overflow-hidden shrink-0">
          <button
            onClick={() => setView('graph')}
            title="Graph view"
            className={cn('flex items-center gap-1 h-8 px-2 text-xs', view === 'graph' ? 'bg-accent-soft text-accent-strong' : 'text-text-3 hover:bg-surface-2')}
          >
            <Waypoints size={13} /> Graph
          </button>
          <button
            onClick={() => setView('tree')}
            title="Tree view"
            className={cn('flex items-center gap-1 h-8 px-2 text-xs border-l border-border', view === 'tree' ? 'bg-accent-soft text-accent-strong' : 'text-text-3 hover:bg-surface-2')}
          >
            <ListTree size={13} /> Tree
          </button>
        </div>
        {loading && <span className="text-text-3 text-sm">loading…</span>}
        {error && <span className="text-danger text-sm">{error}</span>}
      </div>

      {candidates && (
        <div className="px-3 py-2 border-b border-border-soft bg-surface-2 shrink-0">
          <p className="text-text-3 text-sm mb-1.5">
            "{searchInput.trim()}" matches {candidates.length} entities — pick one:
          </p>
          <div className="flex flex-wrap gap-1.5">
            {candidates.map((c) => (
              <button
                key={c.ID}
                onClick={() => pickCandidate(c)}
                className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-full text-sm border border-border bg-surface hover:bg-surface-2"
              >
                <span className="size-2 rounded-full shrink-0" style={{ background: `var(--pastel-${kindSlot(c.Kind)})` }} />
                <span className="text-text-2">{c.Kind}</span>
                <span className="text-text-4 mono text-xs">{c.Anchor.File}</span>
              </button>
            ))}
          </div>
        </div>
      )}

      {history.length > 0 && (
        <div className="flex items-center gap-1 px-3 py-1.5 text-sm text-text-3 border-b border-border-soft overflow-x-auto shrink-0">
          {history.map((h, i) => (
            <span key={i} className="flex items-center gap-1 shrink-0">
              <button className="hover:text-accent hover:underline" onClick={() => setHistory(history.slice(0, i))}>
                {h.name}
              </button>
              <span>/</span>
            </span>
          ))}
          <span className="font-medium text-text">{center?.name}</span>
        </div>
      )}

      <div className="relative flex-1 min-h-0">
        {!center ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <p className="text-text-3 text-md">Search an entity above to start exploring the graph.</p>
          </div>
        ) : view === 'tree' ? (
          <TreeView inspection={inspection} entitiesById={entitiesRef.current} onSelect={navigateTo} />
        ) : (
          <ReactFlowProvider>
            <GraphCanvas
              nodes={nodes}
              edges={edges}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onNodeClick={onNodeClick}
              proOptions={proOptions}
            />
          </ReactFlowProvider>
        )}

        {view === 'graph' && (
          <div className="absolute bottom-3 left-3 flex flex-wrap gap-2 max-w-sm pointer-events-none z-10">
            {KIND_LEGEND.map((kind) => (
              <span key={kind} className="inline-flex items-center gap-1.5 text-xs text-text-3 bg-bg/70 px-1.5 rounded">
                <span className="size-2.5 rounded-full shrink-0" style={{ background: `var(--pastel-${kindSlot(kind)})` }} />
                {kind}
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

// GraphCanvas is the actual React Flow canvas plus the in-canvas node
// finder — split out from EntityGraphPanel because useReactFlow() (which
// the finder needs, to pan/zoom to a match) only works inside a
// <ReactFlowProvider>, and <ReactFlow> itself only PROVIDES that context
// to its own children, not to whatever renders it — so the finder's own
// logic has to live in a component nested inside the Provider, not in
// EntityGraphPanel's own body.
function GraphCanvas({
  nodes,
  edges,
  onNodesChange,
  onEdgesChange,
  onNodeClick,
  proOptions,
}: {
  nodes: Node[]
  edges: Edge[]
  onNodesChange: OnNodesChange<Node>
  onEdgesChange: OnEdgesChange<Edge>
  onNodeClick: (e: React.MouseEvent, node: Node) => void
  proOptions: { hideAttribution: boolean }
}) {
  const { fitView } = useReactFlow()
  // React Flow's own `colorMode` defaults to following the OS's
  // `prefers-color-scheme`, entirely independent of the app's manual
  // light/dark toggle (NavRail) — the two disagree whenever the OS is
  // dark but the user has picked light in-app (or vice versa), and React
  // Flow then paints its canvas/controls/minimap in the wrong mode (a
  // black canvas while the rest of the UI is light). Pass the app's own
  // applied theme explicitly instead of "system" so the graph always
  // matches what's actually on screen.
  const appliedTheme = useAppliedTheme()
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)

  // Matches are derived purely from the nodes ALREADY loaded — no API
  // call, no re-fetch, unlike the toolbar's own "center the graph on"
  // search above. Case-insensitive substring on the node's own label
  // (its bare Name), the same match rule EntityTable's own search box
  // uses.
  const matchIds = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return []
    return nodes.filter((n) => (n.data as unknown as NodeData).label.toLowerCase().includes(q)).map((n) => n.id)
  }, [nodes, query])

  const matchSet = useMemo(() => new Set(matchIds), [matchIds])
  const clampedIndex = matchIds.length > 0 ? activeIndex % matchIds.length : 0

  // Overlay dimmed/matched onto the already-loaded nodes for rendering
  // only — the underlying `nodes` state (owned by EntityGraphPanel,
  // shared with onNodesChange's drag/select handling) is never mutated
  // by the finder.
  const renderNodes = useMemo(() => {
    if (!query.trim()) return nodes
    return nodes.map((n) => ({
      ...n,
      data: { ...n.data, dimmed: !matchSet.has(n.id), matched: n.id === matchIds[clampedIndex] } as NodeData,
    }))
  }, [nodes, query, matchSet, matchIds, clampedIndex])

  const goToMatch = useCallback(
    (index: number) => {
      const id = matchIds[((index % matchIds.length) + matchIds.length) % matchIds.length]
      if (!id) return
      fitView({ nodes: [{ id }], duration: 300, padding: 2, maxZoom: 1.2 })
    },
    [matchIds, fitView],
  )

  useEffect(() => {
    if (matchIds.length > 0) {
      setActiveIndex(0)
      goToMatch(0)
    }
    // Only re-run when the QUERY changes (a fresh search should jump to
    // its first match) — not on every `goToMatch` identity change, which
    // would fight the user's own up/down cycling below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query])

  function cycle(delta: number) {
    if (matchIds.length === 0) return
    const next = clampedIndex + delta
    setActiveIndex(next)
    goToMatch(next)
  }

  return (
    <>
      <ReactFlow
        nodes={renderNodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick}
        nodeTypes={nodeTypes}
        fitView
        proOptions={proOptions}
        colorMode={appliedTheme}
      >
        <Background />
        <Controls showInteractive={false} />
        <MiniMap pannable zoomable className="!bg-surface" />
      </ReactFlow>

      {/* The in-canvas node finder — deliberately separate from the
          toolbar's "center the graph on" search above the canvas: that
          one re-fetches a whole new neighborhood; this one only
          highlights/pans to a node ALREADY on screen. */}
      <div className="absolute top-3 right-3 z-10 flex items-center gap-1 bg-surface border border-border rounded-md shadow-[var(--shadow-2)] px-2 py-1.5">
        <SearchIcon size={13} className="text-text-4 shrink-0" />
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') cycle(e.shiftKey ? -1 : 1)
            if (e.key === 'Escape') setQuery('')
          }}
          placeholder="Find a node on screen…"
          className="h-6 w-40 bg-transparent text-sm text-text placeholder:text-text-4 focus:outline-none"
        />
        {query.trim() && (
          <>
            <span className="text-text-4 text-xs tabular-nums shrink-0 mono">
              {matchIds.length > 0 ? `${clampedIndex + 1}/${matchIds.length}` : '0/0'}
            </span>
            <button
              className="text-text-3 hover:text-text disabled:opacity-30 shrink-0"
              disabled={matchIds.length === 0}
              onClick={() => cycle(-1)}
              title="Previous match (Shift+Enter)"
            >
              <ChevronUp size={13} />
            </button>
            <button
              className="text-text-3 hover:text-text disabled:opacity-30 shrink-0"
              disabled={matchIds.length === 0}
              onClick={() => cycle(1)}
              title="Next match (Enter)"
            >
              <ChevronDown size={13} />
            </button>
            <button className="text-text-3 hover:text-text shrink-0" onClick={() => setQuery('')} title="Clear (Esc)">
              <X size={13} />
            </button>
          </>
        )}
      </div>
    </>
  )
}

// TreeView is the same fan-in/fan-out relationship data as the graph,
// shown as a scannable indented text list instead of a canvas — a
// different, complementary way to read the same thing, not a second
// dataset. Clicking any entry re-centers, exactly like clicking a graph
// node.
function TreeView({
  inspection,
  entitiesById,
  onSelect,
}: {
  inspection: Inspection | null
  entitiesById: Map<string, Entity>
  onSelect: (name: string, file: string) => void
}) {
  if (!inspection) return null
  const { Entity: center, FanIn, FanOut } = inspection

  return (
    <div className="absolute inset-0 overflow-y-auto p-4">
      <div className="flex items-center gap-2 mb-4">
        <span className="size-2.5 rounded-full shrink-0" style={{ background: `var(--pastel-${kindSlot(center.Kind)})` }} />
        <span className="font-semibold text-text">{center.Name}</span>
        <span className="text-text-4 text-xs">{center.Kind}</span>
      </div>

      <TreeBranch title={`Depends on (fan-out, ${FanOut?.length ?? 0})`} edges={FanOut} endpoint="Dst" entitiesById={entitiesById} onSelect={onSelect} />
      <TreeBranch
        title={`Depended on by (fan-in, ${FanIn?.length ?? 0})`}
        edges={FanIn}
        endpoint="Src"
        entitiesById={entitiesById}
        onSelect={onSelect}
        className="mt-5"
      />
    </div>
  )
}

function TreeBranch({
  title,
  edges,
  endpoint,
  entitiesById,
  onSelect,
  className,
}: {
  title: string
  edges: Inspection['FanIn']
  endpoint: 'Src' | 'Dst'
  entitiesById: Map<string, Entity>
  onSelect: (name: string, file: string) => void
  className?: string
}) {
  return (
    <div className={className}>
      <h3 className="eyebrow mb-1.5">{title}</h3>
      {!edges || edges.length === 0 ? (
        <p className="text-text-4 text-sm pl-4">(none)</p>
      ) : (
        <ul className="flex flex-col">
          {edges.map((e, i) => {
            const target = entitiesById.get(e[endpoint])
            return (
              <li key={i} className="flex items-center gap-1.5 py-1 pl-2 border-l-2 border-border-soft ml-1.5 text-sm">
                <span className="text-text-4 text-[10px] uppercase w-14 shrink-0">{e.Kind}</span>
                {target ? (
                  <button
                    className="flex items-center gap-1.5 text-text-2 hover:text-accent hover:underline truncate"
                    onClick={() => onSelect(target.Name, target.Anchor.File)}
                  >
                    <span className="size-1.5 rounded-full shrink-0" style={{ background: `var(--pastel-${kindSlot(target.Kind)})` }} />
                    {target.Name}
                    <span className="text-text-4 text-xs truncate">{target.Anchor.File}</span>
                  </button>
                ) : (
                  <span className="mono text-text-4 text-xs">{e[endpoint]}</span>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

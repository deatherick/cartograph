// Sequence view — exploratory (docs/adr/0030-sequence-diagram-exploration.md):
// renders `ctx path`/`context_path`'s existing real, deterministic
// shortest-path chain as a sequence diagram (participants as lifelines,
// each hop a labeled arrow, top to bottom in path order). Every arrow is
// a REAL resolved edge this project's own resolver bound — never an
// authored or LLM-curated relationship, unlike the kind of diagram tool
// that inspired trying this (see that ADR's own account of why the
// layout mechanism itself was deliberately NOT ported, only the idea of
// a sequence-shaped view).
import { useState } from 'react'
import { api, type Entity, type PathResult } from '@/lib/api'
import { Input, Button } from '@/components/ui'
import { useProject } from '@/lib/project-context'
import { kindSlot } from '@/lib/graph-colors'

const COL_WIDTH = 220
const HEADER_H = 56
const HOP_H = 64
const PAD = 24

export function SequencePage() {
  const { project } = useProject()
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [result, setResult] = useState<PathResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  async function findPath() {
    if (!from.trim() || !to.trim()) return
    setLoading(true)
    setError(null)
    try {
      setResult(await api.path(project, from.trim(), to.trim()))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setResult(null)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="p-6 flex flex-col h-full">
      <p className="eyebrow mb-2">Exploratory</p>
      <h1 className="text-xl font-semibold text-text mb-1">Sequence</h1>
      <p className="text-text-3 mb-5 max-w-2xl">
        The shortest real path between two entities, drawn as a sequence diagram — every arrow is a resolved edge
        this project's own resolver bound, never an authored or guessed relationship. The same data{' '}
        <code className="mono">ctx path</code> and <code className="mono">context_path</code> already return.
      </p>

      <div className="flex gap-2 mb-5 max-w-2xl">
        <Input value={from} onChange={(e) => setFrom(e.target.value)} placeholder="From entity name…" />
        <Input value={to} onChange={(e) => setTo(e.target.value)} placeholder="To entity name…" />
        <Button onClick={findPath} disabled={loading}>
          {loading ? 'Finding…' : 'Find path'}
        </Button>
      </div>

      {error && <p className="text-danger mb-4">{error}</p>}
      {result && !result.Found && (
        <p className="text-text-4">
          No path found from {result.From.Qualified} to {result.To.Qualified}.
        </p>
      )}
      {result?.Found && <SequenceDiagram result={result} />}
      {!result && !error && !loading && (
        <p className="text-text-4 text-sm">Enter two entity names above to see how one reaches the other.</p>
      )}
    </div>
  )
}

function SequenceDiagram({ result }: { result: PathResult }) {
  const hops = result.Path ?? []
  const participants: Entity[] = [result.From, ...hops.map((h) => h.Entity)]
  const width = PAD * 2 + COL_WIDTH * participants.length
  const height = HEADER_H + PAD + hops.length * HOP_H + PAD

  const colX = (i: number) => PAD + COL_WIDTH * i + COL_WIDTH / 2

  return (
    <div className="ag-scroll-mac border border-border rounded-lg bg-surface" style={{ maxHeight: '100%' }}>
      <svg width={width} height={height} className="block">
        <defs>
          <marker id="seq-arrow" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto">
            <path d="M0,0 L7,3 L0,6 Z" fill="var(--accent)" />
          </marker>
        </defs>

        {/* Lifelines */}
        {participants.map((_, i) => (
          <line
            key={i}
            x1={colX(i)}
            y1={HEADER_H}
            x2={colX(i)}
            y2={height - PAD / 2}
            stroke="var(--border-strong)"
            strokeDasharray="3 3"
          />
        ))}

        {/* Participant headers */}
        {participants.map((e, i) => (
          <g key={e.ID + i} transform={`translate(${colX(i) - COL_WIDTH / 2 + 8}, 6)`}>
            <rect width={COL_WIDTH - 16} height={HEADER_H - 14} rx={8} className="fill-surface-2 stroke-border" />
            <circle cx={14} cy={16} r={4} fill={`var(--pastel-${kindSlot(e.Kind)})`} />
            <text x={24} y={19} fontSize={10} fill="var(--text-3)" fontFamily="var(--font-mono)">
              {e.Kind}
            </text>
            <text x={12} y={34} fontSize={12} fontWeight={600} fill="var(--text)" fontFamily="var(--font-display)">
              {truncateMiddle(e.Name, 22)}
            </text>
          </g>
        ))}

        {/* Hop arrows, one per row, top to bottom in path order. Labels
            sit comfortably above the arrow line itself (a fixed gap, not
            derived from font metrics) — found via a live screenshot that
            an earlier, tighter spacing let the "conf" label visually
            cross the line beneath it. */}
        {hops.map((hop, i) => {
          const rowTop = HEADER_H + PAD + i * HOP_H
          const arrowY = rowTop + HOP_H - 16
          const x1 = colX(i)
          const x2 = colX(i + 1)
          return (
            <g key={i}>
              <line
                x1={x1}
                y1={arrowY}
                x2={x2 - 10}
                y2={arrowY}
                stroke="var(--accent)"
                strokeWidth={1.5}
                markerEnd="url(#seq-arrow)"
              />
              <text
                x={(x1 + x2) / 2}
                y={rowTop + 20}
                textAnchor="middle"
                fontSize={10.5}
                fontFamily="var(--font-mono)"
                fontWeight={600}
                fill="var(--accent-strong)"
              >
                {hop.Via.Kind}
              </text>
              <text
                x={(x1 + x2) / 2}
                y={rowTop + 34}
                textAnchor="middle"
                fontSize={9.5}
                fontFamily="var(--font-mono)"
                fill="var(--text-3)"
              >
                conf {hop.Via.Confidence.toFixed(2)}
              </text>
            </g>
          )
        })}
      </svg>
    </div>
  )
}

function truncateMiddle(s: string, max: number): string {
  if (s.length <= max) return s
  const half = Math.floor((max - 1) / 2)
  return s.slice(0, half) + '…' + s.slice(s.length - half)
}

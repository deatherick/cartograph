// Duplicates view (ADR-0021/ADR-0025's Similarity/Duplicate Engine,
// finally with somewhere to look at it — docs/MVP.md's own "data now
// exists, but no UI panel reads it yet" gap). Every score is shown fully
// decomposed (Exact/Structural/Behavioral/Overall), never collapsed to
// one number — the same "never prescriptive, evidence not a verdict"
// rule `ctx duplicates`/`ctx decide` already follow; recording a decision
// here is the exact same internal/service.Decide the CLI calls, so a
// pair decided from the browser never resurfaces on the CLI either, and
// vice versa.
//
// Card layout restyled as part of this project's Observatory-inspired
// redesign (docs/adr): scores now use accent2 (this page's own reserved
// "pattern match" identity color, tokens.css) instead of the generic
// success/warning ramp — a duplicate candidate is a SIMILARITY signal,
// not a health status, and deserves its own visual language rather than
// borrowing the "is this okay or not" one every other status pill uses.
import { useMemo, useState } from 'react'
import { Search as SearchIcon } from 'lucide-react'
import { api, DECISIONS, type Decision, type PairWithEntities } from '@/lib/api'
import { Badge, Button, Card, CardBody, CardHeader, Input } from '@/components/ui'
import { useProject } from '@/lib/project-context'
import { usePoll } from '@/hooks/usePoll'

export function DuplicatesPage() {
  const { project } = useProject()
  const [pairs, setPairs] = useState<PairWithEntities[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  // Every undecided pair loads up front (usePoll above) — this page's own
  // context for the same predictive-search pattern ADR-0031 introduced:
  // client-side, not api.suggest, since the whole list is already in
  // memory and a duplicate pair is only ever "similar to a name I'm
  // typing" against entities already resolved server-side, not a fuzzy
  // lookup of its own. A real gap before this: with more than a handful
  // of undecided pairs, there was no way to jump to one specific entity's
  // duplicates short of scanning the whole list by eye.
  const [query, setQuery] = useState('')

  usePoll(
    () => {
      api
        .duplicates(project)
        .then((got) => {
          setPairs(got)
          setError(null)
        })
        .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    },
    [project],
    5000, // a deliberately slower cadence than Overview's 3s (ADR-0026's
    // own reasoning for ctxd's own registry poll applies here too: this
    // view changes when a human records a decision or the graph is
    // reindexed, neither of which happens as often as a live stats
    // counter ticking).
  )

  const filteredPairs = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return pairs
    return (pairs ?? []).filter(
      (p) => p.A.Name.toLowerCase().includes(q) || p.B.Name.toLowerCase().includes(q) ||
        p.A.Qualified.toLowerCase().includes(q) || p.B.Qualified.toLowerCase().includes(q),
    )
  }, [pairs, query])

  async function decide(pair: PairWithEntities, decision: Decision) {
    try {
      await api.decide(project, pair.A.Name, pair.A.Anchor.File, pair.B.Name, pair.B.Anchor.File, decision)
      // Optimistic removal — the next poll would drop it anyway, but a
      // human recording a decision expects it gone immediately, not up
      // to 5 seconds later.
      setPairs((prev) => (prev ?? []).filter((p) => p.Pair.A !== pair.Pair.A || p.Pair.B !== pair.Pair.B))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="p-6 max-w-4xl">
      <p className="eyebrow mb-2">Similarity engine</p>
      <h1 className="text-xl font-semibold text-text mb-1">Duplicates &amp; similarity</h1>
      <p className="text-text-3 mb-5 max-w-2xl">
        Every undecided candidate the similarity engine found — evidence, never a verdict. Record what you actually
        think of each pair; a decided pair stops resurfacing here (and in <code className="mono">ctx duplicates</code>
        ) once you do.
      </p>

      {error && <p className="text-danger mb-4">{error}</p>}
      {pairs === null && !error && <p className="text-text-3">Loading…</p>}
      {pairs !== null && pairs.length === 0 && (
        <p className="text-text-4">No undecided duplicate/similarity pairs above the default threshold.</p>
      )}

      {pairs !== null && pairs.length > 0 && (
        <div className="flex items-center gap-2 mb-4">
          <SearchIcon size={14} className="text-text-4 shrink-0" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter by entity name or qualified name…"
            className="max-w-sm"
          />
          <span className="text-text-3 text-sm">
            {filteredPairs?.length ?? 0} of {pairs.length} pairs
          </span>
        </div>
      )}

      <div className="flex flex-col gap-3">
        {filteredPairs?.map((pair) => (
          <PairCard key={`${pair.Pair.A}_${pair.Pair.B}`} pair={pair} onDecide={(d) => decide(pair, d)} />
        ))}
        {pairs !== null && pairs.length > 0 && filteredPairs?.length === 0 && (
          <p className="text-text-4">No undecided pair matches "{query}".</p>
        )}
      </div>
    </div>
  )
}

function PairCard({ pair, onDecide }: { pair: PairWithEntities; onDecide: (d: Decision) => void }) {
  const [decision, setDecision] = useState<Decision>(DECISIONS[0].value)

  return (
    <Card className="transition-shadow hover:shadow-[var(--shadow-2)]">
      <CardHeader className="flex items-center justify-between gap-4">
        <div className="flex flex-col gap-1.5 min-w-0">
          <EntityLine entity={pair.A} />
          <EntityLine entity={pair.B} />
        </div>
        {pair.Pair.Exact && <Badge tone="accent2">Exact match</Badge>}
      </CardHeader>
      <CardBody className="flex items-center justify-between gap-4 flex-wrap">
        <div className="flex items-center gap-2">
          <Badge tone="accent2" mono>
            overall {pair.Pair.Overall.toFixed(2)}
          </Badge>
          <Badge tone="neutral" mono>
            structural {pair.Pair.Structural.toFixed(2)}
          </Badge>
          <Badge tone="neutral" mono>
            behavioral {pair.Pair.Behavioral.toFixed(2)}
          </Badge>
        </div>
        <div className="flex items-center gap-2">
          <select
            className="h-8 rounded-md border border-border bg-surface text-text text-sm px-2"
            value={decision}
            onChange={(e) => setDecision(e.target.value as Decision)}
          >
            {DECISIONS.map((d) => (
              <option key={d.value} value={d.value}>
                {d.label}
              </option>
            ))}
          </select>
          <Button size="sm" onClick={() => onDecide(decision)}>
            Record
          </Button>
        </div>
      </CardBody>
    </Card>
  )
}

function EntityLine({ entity }: { entity: PairWithEntities['A'] }) {
  return (
    <div className="flex items-center gap-2 min-w-0">
      <Badge tone="accent">{entity.Kind}</Badge>
      <span className="mono text-text text-sm truncate">{entity.Qualified}</span>
    </div>
  )
}

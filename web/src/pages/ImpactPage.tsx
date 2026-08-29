// Standalone Impact route — git-diff analysis only. The "by entity" mode
// (search a name, see its blast radius) was removed after direct
// feedback: a bare text input with no visible context on what you're
// searching for ("un buscador que no se sabe lo que busca") was not
// intuitive. That workflow is now Overview's Impact tab instead — select
// a row in the entity table (kind, file, and everything else already
// visible) and its blast radius is right there, no separate search step.
// Git-diff analysis keeps its own page because it has no natural "row" to
// select from a table — it starts from a ref, not an entity.
import { useState } from 'react'
import { api, type GitDiffImpact } from '@/lib/api'
import { Input, Button } from '@/components/ui'
import { EntitySection } from '@/components/EntityImpactPanel'

export function ImpactPage() {
  const [gitRef, setGitRef] = useState('HEAD')
  const [diffResult, setDiffResult] = useState<GitDiffImpact | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  async function runGitDiff() {
    setLoading(true)
    setError(null)
    try {
      setDiffResult(await api.impactFromGitDiff(gitRef))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setDiffResult(null)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="p-6 max-w-3xl">
      <h1 className="text-xl font-semibold text-text mb-1">Impact of a git diff</h1>
      <p className="text-text-3 mb-1">
        Blast radius of every entity a <code className="mono">git diff</code> touched — what changed, what
        transitively depends on it, and which tests to run.
      </p>
      <p className="text-text-4 text-sm mb-5">
        Looking for one specific entity's impact instead? Select it from the table on the Overview page — its Impact
        tab shows this same analysis with no search step.
      </p>

      <div className="flex gap-2 mb-4">
        <Input value={gitRef} onChange={(e) => setGitRef(e.target.value)} placeholder="Ref (default HEAD)" />
        <Button onClick={runGitDiff}>Analyze diff</Button>
      </div>

      {loading && <p className="text-text-3">Analyzing…</p>}
      {error && <p className="text-danger">{error}</p>}

      {diffResult && (
        <div className="mt-4">
          <EntitySection title={`Changed entities (${diffResult.ChangedEntities?.length ?? 0})`} entities={diffResult.ChangedEntities} />
          <EntitySection
            title={`Impacted, transitive union (${diffResult.ImpactedEntities?.length ?? 0})`}
            entities={diffResult.ImpactedEntities}
            className="mt-4"
          />
          <EntitySection
            title={`Recommended tests to run (${diffResult.RecommendedTests?.length ?? 0})`}
            entities={diffResult.RecommendedTests}
            className="mt-4"
          />
        </div>
      )}
    </div>
  )
}

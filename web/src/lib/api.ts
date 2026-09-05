// Typed client for internal/httpserver's JSON API
// (internal/httpserver/httpserver.go) — every shape here matches exactly
// what the Go handlers actually encode (some use explicit lowercase
// `json:"..."` tags — /api/stats, /api/source — most default to Go's
// PascalCase field names since model.Entity/model.Edge/service.* carry no
// json tags). No normalization layer: this file is the single place that
// casing quirk is documented, so a page component never has to remember
// which endpoint uses which casing.

export interface Anchor {
  File: string
  StartByte: number
  EndByte: number
  StartLine: number
  EndLine: number
  ContentHash: string
}

export interface Entity {
  ID: string
  Kind: string
  Lang: string
  Repo: string
  Qualified: string
  Name: string
  Signature: string
  DocSummary: string
  Anchor: Anchor
}

export interface Edge {
  ID: string
  Src: string
  Dst: string
  Kind: string
  Confidence: number
  Provenance: string
  Evidence: string
}

export interface RelatedEntity {
  Entity: Entity
  Depth: number
  Via: Edge
}

export interface Inspection {
  Entity: Entity
  FanIn: Edge[]
  FanOut: Edge[]
}

export interface GraphData {
  Entities: Entity[]
  Edges: Edge[]
}

export interface ImpactResult {
  Target: Entity
  DirectCallers: Entity[] | null
  Transitive: RelatedEntity[] | null
  CoveringTests: Entity[] | null
}

export interface GitDiffImpact {
  ChangedEntities: Entity[] | null
  ImpactedEntities: Entity[] | null
  RecommendedTests: Entity[] | null
}

export interface Stats {
  repo: string
  entities: number
  edges: number
  byKind: Record<string, number>
}

// Pair mirrors internal/similar.Pair — every score fully decomposed,
// never a single opaque number (ADR-0021's own standing rule; the UI
// below shows every field, not just Overall).
export interface Pair {
  A: string
  B: string
  Exact: boolean
  Structural: number
  Behavioral: number
  Overall: number
}

// PairWithEntities mirrors internal/service.PairWithEntities — a Pair
// plus both entities it names, resolved once server-side so the UI never
// needs a second lookup per pair.
export interface PairWithEntities {
  Pair: Pair
  A: Entity
  B: Entity
}

// Decision mirrors internal/similar.Decision's five valid values
// (similar.ValidDecisions()) — kept as a literal union, not a bare
// string, so an invalid value is a compile-time error in the UI, not
// just a 400 from /api/decide.
export type Decision = 'ignore' | 'intentional' | 'same-pattern' | 'should-share-abstraction' | 'false-positive'

export const DECISIONS: { value: Decision; label: string }[] = [
  { value: 'same-pattern', label: 'Same pattern (expected)' },
  { value: 'should-share-abstraction', label: 'Should share an abstraction' },
  { value: 'intentional', label: 'Intentional duplication' },
  { value: 'false-positive', label: 'False positive' },
  { value: 'ignore', label: 'Ignore' },
]

// Project is one entry from /api/projects — the daemon-side multi-project
// list (ADR-0019). watching reflects that project's opstatus.Tracker, if
// it has one (a project ctxd is only indexing, never watching, always
// reports false, not an error).
export interface Project {
  name: string
  repo: string
  root: string
  watching: boolean
}

// Operations is /api/operations' shape — internal/opstatus.Status
// (Go's PascalCase field names, since that struct carries no json tags;
// see this file's header note on casing).
export interface Operations {
  StartedAt: string
  Watching: boolean
  ReindexCount: number
  LastReindexAt: string
  LastReason: string
  LastStats: { Files: number; Entities: number; ResolvedEdges: number; Dispositions: Record<string, number> }
  LastError: string
  LastWatchError: string
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path)
  const data = await res.json()
  if (!res.ok) {
    throw new Error((data as { error?: string }).error ?? res.statusText)
  }
  return data as T
}

// postJSON is only used by /api/decide today — this server's one
// mutating endpoint (see internal/httpserver's own doc on why that one
// is POST while everything else is a GET). A non-2xx response's body is
// plain text (http.Error), not JSON, unlike getJSON's error path.
async function postJSON<T>(path: string): Promise<T> {
  const res = await fetch(path, { method: 'POST' })
  if (!res.ok) {
    throw new Error((await res.text()) || res.statusText)
  }
  return (await res.json()) as T
}

// withProject appends ?project=<name> (or &project=<name> if the path
// already has a query string) unless project is empty — every endpoint
// but /api/projects itself is scoped this way (ADR-0019); omitting it
// entirely falls back to the daemon's first/default project, so callers
// mid-load (project not resolved yet) degrade gracefully instead of
// erroring.
function withProject(path: string, project: string): string {
  if (!project) return path
  const sep = path.includes('?') ? '&' : '?'
  return `${path}${sep}project=${encodeURIComponent(project)}`
}

export const api = {
  projects: () => getJSON<Project[]>('/api/projects'),
  operations: (project: string) => getJSON<Operations>(withProject('/api/operations', project)),
  stats: (project: string) => getJSON<Stats>(withProject('/api/stats', project)),
  graph: (project: string) => getJSON<GraphData>(withProject('/api/graph', project)),
  find: (project: string, name: string) =>
    getJSON<Entity[]>(withProject(`/api/find?name=${encodeURIComponent(name)}`, project)),
  inspect: (project: string, name: string, file = '') =>
    getJSON<Inspection>(
      withProject(`/api/inspect?name=${encodeURIComponent(name)}&file=${encodeURIComponent(file)}`, project),
    ),
  related: (project: string, name: string, file = '', depth = 2) =>
    getJSON<RelatedEntity[]>(
      withProject(
        `/api/related?name=${encodeURIComponent(name)}&file=${encodeURIComponent(file)}&depth=${depth}`,
        project,
      ),
    ),
  impact: (project: string, name: string, file = '', depth = 0) =>
    getJSON<ImpactResult>(
      withProject(
        `/api/impact?name=${encodeURIComponent(name)}&file=${encodeURIComponent(file)}&depth=${depth}`,
        project,
      ),
    ),
  impactFromGitDiff: (project: string, gitRef = '', depth = 0) =>
    getJSON<GitDiffImpact>(withProject(`/api/impact?gitDiff=${encodeURIComponent(gitRef)}&depth=${depth}`, project)),
  source: (project: string, name: string, file = '') =>
    getJSON<{ entity: Entity; source: string }>(
      withProject(`/api/source?name=${encodeURIComponent(name)}&file=${encodeURIComponent(file)}`, project),
    ),
  duplicates: (project: string, threshold = 0) =>
    getJSON<PairWithEntities[]>(withProject(`/api/duplicates?threshold=${threshold}`, project)),
  similar: (project: string, name: string, file = '') =>
    getJSON<{ match: Entity; pairs: PairWithEntities[] }>(
      withProject(`/api/similar?name=${encodeURIComponent(name)}&file=${encodeURIComponent(file)}`, project),
    ),
  decide: (project: string, nameA: string, fileA: string, nameB: string, fileB: string, decision: Decision) =>
    postJSON<{ ok: boolean }>(
      withProject(
        `/api/decide?nameA=${encodeURIComponent(nameA)}&fileA=${encodeURIComponent(fileA)}` +
          `&nameB=${encodeURIComponent(nameB)}&fileB=${encodeURIComponent(fileB)}&decision=${decision}`,
        project,
      ),
    ),
}

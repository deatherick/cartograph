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
}

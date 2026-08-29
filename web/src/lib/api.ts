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

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path)
  const data = await res.json()
  if (!res.ok) {
    throw new Error((data as { error?: string }).error ?? res.statusText)
  }
  return data as T
}

export const api = {
  stats: () => getJSON<Stats>('/api/stats'),
  graph: () => getJSON<GraphData>('/api/graph'),
  find: (name: string) => getJSON<Entity[]>(`/api/find?name=${encodeURIComponent(name)}`),
  inspect: (name: string, file = '') =>
    getJSON<Inspection>(`/api/inspect?name=${encodeURIComponent(name)}&file=${encodeURIComponent(file)}`),
  related: (name: string, file = '', depth = 2) =>
    getJSON<RelatedEntity[]>(
      `/api/related?name=${encodeURIComponent(name)}&file=${encodeURIComponent(file)}&depth=${depth}`,
    ),
  impact: (name: string, file = '', depth = 0) =>
    getJSON<ImpactResult>(
      `/api/impact?name=${encodeURIComponent(name)}&file=${encodeURIComponent(file)}&depth=${depth}`,
    ),
  impactFromGitDiff: (gitRef = '', depth = 0) =>
    getJSON<GitDiffImpact>(`/api/impact?gitDiff=${encodeURIComponent(gitRef)}&depth=${depth}`),
  source: (name: string, file = '') =>
    getJSON<{ entity: Entity; source: string }>(
      `/api/source?name=${encodeURIComponent(name)}&file=${encodeURIComponent(file)}`,
    ),
}

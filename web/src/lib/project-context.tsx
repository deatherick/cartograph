// The current-project selection (ADR-0019's multi-project daemon), shared
// across every page/panel via context rather than prop-drilled through
// Overview -> EntityDetail/EntityGraphPanel/EntityImpactPanel, which would
// otherwise need a `project` prop threaded into components that are also
// used standalone (GraphPage, ImpactPage) with no natural place to receive
// one. Selection is persisted to localStorage so it survives a reload and
// switching routes, but never trusted blindly: it's only applied once the
// real /api/projects list confirms that name still exists.
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, type Project } from './api'

const STORAGE_KEY = 'cartograph.selectedProject'

interface ProjectContextValue {
  projects: Project[]
  project: string
  setProject: (name: string) => void
  loading: boolean
  error: string | null
}

const ProjectContext = createContext<ProjectContextValue | null>(null)

export function ProjectProvider({ children }: { children: ReactNode }) {
  const [projects, setProjects] = useState<Project[]>([])
  const [project, setProjectState] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api
      .projects()
      .then((ps) => {
        setProjects(ps)
        let stored = ''
        try {
          stored = localStorage.getItem(STORAGE_KEY) ?? ''
        } catch {
          // localStorage can throw (private mode, disabled storage) —
          // falling through to the first project is the correct default.
        }
        const initial = ps.find((p) => p.name === stored)?.name ?? ps[0]?.name ?? ''
        setProjectState(initial)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false))
  }, [])

  function setProject(name: string) {
    setProjectState(name)
    try {
      localStorage.setItem(STORAGE_KEY, name)
    } catch {
      // Best-effort persistence only — losing the stored choice just means
      // the default project is picked again next load, not an error.
    }
  }

  const value = useMemo(
    () => ({ projects, project, setProject, loading, error }),
    [projects, project, loading, error],
  )

  return <ProjectContext.Provider value={value}>{children}</ProjectContext.Provider>
}

export function useProject(): ProjectContextValue {
  const ctx = useContext(ProjectContext)
  if (!ctx) {
    throw new Error('useProject must be used within a ProjectProvider')
  }
  return ctx
}

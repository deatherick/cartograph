import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import { AppShell } from '@/components/chrome/AppShell'
import { Overview } from '@/pages/Overview'
import { GraphPage } from '@/pages/GraphPage'
import { ImpactPage } from '@/pages/ImpactPage'
import { DuplicatesPage } from '@/pages/DuplicatesPage'
import { ProjectProvider } from '@/lib/project-context'

const router = createBrowserRouter([
  {
    element: <AppShell />,
    children: [
      { path: '/', element: <Overview /> },
      { path: '/graph', element: <GraphPage /> },
      { path: '/impact', element: <ImpactPage /> },
      { path: '/duplicates', element: <DuplicatesPage /> },
    ],
  },
])

export function App() {
  return (
    <ProjectProvider>
      <RouterProvider router={router} />
    </ProjectProvider>
  )
}

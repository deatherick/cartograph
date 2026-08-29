import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import { AppShell } from '@/components/chrome/AppShell'
import { Overview } from '@/pages/Overview'
import { GraphPage } from '@/pages/GraphPage'
import { ImpactPage } from '@/pages/ImpactPage'
import { ProjectProvider } from '@/lib/project-context'

const router = createBrowserRouter([
  {
    element: <AppShell />,
    children: [
      { path: '/', element: <Overview /> },
      { path: '/graph', element: <GraphPage /> },
      { path: '/impact', element: <ImpactPage /> },
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

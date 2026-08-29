import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import { AppShell } from '@/components/chrome/AppShell'
import { Overview } from '@/pages/Overview'
import { GraphPage } from '@/pages/GraphPage'
import { ImpactPage } from '@/pages/ImpactPage'

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
  return <RouterProvider router={router} />
}

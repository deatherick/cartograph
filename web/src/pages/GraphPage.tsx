// Standalone Graph route — free-form exploration by typing any entity
// name, or arriving via ?name=&file= from another page's "Graph" link.
// All the actual logic lives in EntityGraphPanel (also embedded inline as
// a tab in Overview) so there is exactly one graph implementation, not
// two.
import { useSearchParams } from 'react-router-dom'
import { EntityGraphPanel } from '@/components/EntityGraphPanel'

export function GraphPage() {
  const [params] = useSearchParams()
  return <EntityGraphPanel initialName={params.get('name') ?? ''} initialFile={params.get('file') ?? ''} />
}

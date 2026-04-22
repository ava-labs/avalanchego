import { useState, useMemo } from 'react'
import { CreateParty } from './pages/CreateParty'
import { JoinParty } from './pages/JoinParty'

export function App() {
  const [view, setView] = useState<{ page: 'create' | 'join'; partyId?: string }>(() => {
    const path = window.location.pathname
    const match = path.match(/^\/join\/(.+)$/)
    if (match?.[1]) return { page: 'join', partyId: match[1] }
    return { page: 'create' }
  })

  const handleCreated = useMemo(() => (partyId: string) => {
    window.history.pushState(null, '', `/join/${partyId}`)
    setView({ page: 'join', partyId })
  }, [])

  if (view.page === 'join' && view.partyId) {
    return <JoinParty partyId={view.partyId} />
  }

  return <CreateParty onCreated={handleCreated} />
}

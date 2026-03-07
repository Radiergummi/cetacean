import { useState, useEffect } from 'react'

export default function ConnectionStatus() {
  const [connected, setConnected] = useState(true)

  useEffect(() => {
    const es = new EventSource('/api/events')
    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    return () => es.close()
  }, [])

  return (
    <div className="flex items-center gap-1.5" title={connected ? 'Connected' : 'Reconnecting...'}>
      <div className={`w-2 h-2 rounded-full ${connected ? 'bg-green-500' : 'bg-red-500 animate-pulse'}`} />
      <span className="text-xs text-muted-foreground hidden sm:inline">
        {connected ? 'Live' : 'Reconnecting'}
      </span>
    </div>
  )
}

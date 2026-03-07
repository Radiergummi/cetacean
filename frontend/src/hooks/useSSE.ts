import { useEffect, useRef } from 'react'

interface SSEEvent {
  type: string
  action: string
  id: string
  resource?: any
}

export function useSSE(
  types: string[],
  onEvent: (event: SSEEvent) => void,
) {
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  useEffect(() => {
    const params = types.length > 0 ? `?types=${types.join(',')}` : ''
    const es = new EventSource(`/api/events${params}`)

    const handler = (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as SSEEvent
        onEventRef.current(data)
      } catch {
        // ignore malformed events
      }
    }

    for (const type of types) {
      es.addEventListener(type, handler)
    }

    if (types.length === 0) {
      es.onmessage = handler
    }

    return () => es.close()
  }, [types.join(',')])
}

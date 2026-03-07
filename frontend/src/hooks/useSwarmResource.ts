import { useState, useEffect, useCallback, useRef } from 'react'
import { useSSE } from './useSSE'

export function useSwarmResource<T>(
  fetchFn: () => Promise<T[]>,
  sseType: string,
  getId: (item: T) => string,
) {
  const [data, setData] = useState<T[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)
  const getIdRef = useRef(getId)
  getIdRef.current = getId

  useEffect(() => {
    fetchFn()
      .then(setData)
      .catch(setError)
      .finally(() => setLoading(false))
  }, [])

  useSSE([sseType], useCallback((event) => {
    if (event.action === 'remove') {
      setData(prev => prev.filter(item => getIdRef.current(item) !== event.id))
    } else if (event.resource) {
      setData(prev => {
        const idx = prev.findIndex(item => getIdRef.current(item) === event.id)
        if (idx >= 0) {
          const next = [...prev]
          next[idx] = event.resource
          return next
        }
        return [...prev, event.resource]
      })
    }
  }, []))

  return { data, loading, error }
}

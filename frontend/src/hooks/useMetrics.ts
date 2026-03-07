import { useState, useEffect } from 'react'
import { api } from '../api/client'

const RANGE_SECONDS: Record<string, number> = {
  '1h': 3600,
  '6h': 21600,
  '24h': 86400,
  '7d': 604800,
}

export function useMetrics(query: string, range: string) {
  const [data, setData] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!query) return

    const rangeSec = RANGE_SECONDS[range] || 3600
    const now = Math.floor(Date.now() / 1000)
    const start = now - rangeSec
    const step = Math.max(Math.floor(rangeSec / 300), 15)

    setLoading(true)
    api.metricsQueryRange(query, String(start), String(now), String(step))
      .then((resp) => {
        setData(resp.data)
        setError(null)
      })
      .catch(() => setError('Failed to load metrics'))
      .finally(() => setLoading(false))
  }, [query, range])

  return { data, loading, error }
}

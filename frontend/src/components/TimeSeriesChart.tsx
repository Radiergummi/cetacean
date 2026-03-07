import { useEffect, useRef, useState } from 'react'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { api } from '../api/client'

interface Props {
  title: string
  query: string
  range: string
  unit?: string
  refreshKey?: number
}

const RANGE_SECONDS: Record<string, number> = {
  '1h': 3600,
  '6h': 21600,
  '24h': 86400,
  '7d': 604800,
}

export default function TimeSeriesChart({ title, query, range, unit, refreshKey }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<uPlot | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    const rangeSec = RANGE_SECONDS[range] || 3600
    const now = Math.floor(Date.now() / 1000)
    const start = now - rangeSec
    const step = Math.max(Math.floor(rangeSec / 300), 15)

    api.metricsQueryRange(query, String(start), String(now), String(step))
      .then((resp: any) => {
        if (cancelled) return

        if (!resp.data?.result?.length) {
          setError('No data')
          return
        }
        setError(null)

        const series = resp.data.result
        const timestamps = series[0].values.map((v: any) => Number(v[0]))
        const data: uPlot.AlignedData = [
          timestamps,
          ...series.map((s: any) => s.values.map((v: any) => Number(v[1]))),
        ]

        if (chartRef.current) chartRef.current.destroy()

        if (!containerRef.current) return

        const opts: uPlot.Options = {
          width: containerRef.current.clientWidth || 600,
          height: 200,
          series: [
            {},
            ...series.map((_s: any, i: number) => ({
              label: `series-${i}`,
              stroke: `hsl(${i * 60}, 70%, 50%)`,
            })),
          ],
          axes: [
            {},
            { label: unit || '' },
          ],
        }

        chartRef.current = new uPlot(opts, data, containerRef.current)
      })
      .catch(() => {
        if (!cancelled) setError('Failed to load metrics')
      })

    return () => {
      cancelled = true
      if (chartRef.current) {
        chartRef.current.destroy()
        chartRef.current = null
      }
    }
  }, [query, range, refreshKey])

  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="text-sm font-medium mb-2">{title}</div>
      {error ? (
        <div className="text-sm text-muted-foreground h-[200px] flex items-center justify-center">{error}</div>
      ) : (
        <div ref={containerRef} />
      )}
    </div>
  )
}

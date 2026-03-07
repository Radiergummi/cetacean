import { useState, useEffect } from 'react'
import TimeSeriesChart from './TimeSeriesChart'

interface ChartDef {
  title: string
  query: string
  unit?: string
}

interface Props {
  charts: ChartDef[]
}

const RANGES = ['1h', '6h', '24h', '7d'] as const

export default function MetricsPanel({ charts }: Props) {
  const [range, setRange] = useState<string>('1h')
  const [refreshKey, setRefreshKey] = useState(0)
  const [autoRefresh, setAutoRefresh] = useState(false)

  useEffect(() => {
    if (!autoRefresh) return
    const interval = setInterval(() => setRefreshKey(k => k + 1), 30000)
    return () => clearInterval(interval)
  }, [autoRefresh])

  return (
    <div>
      <div className="flex flex-wrap items-center gap-2 mb-4">
        <span className="text-sm text-muted-foreground">Time range:</span>
        {RANGES.map(r => (
          <button
            key={r}
            onClick={() => setRange(r)}
            className={`px-3 py-1 text-sm rounded ${
              range === r ? 'bg-primary text-primary-foreground' : 'bg-muted'
            }`}
          >
            {r}
          </button>
        ))}
        <button
          onClick={() => setRefreshKey(k => k + 1)}
          className="px-3 py-1 text-sm rounded bg-muted hover:bg-muted/80"
          title="Refresh"
        >
          Refresh
        </button>
        <label className="flex items-center gap-1.5 text-sm text-muted-foreground cursor-pointer">
          <input
            type="checkbox"
            checked={autoRefresh}
            onChange={e => setAutoRefresh(e.target.checked)}
            className="rounded"
          />
          Auto (30s)
        </label>
      </div>
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {charts.map(chart => (
          <TimeSeriesChart key={chart.query} {...chart} range={range} refreshKey={refreshKey} />
        ))}
      </div>
    </div>
  )
}

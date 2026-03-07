import { useState, useEffect, useCallback } from 'react'
import { api, type ClusterSnapshot } from '../api/client'
import { useSSE } from '../hooks/useSSE'

export default function ClusterOverview() {
  const [snapshot, setSnapshot] = useState<ClusterSnapshot | null>(null)

  const fetchSnapshot = useCallback(() => {
    api.cluster().then(setSnapshot)
  }, [])

  useEffect(() => { fetchSnapshot() }, [fetchSnapshot])

  useSSE(['node', 'service', 'task', 'stack'], useCallback(() => {
    fetchSnapshot()
  }, [fetchSnapshot]))

  if (!snapshot) return <div>Loading...</div>

  const tasksRunning = snapshot.tasksByState?.['running'] || 0
  const tasksFailed = snapshot.tasksByState?.['failed'] || 0
  const tasksOther = snapshot.taskCount - tasksRunning - tasksFailed

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Cluster Overview</h1>
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatCard label="Nodes Ready" value={snapshot.nodesReady} color="green" />
        <StatCard label="Nodes Down" value={snapshot.nodesDown} color={snapshot.nodesDown > 0 ? 'red' : undefined} />
        <StatCard label="Services" value={snapshot.serviceCount} />
        <StatCard label="Stacks" value={snapshot.stackCount} />
        <StatCard label="Tasks Running" value={tasksRunning} color="green" />
        <StatCard label="Tasks Failed" value={tasksFailed} color={tasksFailed > 0 ? 'red' : undefined} />
        <StatCard label="Tasks Other" value={tasksOther} />
        <StatCard label="Tasks Total" value={snapshot.taskCount} />
      </div>
    </div>
  )
}

function StatCard({ label, value, color }: { label: string; value: number; color?: string }) {
  const valueColor = color === 'green' ? 'text-green-600'
    : color === 'red' ? 'text-red-600'
    : ''
  return (
    <div className="rounded-lg border bg-card p-6">
      <div className="text-sm text-muted-foreground">{label}</div>
      <div className={`text-3xl font-bold ${valueColor}`}>{value}</div>
    </div>
  )
}

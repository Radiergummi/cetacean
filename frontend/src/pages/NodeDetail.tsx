import { useParams } from 'react-router-dom'
import { useState, useEffect } from 'react'
import { api } from '../api/client'
import MetricsPanel from '../components/MetricsPanel'

export default function NodeDetail() {
  const { id } = useParams<{ id: string }>()
  const [node, setNode] = useState<any>(null)
  const [tasks, setTasks] = useState<any[]>([])

  useEffect(() => {
    if (id) {
      api.node(id).then(setNode)
      api.nodeTasks(id).then(setTasks).catch(() => {})
    }
  }, [id])

  if (!node) return <div>Loading...</div>

  const addr = node.Status?.Addr || ''

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">
        {node.Description?.Hostname || node.ID}
      </h1>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
        <InfoCard label="Role" value={node.Spec?.Role} />
        <InfoCard label="Status" value={node.Status?.State} />
        <InfoCard label="Availability" value={node.Spec?.Availability} />
        <InfoCard label="Engine" value={node.Description?.Engine?.EngineVersion} />
        <InfoCard label="OS" value={`${node.Description?.Platform?.OS || ''} ${node.Description?.Platform?.Architecture || ''}`} />
        <InfoCard label="Address" value={addr} />
      </div>

      {tasks.length > 0 && (
        <div className="mb-6">
          <h2 className="text-lg font-semibold mb-2">Tasks</h2>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">ID</th>
                  <th className="text-left p-3 text-sm font-medium">Service</th>
                  <th className="text-left p-3 text-sm font-medium">State</th>
                  <th className="text-left p-3 text-sm font-medium">Desired</th>
                  <th className="text-left p-3 text-sm font-medium">Error</th>
                  <th className="text-left p-3 text-sm font-medium">Timestamp</th>
                </tr>
              </thead>
              <tbody>
                {tasks.map((task: any) => {
                  const isFailed = task.Status?.State === 'failed' || task.Status?.State === 'rejected'
                  const exitCode = task.Status?.ContainerStatus?.ExitCode
                  const errorMsg = task.Status?.Err || (exitCode && exitCode !== 0 ? `exit ${exitCode}` : '')
                  return (
                    <tr key={task.ID} className={`border-b ${isFailed ? 'bg-red-50' : ''}`}>
                      <td className="p-3 text-sm font-mono text-xs">{task.ID?.slice(0, 12)}</td>
                      <td className="p-3 text-sm">{task.ServiceID?.slice(0, 12)}</td>
                      <td className="p-3 text-sm">
                        <TaskStatusBadge state={task.Status?.State} />
                      </td>
                      <td className="p-3 text-sm">{task.DesiredState}</td>
                      <td className="p-3 text-sm text-red-600">{errorMsg}</td>
                      <td className="p-3 text-sm text-muted-foreground">
                        {task.Status?.Timestamp ? new Date(task.Status.Timestamp).toLocaleString() : '\u2014'}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      <MetricsPanel charts={[
        { title: 'CPU Usage', query: `100 - (avg by(instance) (rate(node_cpu_seconds_total{mode="idle",instance=~"${addr}.*"}[5m])) * 100)`, unit: '%' },
        { title: 'Memory Usage', query: `(1 - node_memory_MemAvailable_bytes{instance=~"${addr}.*"} / node_memory_MemTotal_bytes{instance=~"${addr}.*"}) * 100`, unit: '%' },
        { title: 'Disk I/O', query: `rate(node_disk_read_bytes_total{instance=~"${addr}.*"}[5m])`, unit: 'bytes/s' },
        { title: 'Network I/O', query: `rate(node_network_receive_bytes_total{instance=~"${addr}.*"}[5m])`, unit: 'bytes/s' },
      ]} />
    </div>
  )
}

function InfoCard({ label, value }: { label: string; value?: string }) {
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="text-sm text-muted-foreground">{label}</div>
      <div className="text-lg font-medium">{value || '\u2014'}</div>
    </div>
  )
}

function TaskStatusBadge({ state }: { state?: string }) {
  const color = state === 'running' ? 'bg-green-100 text-green-800'
    : state === 'failed' || state === 'rejected' ? 'bg-red-100 text-red-800'
    : state === 'preparing' || state === 'starting' ? 'bg-yellow-100 text-yellow-800'
    : 'bg-gray-100 text-gray-800'
  return (
    <span className={`inline-block px-2 py-0.5 rounded text-xs font-medium ${color}`}>
      {state || 'unknown'}
    </span>
  )
}

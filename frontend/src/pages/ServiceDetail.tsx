import { useParams } from 'react-router-dom'
import { useState, useEffect } from 'react'
import { api } from '../api/client'
import MetricsPanel from '../components/MetricsPanel'

export default function ServiceDetail() {
  const { id } = useParams<{ id: string }>()
  const [service, setService] = useState<any>(null)
  const [tasks, setTasks] = useState<any[]>([])
  const [logs, setLogs] = useState<string>('')
  const [showLogs, setShowLogs] = useState(false)

  useEffect(() => {
    if (id) {
      api.service(id).then(setService)
      api.serviceTasks(id).then(setTasks).catch(() => {})
    }
  }, [id])

  if (!service) return <div>Loading...</div>

  const name = service.Spec?.Name || service.ID

  const loadLogs = () => {
    setShowLogs(true)
    api.serviceLogs(id!, 500).then(setLogs).catch(() => setLogs('Failed to load logs'))
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">{name}</h1>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
        <InfoCard label="Image" value={service.Spec?.TaskTemplate?.ContainerSpec?.Image?.split('@')[0]} />
        <InfoCard label="Mode" value={service.Spec?.Mode?.Replicated ? `replicated (${service.Spec.Mode.Replicated.Replicas})` : 'global'} />
        <InfoCard label="Update Status" value={service.UpdateStatus?.State} />
        <InfoCard label="Stack" value={service.Spec?.Labels?.['com.docker.stack.namespace']} />
      </div>

      {service.Endpoint?.Ports && service.Endpoint.Ports.length > 0 && (
        <div className="mb-6">
          <h2 className="text-lg font-semibold mb-2">Ports</h2>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">Published</th>
                  <th className="text-left p-3 text-sm font-medium">Target</th>
                  <th className="text-left p-3 text-sm font-medium">Protocol</th>
                </tr>
              </thead>
              <tbody>
                {service.Endpoint.Ports.map((port: any, i: number) => (
                  <tr key={i} className="border-b">
                    <td className="p-3 text-sm">{port.PublishedPort}</td>
                    <td className="p-3 text-sm">{port.TargetPort}</td>
                    <td className="p-3 text-sm">{port.Protocol}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {tasks.length > 0 && (
        <div className="mb-6">
          <h2 className="text-lg font-semibold mb-2">Tasks</h2>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">ID</th>
                  <th className="text-left p-3 text-sm font-medium">Slot</th>
                  <th className="text-left p-3 text-sm font-medium">State</th>
                  <th className="text-left p-3 text-sm font-medium">Node</th>
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
                      <td className="p-3 text-sm">{task.Slot}</td>
                      <td className="p-3 text-sm">
                        <TaskStatusBadge state={task.Status?.State} />
                      </td>
                      <td className="p-3 text-sm">{task.NodeID?.slice(0, 12)}</td>
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

      <div className="mb-6">
        <h2 className="text-lg font-semibold mb-2">Logs</h2>
        {!showLogs ? (
          <button
            onClick={loadLogs}
            className="px-4 py-2 text-sm rounded bg-primary text-primary-foreground hover:opacity-90"
          >
            View Logs
          </button>
        ) : (
          <pre className="bg-black text-green-400 p-4 rounded-lg text-xs font-mono overflow-auto max-h-96 mt-2">
            {logs || 'No logs available'}
          </pre>
        )}
      </div>

      <MetricsPanel charts={[
        { title: 'CPU Usage', query: `rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name="${name}"}[5m])`, unit: 'cores' },
        { title: 'Memory Usage', query: `container_memory_usage_bytes{container_label_com_docker_swarm_service_name="${name}"}`, unit: 'bytes' },
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

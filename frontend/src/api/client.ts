const BASE = '/api'

async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json()
}

export interface ClusterSnapshot {
  nodeCount: number
  serviceCount: number
  taskCount: number
  stackCount: number
  tasksByState: Record<string, number>
  nodesReady: number
  nodesDown: number
}

export const api = {
  cluster: () => fetchJSON<ClusterSnapshot>('/cluster'),
  nodes: () => fetchJSON<any[]>('/nodes'),
  node: (id: string) => fetchJSON<any>(`/nodes/${id}`),
  services: () => fetchJSON<any[]>('/services'),
  service: (id: string) => fetchJSON<any>(`/services/${id}`),
  tasks: () => fetchJSON<any[]>('/tasks'),
  stacks: () => fetchJSON<any[]>('/stacks'),
  stack: (name: string) => fetchJSON<any>(`/stacks/${name}`),
  configs: () => fetchJSON<any[]>('/configs'),
  secrets: () => fetchJSON<any[]>('/secrets'),
  networks: () => fetchJSON<any[]>('/networks'),
  volumes: () => fetchJSON<any[]>('/volumes'),
  serviceTasks: (id: string) => fetchJSON<any[]>(`/services/${id}/tasks`),
  serviceLogs: (id: string, tail?: number) =>
    fetch(`${BASE}/services/${id}/logs?tail=${tail || 200}`).then(r => r.text()),
  nodeTasks: (id: string) => fetchJSON<any[]>(`/nodes/${id}/tasks`),
  metricsQuery: (query: string, time?: string) => {
    const params = new URLSearchParams({ query })
    if (time) params.set('time', time)
    return fetchJSON<any>(`/metrics/query?${params}`)
  },
  metricsQueryRange: (query: string, start: string, end: string, step: string) => {
    const params = new URLSearchParams({ query, start, end, step })
    return fetchJSON<any>(`/metrics/query_range?${params}`)
  },
}

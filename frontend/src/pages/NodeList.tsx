import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useSwarmResource } from '../hooks/useSwarmResource'
import { api } from '../api/client'
import SearchInput from '../components/SearchInput'

export default function NodeList() {
  const { data: nodes, loading } = useSwarmResource(
    api.nodes,
    'node',
    (n: any) => n.ID,
  )
  const [search, setSearch] = useState('')

  if (loading) return <div>Loading...</div>

  const filtered = nodes.filter((n: any) =>
    (n.Description?.Hostname || n.ID).toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Nodes</h1>
      <SearchInput value={search} onChange={setSearch} placeholder="Search nodes..." />
      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="text-left p-3 text-sm font-medium">Hostname</th>
              <th className="text-left p-3 text-sm font-medium">Role</th>
              <th className="text-left p-3 text-sm font-medium">Status</th>
              <th className="text-left p-3 text-sm font-medium">Availability</th>
              <th className="text-left p-3 text-sm font-medium">Engine</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((node: any) => (
              <tr key={node.ID} className="border-b">
                <td className="p-3">
                  <Link to={`/nodes/${node.ID}`} className="text-blue-600 hover:underline">
                    {node.Description?.Hostname || node.ID}
                  </Link>
                </td>
                <td className="p-3 text-sm">{node.Spec?.Role}</td>
                <td className="p-3 text-sm">
                  <StatusBadge state={node.Status?.State} />
                </td>
                <td className="p-3 text-sm">{node.Spec?.Availability}</td>
                <td className="p-3 text-sm">{node.Description?.Engine?.EngineVersion}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function StatusBadge({ state }: { state?: string }) {
  const color = state === 'ready' ? 'bg-green-100 text-green-800'
    : state === 'down' ? 'bg-red-100 text-red-800'
    : 'bg-yellow-100 text-yellow-800'
  return (
    <span className={`inline-block px-2 py-0.5 rounded text-xs font-medium ${color}`}>
      {state || 'unknown'}
    </span>
  )
}

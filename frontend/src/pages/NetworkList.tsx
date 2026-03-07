import { useState } from 'react'
import { useSwarmResource } from '../hooks/useSwarmResource'
import { api } from '../api/client'
import SearchInput from '../components/SearchInput'

export default function NetworkList() {
  const { data: networks, loading } = useSwarmResource(
    api.networks,
    'network',
    (n: any) => n.Id,
  )
  const [search, setSearch] = useState('')

  if (loading) return <div>Loading...</div>

  const filtered = networks.filter((n: any) =>
    (n.Name || '').toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Networks</h1>
      <SearchInput value={search} onChange={setSearch} placeholder="Search networks..." />
      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="text-left p-3 text-sm font-medium">Name</th>
              <th className="text-left p-3 text-sm font-medium">Driver</th>
              <th className="text-left p-3 text-sm font-medium">Scope</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((net: any) => (
              <tr key={net.Id} className="border-b">
                <td className="p-3 text-sm">{net.Name}</td>
                <td className="p-3 text-sm">{net.Driver}</td>
                <td className="p-3 text-sm">{net.Scope}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

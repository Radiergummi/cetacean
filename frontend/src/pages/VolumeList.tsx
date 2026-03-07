import { useState } from 'react'
import { useSwarmResource } from '../hooks/useSwarmResource'
import { api } from '../api/client'
import SearchInput from '../components/SearchInput'

export default function VolumeList() {
  const { data: volumes, loading } = useSwarmResource(
    api.volumes,
    'volume',
    (v: any) => v.Name,
  )
  const [search, setSearch] = useState('')

  if (loading) return <div>Loading...</div>

  const filtered = volumes.filter((v: any) =>
    (v.Name || '').toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Volumes</h1>
      <SearchInput value={search} onChange={setSearch} placeholder="Search volumes..." />
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
            {filtered.map((vol: any) => (
              <tr key={vol.Name} className="border-b">
                <td className="p-3 text-sm">{vol.Name}</td>
                <td className="p-3 text-sm">{vol.Driver}</td>
                <td className="p-3 text-sm">{vol.Scope}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

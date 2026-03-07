import { useState } from 'react'
import { useSwarmResource } from '../hooks/useSwarmResource'
import { api } from '../api/client'
import SearchInput from '../components/SearchInput'

export default function ConfigList() {
  const { data: configs, loading } = useSwarmResource(
    api.configs,
    'config',
    (c: any) => c.ID,
  )
  const [search, setSearch] = useState('')

  if (loading) return <div>Loading...</div>

  const filtered = configs.filter((c: any) =>
    (c.Spec?.Name || c.ID).toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Configs</h1>
      <SearchInput value={search} onChange={setSearch} placeholder="Search configs..." />
      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="text-left p-3 text-sm font-medium">Name</th>
              <th className="text-left p-3 text-sm font-medium">Created</th>
              <th className="text-left p-3 text-sm font-medium">Updated</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((cfg: any) => (
              <tr key={cfg.ID} className="border-b">
                <td className="p-3 text-sm">{cfg.Spec?.Name || cfg.ID}</td>
                <td className="p-3 text-sm">{cfg.CreatedAt ? new Date(cfg.CreatedAt).toLocaleString() : '\u2014'}</td>
                <td className="p-3 text-sm">{cfg.UpdatedAt ? new Date(cfg.UpdatedAt).toLocaleString() : '\u2014'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

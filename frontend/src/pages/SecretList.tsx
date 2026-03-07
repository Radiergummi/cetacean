import { useState } from 'react'
import { useSwarmResource } from '../hooks/useSwarmResource'
import { api } from '../api/client'
import SearchInput from '../components/SearchInput'

export default function SecretList() {
  const { data: secrets, loading } = useSwarmResource(
    api.secrets,
    'secret',
    (s: any) => s.ID,
  )
  const [search, setSearch] = useState('')

  if (loading) return <div>Loading...</div>

  const filtered = secrets.filter((s: any) =>
    (s.Spec?.Name || s.ID).toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Secrets</h1>
      <p className="text-sm text-muted-foreground mb-4">Metadata only. Secret values are never exposed.</p>
      <SearchInput value={search} onChange={setSearch} placeholder="Search secrets..." />
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
            {filtered.map((secret: any) => (
              <tr key={secret.ID} className="border-b">
                <td className="p-3 text-sm">{secret.Spec?.Name || secret.ID}</td>
                <td className="p-3 text-sm">{secret.CreatedAt ? new Date(secret.CreatedAt).toLocaleString() : '\u2014'}</td>
                <td className="p-3 text-sm">{secret.UpdatedAt ? new Date(secret.UpdatedAt).toLocaleString() : '\u2014'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

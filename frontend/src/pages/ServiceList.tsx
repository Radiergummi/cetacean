import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useSwarmResource } from '../hooks/useSwarmResource'
import { api } from '../api/client'
import SearchInput from '../components/SearchInput'

export default function ServiceList() {
  const { data: services, loading } = useSwarmResource(
    api.services,
    'service',
    (s: any) => s.ID,
  )
  const [search, setSearch] = useState('')

  if (loading) return <div>Loading...</div>

  const filtered = services.filter((s: any) =>
    (s.Spec?.Name || s.ID).toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Services</h1>
      <SearchInput value={search} onChange={setSearch} placeholder="Search services..." />
      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="text-left p-3 text-sm font-medium">Name</th>
              <th className="text-left p-3 text-sm font-medium">Image</th>
              <th className="text-left p-3 text-sm font-medium">Mode</th>
              <th className="text-left p-3 text-sm font-medium">Replicas</th>
              <th className="text-left p-3 text-sm font-medium">Update Status</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((svc: any) => (
              <tr key={svc.ID} className="border-b">
                <td className="p-3">
                  <Link to={`/services/${svc.ID}`} className="text-blue-600 hover:underline">
                    {svc.Spec?.Name || svc.ID}
                  </Link>
                </td>
                <td className="p-3 text-sm font-mono text-xs">
                  {svc.Spec?.TaskTemplate?.ContainerSpec?.Image?.split('@')[0] || '\u2014'}
                </td>
                <td className="p-3 text-sm">
                  {svc.Spec?.Mode?.Replicated ? 'replicated' : 'global'}
                </td>
                <td className="p-3 text-sm">
                  {svc.Spec?.Mode?.Replicated?.Replicas ?? '\u2014'}
                </td>
                <td className="p-3 text-sm">
                  {svc.UpdateStatus?.State || '\u2014'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

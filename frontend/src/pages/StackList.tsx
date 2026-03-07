import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useSwarmResource } from '../hooks/useSwarmResource'
import { api } from '../api/client'
import SearchInput from '../components/SearchInput'

export default function StackList() {
  const { data: stacks, loading } = useSwarmResource(
    api.stacks,
    'stack',
    (s: any) => s.name,
  )
  const [search, setSearch] = useState('')

  if (loading) return <div>Loading...</div>

  const filtered = stacks.filter((s: any) =>
    (s.name || '').toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Stacks</h1>
      <SearchInput value={search} onChange={setSearch} placeholder="Search stacks..." />
      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="text-left p-3 text-sm font-medium">Name</th>
              <th className="text-left p-3 text-sm font-medium">Services</th>
              <th className="text-left p-3 text-sm font-medium">Configs</th>
              <th className="text-left p-3 text-sm font-medium">Secrets</th>
              <th className="text-left p-3 text-sm font-medium">Networks</th>
              <th className="text-left p-3 text-sm font-medium">Volumes</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((stack: any) => (
              <tr key={stack.name} className="border-b">
                <td className="p-3">
                  <Link to={`/stacks/${stack.name}`} className="text-blue-600 hover:underline">
                    {stack.name}
                  </Link>
                </td>
                <td className="p-3 text-sm">{stack.services?.length || 0}</td>
                <td className="p-3 text-sm">{stack.configs?.length || 0}</td>
                <td className="p-3 text-sm">{stack.secrets?.length || 0}</td>
                <td className="p-3 text-sm">{stack.networks?.length || 0}</td>
                <td className="p-3 text-sm">{stack.volumes?.length || 0}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

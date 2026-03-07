import { useParams, Link } from 'react-router-dom'
import { useState, useEffect } from 'react'
import { api } from '../api/client'

export default function StackDetail() {
  const { name } = useParams<{ name: string }>()
  const [stack, setStack] = useState<any>(null)
  const [services, setServices] = useState<any[]>([])
  const [configs, setConfigs] = useState<any[]>([])
  const [secrets, setSecrets] = useState<any[]>([])
  const [networks, setNetworks] = useState<any[]>([])

  useEffect(() => {
    if (name) {
      api.stack(name).then(async (s) => {
        setStack(s)
        const svcIds = new Set(s.services || [])
        const cfgIds = new Set(s.configs || [])
        const secIds = new Set(s.secrets || [])
        const netIds = new Set(s.networks || [])

        if (svcIds.size > 0) {
          api.services().then(all => setServices(all.filter((svc: any) => svcIds.has(svc.ID))))
        }
        if (cfgIds.size > 0) {
          api.configs().then(all => setConfigs(all.filter((c: any) => cfgIds.has(c.ID))))
        }
        if (secIds.size > 0) {
          api.secrets().then(all => setSecrets(all.filter((s: any) => secIds.has(s.ID))))
        }
        if (netIds.size > 0) {
          api.networks().then(all => setNetworks(all.filter((n: any) => netIds.has(n.Id))))
        }
      })
    }
  }, [name])

  if (!stack) return <div>Loading...</div>

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Stack: {stack.name}</h1>
      <div className="space-y-6">
        {services.length > 0 && (
          <div>
            <h2 className="text-lg font-semibold mb-2">Services</h2>
            <div className="overflow-x-auto rounded-lg border">
              <table className="w-full">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="text-left p-3 text-sm font-medium">Name</th>
                    <th className="text-left p-3 text-sm font-medium">Image</th>
                    <th className="text-left p-3 text-sm font-medium">Mode</th>
                  </tr>
                </thead>
                <tbody>
                  {services.map((svc: any) => (
                    <tr key={svc.ID} className="border-b last:border-b-0">
                      <td className="p-3 text-sm">
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
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {configs.length > 0 && (
          <div>
            <h2 className="text-lg font-semibold mb-2">Configs</h2>
            <div className="overflow-x-auto rounded-lg border">
              <table className="w-full">
                <tbody>
                  {configs.map((c: any) => (
                    <tr key={c.ID} className="border-b last:border-b-0">
                      <td className="p-3 text-sm">{c.Spec?.Name || c.ID}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {secrets.length > 0 && (
          <div>
            <h2 className="text-lg font-semibold mb-2">Secrets</h2>
            <div className="overflow-x-auto rounded-lg border">
              <table className="w-full">
                <tbody>
                  {secrets.map((s: any) => (
                    <tr key={s.ID} className="border-b last:border-b-0">
                      <td className="p-3 text-sm">{s.Spec?.Name || s.ID}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {networks.length > 0 && (
          <div>
            <h2 className="text-lg font-semibold mb-2">Networks</h2>
            <div className="overflow-x-auto rounded-lg border">
              <table className="w-full">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="text-left p-3 text-sm font-medium">Name</th>
                    <th className="text-left p-3 text-sm font-medium">Driver</th>
                  </tr>
                </thead>
                <tbody>
                  {networks.map((n: any) => (
                    <tr key={n.Id} className="border-b last:border-b-0">
                      <td className="p-3 text-sm">{n.Name}</td>
                      <td className="p-3 text-sm">{n.Driver}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        <ResourceList title="Volumes" items={stack.volumes} />
      </div>
    </div>
  )
}

function ResourceList({ title, items }: { title: string; items?: string[] }) {
  if (!items || items.length === 0) return null
  return (
    <div>
      <h2 className="text-lg font-semibold mb-2">{title}</h2>
      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full">
          <tbody>
            {items.map((item) => (
              <tr key={item} className="border-b last:border-b-0">
                <td className="p-3 text-sm">{item}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

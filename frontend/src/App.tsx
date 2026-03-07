import { useState } from 'react'
import { BrowserRouter, Routes, Route, Link } from 'react-router-dom'
import ClusterOverview from './pages/ClusterOverview'
import NodeList from './pages/NodeList'
import NodeDetail from './pages/NodeDetail'
import StackList from './pages/StackList'
import StackDetail from './pages/StackDetail'
import ServiceList from './pages/ServiceList'
import ServiceDetail from './pages/ServiceDetail'
import ConfigList from './pages/ConfigList'
import SecretList from './pages/SecretList'
import NetworkList from './pages/NetworkList'
import VolumeList from './pages/VolumeList'
import ConnectionStatus from './components/ConnectionStatus'

function Layout({ children }: { children: React.ReactNode }) {
  const [menuOpen, setMenuOpen] = useState(false)

  return (
    <div className="min-h-screen bg-background">
      <nav className="border-b px-6 py-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            <Link to="/" className="font-bold text-lg">Cetacean</Link>
            <ConnectionStatus />
          </div>
          <button
            className="md:hidden p-2"
            onClick={() => setMenuOpen(!menuOpen)}
            aria-label="Toggle menu"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              {menuOpen
                ? <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                : <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
              }
            </svg>
          </button>
          <div className="hidden md:flex items-center gap-6">
            <NavLinks />
          </div>
        </div>
        {menuOpen && (
          <div className="md:hidden flex flex-col gap-2 pt-3" onClick={() => setMenuOpen(false)}>
            <NavLinks />
          </div>
        )}
      </nav>
      <main className="p-6">{children}</main>
    </div>
  )
}

function NavLinks() {
  const links = [
    { to: '/nodes', label: 'Nodes' },
    { to: '/stacks', label: 'Stacks' },
    { to: '/services', label: 'Services' },
    { to: '/configs', label: 'Configs' },
    { to: '/secrets', label: 'Secrets' },
    { to: '/networks', label: 'Networks' },
    { to: '/volumes', label: 'Volumes' },
  ]
  return (
    <>
      {links.map(l => (
        <Link key={l.to} to={l.to} className="text-sm text-muted-foreground hover:text-foreground">
          {l.label}
        </Link>
      ))}
    </>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <Layout>
        <Routes>
          <Route path="/" element={<ClusterOverview />} />
          <Route path="/nodes" element={<NodeList />} />
          <Route path="/nodes/:id" element={<NodeDetail />} />
          <Route path="/stacks" element={<StackList />} />
          <Route path="/stacks/:name" element={<StackDetail />} />
          <Route path="/services" element={<ServiceList />} />
          <Route path="/services/:id" element={<ServiceDetail />} />
          <Route path="/configs" element={<ConfigList />} />
          <Route path="/secrets" element={<SecretList />} />
          <Route path="/networks" element={<NetworkList />} />
          <Route path="/volumes" element={<VolumeList />} />
        </Routes>
      </Layout>
    </BrowserRouter>
  )
}

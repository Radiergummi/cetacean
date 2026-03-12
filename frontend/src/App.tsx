import { Menu, X } from "lucide-react";
import type React from "react";
import { useEffect, useRef, useState } from "react";
import { BrowserRouter, Link, Route, Routes, useLocation } from "react-router-dom";
import ConnectionStatus from "./components/ConnectionStatus";
import ErrorBoundary from "./components/ErrorBoundary";
import { GlobalSearch } from "./components/search";
import ThemeToggle from "./components/ThemeToggle";
import { ConnectionProvider, SSE_EVENT_TYPES } from "./hooks/useResourceStream";
import ClusterOverview from "./pages/ClusterOverview";
import ConfigDetail from "./pages/ConfigDetail";
import ConfigList from "./pages/ConfigList";
import NetworkDetail from "./pages/NetworkDetail";
import NetworkList from "./pages/NetworkList";
import NodeDetail from "./pages/NodeDetail";
import NodeList from "./pages/NodeList";
import NotFound from "./pages/NotFound";
import SearchPage from "./pages/SearchPage";
import SecretDetail from "./pages/SecretDetail";
import SecretList from "./pages/SecretList";
import ServiceDetail from "./pages/ServiceDetail";
import ServiceList from "./pages/ServiceList";
import StackDetail from "./pages/StackDetail";
import StackList from "./pages/StackList";
import SwarmPage from "./pages/SwarmPage";
import TaskDetail from "./pages/TaskDetail";
import Topology from "./pages/Topology";
import VolumeDetail from "./pages/VolumeDetail";
import VolumeList from "./pages/VolumeList";

function Layout({ children }: { children: React.ReactNode }) {
  const [menuOpen, setMenuOpen] = useState(false);

  return (
    <div className="min-h-screen bg-background">
      <nav className="sticky top-0 z-30 border-b bg-background/80 backdrop-blur-sm">
        <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
          <div className="flex h-14 items-center justify-between">
            <div className="flex items-center gap-3">
              <Link to="/" className="font-semibold text-base tracking-tight">
                Cetacean
              </Link>

              <span className="hidden sm:block text-border">|</span>

              <ConnectionStatus />
            </div>

            <div className="flex items-center gap-3">
              <div className="hidden lg:flex items-center gap-1">
                <NavLinks />
              </div>

              <ThemeToggle />
              <GlobalSearch />

              <button
                className="lg:hidden p-2 rounded-md hover:bg-muted"
                onClick={() => setMenuOpen(!menuOpen)}
                aria-label="Toggle menu"
              >
                {menuOpen ? <X className="text-sm size-5" /> : <Menu className="text-sm size-5" />}
              </button>
            </div>
          </div>
          {menuOpen && (
            <div
              className="lg:hidden w-full flex flex-col gap-1 py-3"
              onClick={() => setMenuOpen(false)}
            >
              <NavLinks />
            </div>
          )}
        </div>
      </nav>
      <main className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8 py-6 pb-48">
        <ErrorBoundary>{children}</ErrorBoundary>
      </main>
    </div>
  );
}

function NavLinks() {
  const location = useLocation();
  const links = [
    { to: "/nodes", label: "Nodes" },
    { to: "/stacks", label: "Stacks" },
    { to: "/services", label: "Services" },
    { to: "/configs", label: "Configs" },
    { to: "/secrets", label: "Secrets" },
    { to: "/networks", label: "Networks" },
    { to: "/volumes", label: "Volumes" },
    { to: "/swarm", label: "Swarm" },
    { to: "/topology", label: "Topology" },
  ];
  return (
    <>
      {links.map(({ label, to }) => {
        const active = location.pathname === to || location.pathname.startsWith(to + "/");
        return (
          <Link
            key={to}
            to={to}
            aria-current={active ? "page" : undefined}
            className="text-sm px-3 py-1.5 rounded-md transition-colors text-muted-foreground hover:text-foreground hover:bg-muted/50 aria-[current=page]:bg-muted aria-[current=page]:text-foreground aria-[current=page]:font-medium"
          >
            {label}
          </Link>
        );
      })}
    </>
  );
}

function ConnectionTracker({ children }: { children: React.ReactNode }) {
  const [connected, setConnected] = useState(true);
  const [lastEventAt, setLastEventAt] = useState<number | null>(null);
  const lastEventAtRef = useRef<number | null>(null);

  useEffect(() => {
    const es = new EventSource("/events");
    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);

    const touch = () => {
      const now = Date.now();
      lastEventAtRef.current = now;
      setLastEventAt(now);
    };

    for (const type of SSE_EVENT_TYPES) {
      es.addEventListener(type, touch);
    }
    es.addEventListener("batch", touch);
    return () => es.close();
  }, []);

  return <ConnectionProvider value={{ connected, lastEventAt }}>{children}</ConnectionProvider>;
}

export default function App() {
  return (
    <BrowserRouter>
      <ConnectionTracker>
        <Layout>
          <Routes>
            <Route path="/" element={<ClusterOverview />} />
            <Route path="/nodes" element={<NodeList />} />
            <Route path="/nodes/:id" element={<NodeDetail />} />
            <Route path="/stacks" element={<StackList />} />
            <Route path="/stacks/:name" element={<StackDetail />} />
            <Route path="/services" element={<ServiceList />} />
            <Route path="/services/:id" element={<ServiceDetail />} />
            <Route path="/tasks/:id" element={<TaskDetail />} />
            <Route path="/configs" element={<ConfigList />} />
            <Route path="/configs/:id" element={<ConfigDetail />} />
            <Route path="/secrets" element={<SecretList />} />
            <Route path="/secrets/:id" element={<SecretDetail />} />
            <Route path="/networks" element={<NetworkList />} />
            <Route path="/networks/:id" element={<NetworkDetail />} />
            <Route path="/volumes" element={<VolumeList />} />
            <Route path="/volumes/:name" element={<VolumeDetail />} />
            <Route path="/swarm" element={<SwarmPage />} />
            <Route path="/topology" element={<Topology />} />
            <Route path="/search" element={<SearchPage />} />
            <Route path="*" element={<NotFound />} />
          </Routes>
        </Layout>
      </ConnectionTracker>
    </BrowserRouter>
  );
}

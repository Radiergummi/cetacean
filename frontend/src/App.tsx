import { Menu, X } from "lucide-react";
import type React from "react";
import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react";
import { BrowserRouter, Link, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { api } from "./api/client";
import ConnectionStatus from "./components/ConnectionStatus";
import ErrorBoundary from "./components/ErrorBoundary";
import { GlobalSearch, type GlobalSearchHandle } from "./components/search";
import ShortcutsHelp from "./components/ShortcutsHelp";
import ShortcutTooltip from "./components/ShortcutTooltip";
import ThemeToggle from "./components/ThemeToggle";
import { AuthContext, useAuth } from "./hooks/useAuth";
import { useHotkeys } from "./hooks/useHotkeys";
import { ConnectionProvider, SSE_EVENT_TYPES } from "./hooks/useResourceStream";

const ClusterOverview = lazy(() => import("./pages/ClusterOverview"));
const ConfigDetail = lazy(() => import("./pages/ConfigDetail"));
const ConfigList = lazy(() => import("./pages/ConfigList"));
const NetworkDetail = lazy(() => import("./pages/NetworkDetail"));
const NetworkList = lazy(() => import("./pages/NetworkList"));
const NodeDetail = lazy(() => import("./pages/NodeDetail"));
const NodeList = lazy(() => import("./pages/NodeList"));
const NotFound = lazy(() => import("./pages/NotFound"));
const SearchPage = lazy(() => import("./pages/SearchPage"));
const SecretDetail = lazy(() => import("./pages/SecretDetail"));
const SecretList = lazy(() => import("./pages/SecretList"));
const ServiceDetail = lazy(() => import("./pages/ServiceDetail"));
const ServiceList = lazy(() => import("./pages/ServiceList"));
const StackDetail = lazy(() => import("./pages/StackDetail"));
const StackList = lazy(() => import("./pages/StackList"));
const SwarmPage = lazy(() => import("./pages/SwarmPage"));
const TaskDetail = lazy(() => import("./pages/TaskDetail"));
const TaskList = lazy(() => import("./pages/TaskList"));
const Topology = lazy(() => import("./pages/Topology"));
const VolumeDetail = lazy(() => import("./pages/VolumeDetail"));
const VolumeList = lazy(() => import("./pages/VolumeList"));

function Layout({ children }: { children: React.ReactNode }) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const navigate = useNavigate();
  const searchRef = useRef<GlobalSearchHandle>(null);

  useHotkeys({
    "?": useCallback(() => setShortcutsOpen((o) => !o), []),
    "/": useCallback(() => searchRef.current?.open(), []),
    Escape: useCallback(() => {
      if (shortcutsOpen) setShortcutsOpen(false);
      else navigate(-1);
    }, [shortcutsOpen, navigate]),
    "g h": useCallback(() => navigate("/"), [navigate]),
    "g n": useCallback(() => navigate("/nodes"), [navigate]),
    "g s": useCallback(() => navigate("/services"), [navigate]),
    "g k": useCallback(() => navigate("/stacks"), [navigate]),
    "g c": useCallback(() => navigate("/configs"), [navigate]),
    "g x": useCallback(() => navigate("/secrets"), [navigate]),
    "g w": useCallback(() => navigate("/networks"), [navigate]),
    "g v": useCallback(() => navigate("/volumes"), [navigate]),
    "g a": useCallback(() => navigate("/tasks"), [navigate]),
    "g i": useCallback(() => navigate("/swarm"), [navigate]),
    "g t": useCallback(() => navigate("/topology"), [navigate]),
  });

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
              <GlobalSearch ref={searchRef} />
              <UserBadge />

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
      {shortcutsOpen && <ShortcutsHelp onClose={() => setShortcutsOpen(false)} />}
    </div>
  );
}

function NavLinks() {
  const location = useLocation();
  const links = [
    { to: "/nodes", label: "Nodes", keys: ["g", "n"] },
    { to: "/stacks", label: "Stacks", keys: ["g", "k"] },
    { to: "/services", label: "Services", keys: ["g", "s"] },
    { to: "/tasks", label: "Tasks", keys: ["g", "a"] },
    { to: "/configs", label: "Configs", keys: ["g", "c"] },
    { to: "/secrets", label: "Secrets", keys: ["g", "x"] },
    { to: "/networks", label: "Networks", keys: ["g", "w"] },
    { to: "/volumes", label: "Volumes", keys: ["g", "v"] },
    { to: "/swarm", label: "Swarm", keys: ["g", "i"] },
    { to: "/topology", label: "Topology", keys: ["g", "t"] },
  ];
  return (
    <>
      {links.map(({ label, to, keys }) => {
        const active = location.pathname === to || location.pathname.startsWith(to + "/");
        return (
          <ShortcutTooltip key={to} keys={keys}>
            <Link
              to={to}
              aria-current={active ? "page" : undefined}
              className="text-sm px-3 py-1.5 rounded-md transition-colors text-muted-foreground hover:text-foreground hover:bg-muted/50 aria-[current=page]:bg-muted aria-[current=page]:text-foreground aria-[current=page]:font-medium"
            >
              {label}
            </Link>
          </ShortcutTooltip>
        );
      })}
    </>
  );
}

function UserBadge() {
  const { identity, loading } = useAuth();
  if (loading || !identity || identity.provider === "none") return null;
  return (
    <span className="hidden sm:block text-sm text-muted-foreground truncate max-w-32">
      {identity.displayName || identity.email || identity.subject}
    </span>
  );
}

function AuthProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<{ identity: import("./api/types").Identity | null; loading: boolean }>({
    identity: null,
    loading: true,
  });

  useEffect(() => {
    api
      .whoami()
      .then((identity) => setState({ identity, loading: false }))
      .catch(() => setState({ identity: null, loading: false }));
  }, []);

  return <AuthContext value={state}>{children}</AuthContext>;
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
      <AuthProvider>
        <ConnectionTracker>
          <Layout>
            <Suspense>
              <Routes>
                <Route path="/" element={<ClusterOverview />} />
                <Route path="/nodes" element={<NodeList />} />
                <Route path="/nodes/:id" element={<NodeDetail />} />
                <Route path="/stacks" element={<StackList />} />
                <Route path="/stacks/:name" element={<StackDetail />} />
                <Route path="/services" element={<ServiceList />} />
                <Route path="/services/:id" element={<ServiceDetail />} />
                <Route path="/tasks" element={<TaskList />} />
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
            </Suspense>
          </Layout>
        </ConnectionTracker>
      </AuthProvider>
    </BrowserRouter>
  );
}

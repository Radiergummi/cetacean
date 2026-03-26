import ConnectionStatus from "./components/ConnectionStatus";
import ErrorBoundary from "./components/ErrorBoundary";
import { LoadingDetail } from "./components/LoadingSkeleton";
import { GlobalSearch, type GlobalSearchHandle } from "./components/search";
import ShortcutsHelp from "./components/ShortcutsHelp";
import ShortcutTooltip from "./components/ShortcutTooltip";
import ThemeToggle from "./components/ThemeToggle";
import { Toaster } from "./components/ui/sonner";
import { AuthProvider } from "./hooks/AuthProvider";
import { useAuth } from "./hooks/useAuth";
import { useHotkeys } from "./hooks/useHotkeys";
import { OperationsLevelProvider } from "./hooks/useOperationsLevel";
import { ConnectionProvider, sseEventTypes } from "./hooks/useResourceStream";
import { Keyboard, Menu, X } from "lucide-react";
import type React from "react";
import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react";
import { BrowserRouter, Link, Route, Routes, useLocation, useNavigate } from "react-router-dom";

const ClusterOverview = lazy(() => import("./pages/ClusterOverview"));
const ErrorIndex = lazy(() => import("./pages/ErrorIndex"));
const ErrorCodeDetail = lazy(() => import("./pages/ErrorCodeDetail"));
const ConfigDetail = lazy(() => import("./pages/ConfigDetail"));
const ConfigList = lazy(() => import("./pages/ConfigList"));
const NetworkDetail = lazy(() => import("./pages/NetworkDetail"));
const NetworkList = lazy(() => import("./pages/NetworkList"));
const NodeDetail = lazy(() => import("./pages/NodeDetail"));
const NodeList = lazy(() => import("./pages/NodeList"));
const NotFound = lazy(() => import("./pages/NotFound"));
const PluginDetail = lazy(() => import("./pages/PluginDetail"));
const PluginList = lazy(() => import("./pages/PluginList"));
const SearchPage = lazy(() => import("./pages/SearchPage"));
const SecretDetail = lazy(() => import("./pages/SecretDetail"));
const SecretList = lazy(() => import("./pages/SecretList"));
const ServiceDetail = lazy(() => import("./pages/ServiceDetail"));
const ServiceList = lazy(() => import("./pages/ServiceList"));
const ServiceSubResource = lazy(() => import("./pages/ServiceSubResource"));
const StackDetail = lazy(() => import("./pages/StackDetail"));
const StackList = lazy(() => import("./pages/StackList"));
const SwarmPage = lazy(() => import("./pages/SwarmPage"));
const TaskDetail = lazy(() => import("./pages/TaskDetail"));
const TaskList = lazy(() => import("./pages/TaskList"));
const MetricsConsole = lazy(() => import("./pages/MetricsConsole"));
const Topology = lazy(() => import("./pages/Topology"));
const VolumeDetail = lazy(() => import("./pages/VolumeDetail"));
const VolumeList = lazy(() => import("./pages/VolumeList"));
const ProfilePage = lazy(() => import("./pages/ProfilePage"));

function Layout({ children }: { children: React.ReactNode }) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const navigate = useNavigate();
  const searchRef = useRef<GlobalSearchHandle>(null);

  useEffect(() => {
    const handler = () => setShortcutsOpen(true);
    window.addEventListener("cetacean:show-shortcuts", handler);
    return () => window.removeEventListener("cetacean:show-shortcuts", handler);
  }, []);

  useHotkeys({
    "?": useCallback(() => setShortcutsOpen((o) => !o), []),
    "/": useCallback(() => searchRef.current?.open(), []),
    Escape: useCallback(() => {
      if (shortcutsOpen) {
        setShortcutsOpen(false);
      } else {
        navigate(-1);
      }
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
    "g m": useCallback(() => navigate("/metrics"), [navigate]),
  });

  return (
    <div className="min-h-screen bg-background">
      <nav className="sticky top-0 z-30 border-b bg-background/80 backdrop-blur-sm">
        <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
          <div className="relative flex h-12 items-center justify-between">
            <div className="flex items-center gap-3">
              <Link
                to="/"
                className="text-base font-semibold tracking-tight"
              >
                Cetacean
              </Link>

              <span className="hidden text-border sm:block">|</span>

              <span className="hidden sm:block">
                <ConnectionStatus />
              </span>
            </div>

            <div className="flex items-center gap-3">
              <div className="lg:absolute lg:left-1/2 lg:-translate-x-1/2">
                <GlobalSearch ref={searchRef} />
              </div>
              <ShortcutTooltip keys={["?"]}>
                <button
                  className="inline-flex size-8 items-center justify-center rounded-md transition hover:bg-muted"
                  onClick={() => setShortcutsOpen(true)}
                  aria-label="Keyboard shortcuts"
                >
                  <Keyboard className="size-4" />
                </button>
              </ShortcutTooltip>
              <ThemeToggle />
              <UserBadge />

              <button
                className="inline-flex size-8 items-center justify-center rounded-md transition hover:bg-muted lg:hidden"
                onClick={() => setMenuOpen(!menuOpen)}
                aria-label="Toggle menu"
              >
                {menuOpen ? <X className="size-5 text-sm" /> : <Menu className="size-5 text-sm" />}
              </button>
            </div>
          </div>

          <div className="-mb-px hidden h-10 items-center justify-center gap-1 lg:flex">
            <NavLinks />
          </div>

          {menuOpen && (
            <div
              className="flex w-full flex-col gap-1 py-3 lg:hidden"
              onClick={() => setMenuOpen(false)}
            >
              <NavLinks />
            </div>
          )}
        </div>
      </nav>
      <main className="mx-auto max-w-7xl px-4 py-6 pb-48 sm:px-6 lg:px-8">
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
    { to: "/metrics", label: "Metrics", keys: ["g", "m"] },
  ];
  return (
    <>
      {links.map(({ label, to, keys }) => {
        const active = location.pathname === to || location.pathname.startsWith(to + "/");
        return (
          <ShortcutTooltip
            key={to}
            keys={keys}
          >
            <Link
              to={to}
              aria-current={active ? "page" : undefined}
              className="py-2.5 text-sm text-muted-foreground transition-colors hover:text-foreground aria-[current=page]:font-medium aria-[current=page]:text-foreground lg:border-b-2 lg:border-transparent lg:px-3 lg:aria-[current=page]:border-foreground"
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
  if (loading || !identity || identity.provider === "none") {
    return null;
  }
  return (
    <Link
      to="/profile"
      className="hidden max-w-32 truncate text-sm text-muted-foreground transition-colors hover:text-foreground sm:block"
    >
      {identity.displayName || identity.email || identity.subject}
    </Link>
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

    for (const type of sseEventTypes) {
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
        <OperationsLevelProvider>
          <ConnectionTracker>
            <Toaster
              theme="system"
              richColors
              position="bottom-right"
              toastOptions={{ duration: 8000 }}
            />
            <Layout>
              <Suspense fallback={<LoadingDetail />}>
                <Routes>
                  <Route
                    path="/"
                    element={<ClusterOverview />}
                  />
                  <Route
                    path="/nodes"
                    element={<NodeList />}
                  />
                  <Route
                    path="/nodes/:id"
                    element={<NodeDetail />}
                  />
                  <Route
                    path="/stacks"
                    element={<StackList />}
                  />
                  <Route
                    path="/stacks/:name"
                    element={<StackDetail />}
                  />
                  <Route
                    path="/services"
                    element={<ServiceList />}
                  />
                  <Route
                    path="/services/:id"
                    element={<ServiceDetail />}
                  />
                  <Route
                    path="/services/:id/:subResource"
                    element={<ServiceSubResource />}
                  />
                  <Route
                    path="/tasks"
                    element={<TaskList />}
                  />
                  <Route
                    path="/tasks/:id"
                    element={<TaskDetail />}
                  />
                  <Route
                    path="/configs"
                    element={<ConfigList />}
                  />
                  <Route
                    path="/configs/:id"
                    element={<ConfigDetail />}
                  />
                  <Route
                    path="/secrets"
                    element={<SecretList />}
                  />
                  <Route
                    path="/secrets/:id"
                    element={<SecretDetail />}
                  />
                  <Route
                    path="/networks"
                    element={<NetworkList />}
                  />
                  <Route
                    path="/networks/:id"
                    element={<NetworkDetail />}
                  />
                  <Route
                    path="/volumes"
                    element={<VolumeList />}
                  />
                  <Route
                    path="/volumes/:name"
                    element={<VolumeDetail />}
                  />
                  <Route
                    path="/plugins"
                    element={<PluginList />}
                  />
                  <Route
                    path="/plugins/:name"
                    element={<PluginDetail />}
                  />
                  <Route
                    path="/swarm"
                    element={<SwarmPage />}
                  />
                  <Route
                    path="/metrics"
                    element={<MetricsConsole />}
                  />
                  <Route
                    path="/topology"
                    element={<Topology />}
                  />
                  <Route
                    path="/search"
                    element={<SearchPage />}
                  />
                  <Route
                    path="/api/errors"
                    element={<ErrorIndex />}
                  />
                  <Route
                    path="/api/errors/:code"
                    element={<ErrorCodeDetail />}
                  />
                  <Route
                    path="/profile"
                    element={<ProfilePage />}
                  />
                  <Route
                    path="*"
                    element={<NotFound />}
                  />
                </Routes>
              </Suspense>
            </Layout>
          </ConnectionTracker>
        </OperationsLevelProvider>
      </AuthProvider>
    </BrowserRouter>
  );
}

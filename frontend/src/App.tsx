import type React from "react";
import {useState} from "react";
import {BrowserRouter, Link, Route, Routes, useLocation} from "react-router-dom";
import ConnectionStatus from "./components/ConnectionStatus";
import ErrorBoundary from "./components/ErrorBoundary";
import ThemeToggle from "./components/ThemeToggle";
import {SSEProvider} from "./hooks/SSEContext";
import ClusterOverview from "./pages/ClusterOverview";
import ConfigDetail from "./pages/ConfigDetail";
import ConfigList from "./pages/ConfigList";
import NetworkDetail from "./pages/NetworkDetail";
import NetworkList from "./pages/NetworkList";
import NodeDetail from "./pages/NodeDetail";
import NodeList from "./pages/NodeList";
import SecretDetail from "./pages/SecretDetail";
import SecretList from "./pages/SecretList";
import ServiceDetail from "./pages/ServiceDetail";
import ServiceList from "./pages/ServiceList";
import StackDetail from "./pages/StackDetail";
import StackList from "./pages/StackList";
import TaskDetail from "./pages/TaskDetail";
import Topology from "./pages/Topology";
import VolumeDetail from "./pages/VolumeDetail";
import VolumeList from "./pages/VolumeList";
import NotFound from "./pages/NotFound";

function Layout({children}: { children: React.ReactNode }) {
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
                            <ConnectionStatus/>
                        </div>
                        <div className="flex items-center gap-1">
                            <div className="hidden md:flex items-center gap-1">
                                <NavLinks/>
                            </div>
                            <ThemeToggle/>
                            <button
                                className="md:hidden p-2 rounded-md hover:bg-muted"
                                onClick={() => setMenuOpen(!menuOpen)}
                                aria-label="Toggle menu"
                            >
                                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                    {menuOpen ? (
                                        <path
                                            strokeLinecap="round"
                                            strokeLinejoin="round"
                                            strokeWidth={2}
                                            d="M6 18L18 6M6 6l12 12"
                                        />
                                    ) : (
                                        <path
                                            strokeLinecap="round"
                                            strokeLinejoin="round"
                                            strokeWidth={2}
                                            d="M4 6h16M4 12h16M4 18h16"
                                        />
                                    )}
                                </svg>
                            </button>
                        </div>
                    </div>
                    {menuOpen && (
                        <div className="md:hidden flex flex-col gap-1 pb-3" onClick={() => setMenuOpen(false)}>
                            <NavLinks/>
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
        {to: "/nodes", label: "Nodes"},
        {to: "/stacks", label: "Stacks"},
        {to: "/services", label: "Services"},
        {to: "/configs", label: "Configs"},
        {to: "/secrets", label: "Secrets"},
        {to: "/networks", label: "Networks"},
        {to: "/volumes", label: "Volumes"},
        {to: "/topology", label: "Topology"},
    ];
    return (
        <>
            {links.map(({label, to}) => {
                const active = location.pathname === to || location.pathname.startsWith(to + "/");
                return (
                    <Link
                        key={to}
                        to={to}
                        className={`text-sm px-3 py-1.5 rounded-md transition-colors ${active
                            ? "bg-muted text-foreground font-medium"
                            : "text-muted-foreground hover:text-foreground hover:bg-muted/50"}`}
                    >
                        {label}
                    </Link>
                );
            })}
        </>
    );
}

export default function App() {
    return (
        <BrowserRouter>
            <SSEProvider>
                <Layout>
                    <Routes>
                        <Route path="/" element={<ClusterOverview/>}/>
                        <Route path="/nodes" element={<NodeList/>}/>
                        <Route path="/nodes/:id" element={<NodeDetail/>}/>
                        <Route path="/stacks" element={<StackList/>}/>
                        <Route path="/stacks/:name" element={<StackDetail/>}/>
                        <Route path="/services" element={<ServiceList/>}/>
                        <Route path="/services/:id" element={<ServiceDetail/>}/>
                        <Route path="/tasks/:id" element={<TaskDetail/>}/>
                        <Route path="/configs" element={<ConfigList/>}/>
                        <Route path="/configs/:id" element={<ConfigDetail/>}/>
                        <Route path="/secrets" element={<SecretList/>}/>
                        <Route path="/secrets/:id" element={<SecretDetail/>}/>
                        <Route path="/networks" element={<NetworkList/>}/>
                        <Route path="/networks/:id" element={<NetworkDetail/>}/>
                        <Route path="/volumes" element={<VolumeList/>}/>
                        <Route path="/volumes/:name" element={<VolumeDetail/>}/>
                        <Route path="/topology" element={<Topology/>}/>
                        <Route path="*" element={<NotFound/>}/>
                    </Routes>
                </Layout>
            </SSEProvider>
        </BrowserRouter>
    );
}

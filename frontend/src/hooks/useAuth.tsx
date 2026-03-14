import type React from "react";
import { createContext, useContext, useEffect, useState } from "react";
import { api } from "@/api/client";
import type { Identity } from "@/api/types";

interface AuthState {
  identity: Identity | null;
  loading: boolean;
}

const AuthContext = createContext<AuthState>({ identity: null, loading: true });

export function useAuth() {
  return useContext(AuthContext);
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<AuthState>({ identity: null, loading: true });

  useEffect(() => {
    api
      .whoami()
      .then((identity) => setState({ identity, loading: false }))
      .catch(() => setState({ identity: null, loading: false }));
  }, []);

  return <AuthContext value={state}>{children}</AuthContext>;
}

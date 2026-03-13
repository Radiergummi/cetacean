import { createContext, useContext } from "react";
import type { Identity } from "@/api/types";

interface AuthState {
  identity: Identity | null;
  loading: boolean;
}

const AuthContext = createContext<AuthState>({ identity: null, loading: true });

export function useAuth() {
  return useContext(AuthContext);
}

export { AuthContext };

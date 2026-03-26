import type { Identity } from "@/api/types";
import { createContext, useContext } from "react";

export interface AuthState {
  identity: Identity | null;
  loading: boolean;
}

export const AuthContext = createContext<AuthState>({
  identity: null,
  loading: true,
});

export function useAuth() {
  return useContext(AuthContext);
}

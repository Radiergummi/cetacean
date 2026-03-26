import { api } from "@/api/client";
import { AuthContext, type AuthState } from "./useAuth";
import type React from "react";
import { useEffect, useState } from "react";

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<AuthState>({
    identity: null,
    loading: true,
  });

  useEffect(() => {
    api
      .whoami()
      .then((identity) =>
        setState({
          identity,
          loading: false,
        }),
      )
      .catch(() =>
        setState({
          identity: null,
          loading: false,
        }),
      );
  }, []);

  return <AuthContext value={state}>{children}</AuthContext>;
}

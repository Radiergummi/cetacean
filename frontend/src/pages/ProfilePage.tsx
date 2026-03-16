import { LogOut } from "lucide-react";
import { Navigate } from "react-router-dom";
import { useAuth } from "@/hooks/useAuth";
import InfoCard from "@/components/InfoCard";
import PageHeader from "@/components/PageHeader";
import { LoadingDetail } from "@/components/LoadingSkeleton";
import { Button } from "@/components/ui/button";

export default function ProfilePage() {
  const { identity, loading } = useAuth();

  if (loading) return <LoadingDetail />;
  if (!identity || identity.provider === "none") return <Navigate to="/" replace />;

  const providerLabels: Record<string, string> = {
    oidc: "OpenID Connect",
    cert: "Client Certificate",
    headers: "Trusted Proxy Headers",
    tailscale: "Tailscale",
  };

  return (
    <>
      <PageHeader
        title={identity.displayName || identity.subject}
        subtitle={`Authenticated via ${providerLabels[identity.provider] ?? identity.provider}`}
        actions={
          identity.provider === "oidc" ? (
            <form method="POST" action="/auth/logout">
              <Button variant="outline" size="sm" type="submit">
                <LogOut className="size-4 mr-1.5" />
                Sign out
              </Button>
            </form>
          ) : undefined
        }
      />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <InfoCard label="Subject" value={identity.subject} />
        {identity.email && <InfoCard label="Email" value={identity.email} />}
        {identity.groups && identity.groups.length > 0 && (
          <InfoCard label="Groups" value={identity.groups.join(", ")} />
        )}
        <InfoCard label="Provider" value={providerLabels[identity.provider] ?? identity.provider} />
        {typeof identity.raw?.issuer_cn === "string" && (
          <InfoCard label="Issuer" value={identity.raw.issuer_cn} />
        )}
        {typeof identity.raw?.not_after === "string" && (
          <InfoCard label="Certificate Expires" value={identity.raw.not_after} />
        )}
        {typeof identity.raw?.spiffe_id === "string" && (
          <InfoCard label="SPIFFE ID" value={identity.raw.spiffe_id} />
        )}
      </div>
    </>
  );
}

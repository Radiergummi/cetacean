import FetchError from "../components/FetchError";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { api } from "@/api/client";
import { getErrorMessage } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";

export default function ErrorCodeDetail() {
  const { code } = useParams<{ code: string }>();
  const { data: errorDef, error } = useQuery({
    queryKey: ["error-definition", code],
    queryFn: () => api.errorDefinition(code ?? ""),
    staleTime: Infinity,
  });

  if (error) {
    return <FetchError message={getErrorMessage(error, "Failed to load error code")} />;
  }

  if (!errorDef) {
    return <LoadingDetail />;
  }

  return (
    <>
      <PageHeader
        title={`${errorDef.code} — ${errorDef.title}`}
        breadcrumbs={[{ label: "Error Reference", to: "/api/errors" }, { label: errorDef.code }]}
      />

      <div className="space-y-6">
        <dl className="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-2 text-sm">
          <dt className="font-medium text-muted-foreground">HTTP Status</dt>
          <dd>{errorDef.status}</dd>

          <dt className="font-medium text-muted-foreground">Code</dt>
          <dd className="font-mono">{errorDef.code}</dd>
        </dl>

        <p className="text-sm">{errorDef.description}</p>

        <div className="rounded-md border-s-2 border-status-info bg-status-info/10 p-4">
          <p className="text-sm">
            <span className="font-medium">Suggestion: </span>
            {errorDef.suggestion}
          </p>
        </div>
      </div>
    </>
  );
}

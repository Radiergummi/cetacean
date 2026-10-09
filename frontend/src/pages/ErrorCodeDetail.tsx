import FetchError from "../components/FetchError";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { api, type ErrorDefinition } from "@/api/client";
import { getErrorMessage } from "@/lib/utils";
import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";

type ErrorDef = ErrorDefinition;

export default function ErrorCodeDetail() {
  const { code } = useParams<{ code: string }>();
  const [errorDef, setErrorDef] = useState<ErrorDef | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .errorDefinition(code ?? "")
      .then(setErrorDef)
      .catch((caught: unknown) => setError(getErrorMessage(caught, "Failed to load error code")));
  }, [code]);

  if (error) {
    return <FetchError message={error} />;
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

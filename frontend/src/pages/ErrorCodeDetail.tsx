import FetchError from "../components/FetchError";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";

interface ErrorDef {
  code: string;
  title: string;
  status: number;
  description: string;
  suggestion: string;
}

export default function ErrorCodeDetail() {
  const { code } = useParams<{ code: string }>();
  const [errorDef, setErrorDef] = useState<ErrorDef | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch(`/api/errors/${code}`, { headers: { Accept: "application/json" } })
      .then((res) => {
        if (!res.ok) {
          throw new Error(`${res.status} ${res.statusText}`);
        }
        return res.json();
      })
      .then(setErrorDef)
      .catch((err) => setError(err.message));
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

        <div className="rounded-md border-l-2 border-blue-500 bg-blue-50 p-4 dark:bg-blue-950/30">
          <p className="text-sm">
            <span className="font-medium">Suggestion: </span>
            {errorDef.suggestion}
          </p>
        </div>
      </div>
    </>
  );
}

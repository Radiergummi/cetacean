import { ApiError, api } from "@/api/client";
import FetchError from "@/components/FetchError";
import { LoadingDetail } from "@/components/LoadingSkeleton";
import PageHeader from "@/components/PageHeader";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { encodeBase64Text } from "@/lib/base64";
import { useMutation, useQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";

/**
 * What a failed read or create tells the person, by the problem code.
 */
function describeError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === "SEC007") {
      return "This request was opened by someone else's agent, so it can't be completed from your session.";
    }

    if (error.code === "SEC006") {
      return "This request has expired or doesn't exist. Ask your agent to create the secret again.";
    }

    return error.message;
  }

  return error instanceof Error ? error.message : "Something went wrong.";
}

/**
 * Where a person enters the value of a secret an agent asked for over MCP. The
 * name and labels come from the server's record of the request, never the URL,
 * and the value goes from this page to the server without passing the agent.
 */
export default function SecretEntryPage() {
  const [searchParams] = useSearchParams();
  const elicitation = searchParams.get("elicitation") ?? "";
  const [value, setValue] = useState("");

  const entry = useQuery({
    queryKey: ["secret-entry", elicitation],
    queryFn: ({ signal }) => api.secretEntry(elicitation, signal),
    enabled: elicitation !== "",
    retry: false,
  });

  const create = useMutation({
    mutationFn: (data: string) => api.completeSecretEntry(elicitation, data),
  });

  if (elicitation === "") {
    return (
      <>
        <PageHeader title="New secret" />
        <FetchError message="Open this page from the link your agent gave you." />
      </>
    );
  }

  if (entry.isPending) {
    return <LoadingDetail />;
  }

  if (entry.isError) {
    return (
      <>
        <PageHeader title="New secret" />
        <FetchError message={describeError(entry.error)} />
      </>
    );
  }

  const { encoding, labels, name } = entry.data;
  const labelEntries = Object.entries(labels);

  if (create.isSuccess) {
    return (
      <>
        <PageHeader title={`Secret ${name} created`} />
        <p className="text-sm text-muted-foreground">
          Your agent can carry on now. The value went to Cetacean, not to the agent.{" "}
          <Link
            className="underline"
            to={`/secrets/${create.data.secret.ID}`}
          >
            View the secret
          </Link>
        </p>
      </>
    );
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault();

    if (value === "") {
      return;
    }

    create.mutate(encoding === "base64" ? value.trim() : encodeBase64Text(value));
  }

  return (
    <>
      <PageHeader
        title={`New secret ${name}`}
        subtitle="Your agent asked you to enter this secret's value. It goes from your browser to Cetacean, not through the agent."
      />

      <form
        className="flex max-w-xl flex-col gap-4"
        onSubmit={handleSubmit}
      >
        {labelEntries.length > 0 && (
          <div className="flex flex-col gap-1 text-sm">
            <span className="font-medium">Labels</span>
            <ul className="font-mono text-xs text-muted-foreground">
              {labelEntries.map(([key, labelValue]) => (
                <li key={key}>
                  {key}={labelValue}
                </li>
              ))}
            </ul>
          </div>
        )}

        <div className="flex flex-col gap-2">
          <Label htmlFor="secret-value">
            {encoding === "base64" ? "Value, base64-encoded" : "Value"}
          </Label>
          <textarea
            id="secret-value"
            value={value}
            onChange={(event) => setValue(event.target.value)}
            rows={6}
            autoComplete="off"
            spellCheck={false}
            className="w-full rounded-lg border border-input bg-transparent px-2.5 py-2 font-mono text-sm ring-offset-background focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
          />
        </div>

        {create.isError && <FetchError message={describeError(create.error)} />}

        <div>
          <Button
            type="submit"
            disabled={value === "" || create.isPending}
          >
            Create secret
          </Button>
        </div>
      </form>
    </>
  );
}

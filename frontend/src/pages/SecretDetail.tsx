import { useParams } from "react-router-dom";
import { api } from "../api/client";
import ActivitySection from "../components/ActivitySection";
import { LabelSection, ResourceId, ResourceLink, Timestamp } from "../components/data";
import FetchError from "../components/FetchError";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ServiceRefList from "../components/ServiceRefList";
import { useDetailResource } from "../hooks/useDetailResource";
import { parseStackLabels } from "../lib/parseStackLabels";

export default function SecretDetail() {
  const { id } = useParams<{ id: string }>();
  const { data, history, error } = useDetailResource(id, api.secret, `/secrets/${id}`);

  if (error) return <FetchError message="Failed to load secret" />;
  if (!data) return <LoadingDetail />;

  const secret = data.secret;
  const services = data.services ?? [];
  const name = secret.Spec.Name || secret.ID;
  const { entries: labelEntries, stack } = parseStackLabels(secret.Spec.Labels);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={name}
        breadcrumbs={[{ label: "Secrets", to: "/secrets" }, { label: name }]}
      />

      <p className="text-sm text-muted-foreground">
        Metadata only. Secret values are never exposed.
      </p>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        <ResourceId label="ID" id={secret.ID} />
        <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
        <Timestamp label="Created" date={secret.CreatedAt} />
        <Timestamp label="Updated" date={secret.UpdatedAt} />
      </div>

      <LabelSection entries={labelEntries} />

      <ServiceRefList
        services={services}
        label="Used by Services"
        emptyMessage="No services using this secret."
      />

      <ActivitySection entries={history} />
    </div>
  );
}

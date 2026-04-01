import { api } from "../api/client";
import ActivitySection from "../components/ActivitySection";
import { MetadataGrid, ResourceId, ResourceLink, Timestamp } from "../components/data";
import FetchError from "../components/FetchError";
import { KeyValueEditor } from "../components/KeyValueEditor";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { RemoveResourceAction } from "../components/RemoveResourceAction";
import ResourceName from "../components/ResourceName";
import ServiceRefList from "../components/ServiceRefList";
import { useDetailResource } from "../hooks/useDetailResource";
import { isReservedLabelKey, validateLabelKey } from "../lib/labelValidation";
import { parseStackLabels } from "../lib/parseStackLabels";
import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";

export default function SecretDetail() {
  const { id } = useParams<{ id: string }>();
  const { data, history, error, retry, allowedMethods } = useDetailResource(
    id,
    api.secret,
    `/secrets/${id}`,
  );
  const [labels, setLabels] = useState<Record<string, string>>({});

  useEffect(() => {
    if (data?.secret) {
      setLabels(data.secret.Spec.Labels ?? {});
    }
  }, [data?.secret]);

  if (error) {
    return (
      <FetchError
        message={error.message || "Failed to load secret"}
        onRetry={retry}
      />
    );
  }

  if (!data) {
    return <LoadingDetail />;
  }

  const { secret } = data;
  const services = data.services ?? [];
  const name = secret.Spec.Name || secret.ID;
  const { stack } = parseStackLabels(secret.Spec.Labels);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={
          <ResourceName
            name={name}
            direction="column"
          />
        }
        breadcrumbs={[
          { label: "Secrets", to: "/secrets" },
          { label: <ResourceName name={name} /> },
        ]}
        actions={
          <RemoveResourceAction
            resourceType="secret"
            resourceName={name}
            listPath={stack ? `/stacks/${stack}` : "/secrets"}
            onRemove={() => api.removeSecret(secret.ID)}
            canDelete={allowedMethods.has("DELETE")}
            disabled={services.length > 0}
            disabledTitle="Cannot remove a secret that is in use by services"
          />
        }
      />

      <MetadataGrid>
        <ResourceId
          label="ID"
          id={secret.ID}
        />
        <ResourceLink
          label="Stack"
          name={stack}
          to={`/stacks/${stack}`}
        />
        <Timestamp
          label="Created"
          date={secret.CreatedAt}
        />
        <Timestamp
          label="Updated"
          date={secret.UpdatedAt}
        />
      </MetadataGrid>

      <KeyValueEditor
        title="Labels"
        entries={labels}
        defaultOpen={Object.keys(labels).length > 0}
        keyPlaceholder="com.example.my-label"
        valuePlaceholder="value"
        editDisabled={!allowedMethods.has("PATCH")}
        isKeyReadOnly={isReservedLabelKey}
        validateKey={validateLabelKey}
        onSave={async (ops) => {
          const updated = await api.patchSecretLabels(secret.ID, ops);
          setLabels(updated);

          return updated;
        }}
      />

      <ServiceRefList
        services={services}
        label="Used by Services"
        emptyMessage="No services using this secret."
      />

      <ActivitySection
        entries={history}
        hideType
      />
    </div>
  );
}

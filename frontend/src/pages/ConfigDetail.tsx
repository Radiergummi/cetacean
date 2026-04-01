import { api } from "../api/client";
import ActivitySection from "../components/ActivitySection";
import CodeBlock from "../components/CodeBlock";
import CollapsibleSection from "../components/CollapsibleSection";
import { MetadataGrid, ResourceId, ResourceLink, Timestamp } from "../components/data";
import FetchError from "../components/FetchError";
import { IconButton } from "../components/IconButton";
import { KeyValueEditor } from "../components/KeyValueEditor";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { RemoveResourceAction } from "../components/RemoveResourceAction";
import ResourceName from "../components/ResourceName";
import ServiceRefList from "../components/ServiceRefList";
import { useDetailResource } from "../hooks/useDetailResource";
import { isReservedLabelKey, validateLabelKey } from "../lib/labelValidation";
import { parseStackLabels } from "../lib/parseStackLabels";
import { Copy } from "lucide-react";
import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";

export default function ConfigDetail() {
  const { id } = useParams<{ id: string }>();
  const { data, history, error, retry, allowedMethods } = useDetailResource(id, api.config, `/configs/${id}`);
  const [labels, setLabels] = useState<Record<string, string>>({});

  useEffect(() => {
    if (data?.config) {
      setLabels(data.config.Spec.Labels ?? {});
    }
  }, [data?.config]);

  if (error) {
    return (
      <FetchError
        message={error.message || "Failed to load config"}
        onRetry={retry}
      />
    );
  }

  if (!data) {
    return <LoadingDetail />;
  }

  const { config } = data;
  const services = data.services ?? [];
  const name = config.Spec.Name || config.ID;
  const { stack } = parseStackLabels(config.Spec.Labels);
  let decoded: string | null = null;

  if (config.Spec.Data) {
    try {
      decoded = atob(config.Spec.Data);
    } catch {
      decoded = null;
    }
  }

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
          { label: "Configs", to: "/configs" },
          { label: <ResourceName name={name} /> },
        ]}
        actions={
          <RemoveResourceAction
            resourceType="config"
            resourceName={name}
            listPath={stack ? `/stacks/${stack}` : "/configs"}
            onRemove={() => api.removeConfig(config.ID)}
            canDelete={allowedMethods.has("DELETE")}
            disabled={services.length > 0}
            disabledTitle="Cannot remove a config that is in use by services"
          />
        }
      />

      <MetadataGrid>
        <ResourceId
          label="ID"
          id={config.ID}
        />
        <ResourceLink
          label="Stack"
          name={stack}
          to={`/stacks/${stack}`}
        />
        <Timestamp
          label="Created"
          date={config.CreatedAt}
        />
        <Timestamp
          label="Updated"
          date={config.UpdatedAt}
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
          const updated = await api.patchConfigLabels(config.ID, ops);
          setLabels(updated);

          return updated;
        }}
      />

      {decoded != null && (
        <CollapsibleSection
          title="Data"
          controls={
            <IconButton
              onClick={() => navigator.clipboard.writeText(decoded)}
              title="Copy"
              icon={<Copy className="size-3.5" />}
            />
          }
        >
          <CodeBlock code={decoded} />
        </CollapsibleSection>
      )}

      <ServiceRefList
        services={services}
        label="Used by Services"
        emptyMessage="No services using this config."
      />

      <ActivitySection
        entries={history}
        hideType
      />
    </div>
  );
}

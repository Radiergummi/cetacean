import { api } from "../api/client";
import ActivitySection from "../components/ActivitySection";
import CodeBlock from "../components/CodeBlock";
import CollapsibleSection from "../components/CollapsibleSection";
import {
  LabelSection,
  MetadataGrid,
  ResourceId,
  ResourceLink,
  Timestamp,
} from "../components/data";
import FetchError from "../components/FetchError";
import { IconButton } from "../components/IconButton";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ResourceName from "../components/ResourceName";
import ServiceRefList from "../components/ServiceRefList";
import { useDetailResource } from "../hooks/useDetailResource";
import { parseStackLabels } from "../lib/parseStackLabels";
import { Copy } from "lucide-react";
import { useParams } from "react-router-dom";

export default function ConfigDetail() {
  const { id } = useParams<{ id: string }>();
  const { data, history, error, retry } = useDetailResource(id, api.config, `/configs/${id}`);

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

  const config = data.config;
  const services = data.services ?? [];
  const name = config.Spec.Name || config.ID;
  const { entries: labelEntries, stack } = parseStackLabels(config.Spec.Labels);
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

      <LabelSection entries={labelEntries} />

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

      <ActivitySection entries={history} hideType />
    </div>
  );
}

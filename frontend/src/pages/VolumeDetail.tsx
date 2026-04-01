import { api } from "../api/client";
import ActivitySection from "../components/ActivitySection";
import CollapsibleSection from "../components/CollapsibleSection";
import {
  KeyValuePills,
  LabelSection,
  MetadataGrid,
  ResourceLink,
  Timestamp,
} from "../components/data";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { RemoveResourceAction } from "../components/RemoveResourceAction";
import ResourceName from "../components/ResourceName";
import ServiceRefList from "../components/ServiceRefList";
import { useDetailResource } from "../hooks/useDetailResource";
import { parseStackLabels } from "../lib/parseStackLabels";
import { useParams } from "react-router-dom";

export default function VolumeDetail() {
  const { name } = useParams<{ name: string }>();
  const { data, history, error, retry, allowedMethods } = useDetailResource(
    name,
    api.volume,
    `/volumes/${name}`,
  );

  if (error) {
    return (
      <FetchError
        message={error.message || "Failed to load volume"}
        onRetry={retry}
      />
    );
  }

  if (!data) {
    return <LoadingDetail />;
  }

  const volume = data.volume;
  const services = data.services ?? [];
  const { entries: labelEntries, stack } = parseStackLabels(volume.Labels);
  const options = Object.entries(volume.Options ?? {});

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={
          <ResourceName
            name={volume.Name}
            direction="column"
          />
        }
        breadcrumbs={[
          { label: "Volumes", to: "/volumes" },
          { label: <ResourceName name={volume.Name} /> },
        ]}
        actions={
          <RemoveResourceAction
            resourceType="volume"
            resourceName={volume.Name}
            listPath={stack ? `/stacks/${stack}` : "/volumes"}
            onRemove={() => api.removeVolume(volume.Name)}
            onForceRemove={() => api.removeVolume(volume.Name, true)}
            canDelete={allowedMethods.has("DELETE")}
            disabled={services.length > 0}
            disabledTitle="Cannot remove a volume that is in use by services"
          />
        }
      />

      <MetadataGrid>
        <InfoCard
          label="Driver"
          value={volume.Driver}
        />
        <InfoCard
          label="Scope"
          value={volume.Scope}
        />
        <ResourceLink
          label="Stack"
          name={stack}
          to={`/stacks/${stack}`}
        />
        <Timestamp
          label="Created"
          date={volume.CreatedAt}
        />
        <InfoCard
          className="col-span-2"
          label="Mountpoint"
          value={<span className="truncate">{volume.Mountpoint}</span>}
        />
      </MetadataGrid>

      {options.length > 0 && (
        <CollapsibleSection title="Driver Options">
          <KeyValuePills entries={options} />
        </CollapsibleSection>
      )}

      <LabelSection entries={labelEntries} />

      <ServiceRefList
        services={services}
        label="Mounted by Services"
        emptyMessage="No services using this volume."
      />

      <ActivitySection
        entries={history}
        hideType
      />
    </div>
  );
}

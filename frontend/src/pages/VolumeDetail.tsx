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
import ServiceRefList from "../components/ServiceRefList";
import { useDetailResource } from "../hooks/useDetailResource";
import { parseStackLabels } from "../lib/parseStackLabels";
import { useParams } from "react-router-dom";

export default function VolumeDetail() {
  const { name } = useParams<{ name: string }>();
  const { data, history, error } = useDetailResource(name, api.volume, `/volumes/${name}`);

  if (error) return <FetchError message="Failed to load volume" />;
  if (!data) return <LoadingDetail />;

  const volume = data.volume;
  const services = data.services ?? [];
  const { entries: labelEntries, stack } = parseStackLabels(volume.Labels);
  const options = Object.entries(volume.Options || {});

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={volume.Name}
        breadcrumbs={[{ label: "Volumes", to: "/volumes" }, { label: volume.Name }]}
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
          value={volume.Mountpoint}
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

      <ActivitySection entries={history} />
    </div>
  );
}

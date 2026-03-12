import { useParams } from "react-router-dom";
import { api } from "../api/client";
import ActivitySection from "../components/ActivitySection";
import {
  KeyValuePills,
  LabelSection,
  ResourceLink,
  SectionHeader,
  Timestamp,
} from "../components/data";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ServiceRefList from "../components/ServiceRefList";
import { useDetailResource } from "../hooks/useDetailResource";
import { parseStackLabels } from "../lib/parseStackLabels";

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

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        <InfoCard label="Driver" value={volume.Driver} />
        <InfoCard label="Scope" value={volume.Scope} />
        <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
        <Timestamp label="Created" date={volume.CreatedAt} />
        <InfoCard className="col-span-2" label="Mountpoint" value={volume.Mountpoint} />
      </div>

      {options.length > 0 && (
        <div>
          <SectionHeader title="Driver Options" />
          <KeyValuePills entries={options} />
        </div>
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

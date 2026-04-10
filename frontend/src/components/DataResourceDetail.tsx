import type { PatchOp } from "../api/types";
import type { HistoryEntry, ServiceRef } from "../api/types";
import { isReservedLabelKey, validateLabelKey } from "../lib/labelValidation";
import { parseStackLabels } from "../lib/parseStackLabels";
import ActivitySection from "./ActivitySection";
import { MetadataGrid, ResourceId, ResourceLink, Timestamp } from "./data";
import { KeyValueEditor } from "./KeyValueEditor";
import PageHeader from "./PageHeader";
import { RemoveResourceAction } from "./RemoveResourceAction";
import ResourceName from "./ResourceName";
import ServiceRefList from "./ServiceRefList";
import { useEffect, useState, type ReactNode } from "react";

interface DataResourceDetailProps {
  resourceType: "config" | "secret";
  listLabel: string;
  listPath: string;
  id: string;
  name: string;
  labels: Record<string, string>;
  createdAt: string;
  updatedAt: string;
  services: ServiceRef[];
  history: HistoryEntry[];
  allowedMethods: Set<string>;
  onRemove: () => Promise<void>;
  onPatchLabels: (ops: PatchOp[]) => Promise<Record<string, string>>;
  children?: ReactNode;
}

/**
 * Shared detail page layout for configs and secrets.
 *
 * Renders metadata, labels editor, service cross-references, and activity.
 * Pass additional resource-specific sections (e.g. config data viewer) as children.
 */
export default function DataResourceDetail({
  resourceType,
  listLabel,
  listPath,
  id,
  name,
  labels: initialLabels,
  createdAt,
  updatedAt,
  services,
  history,
  allowedMethods,
  onRemove,
  onPatchLabels,
  children,
}: DataResourceDetailProps) {
  const [labels, setLabels] = useState<Record<string, string>>(initialLabels);
  const { stack } = parseStackLabels(initialLabels);

  useEffect(() => {
    setLabels(initialLabels);
  }, [initialLabels]);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={
          <ResourceName
            name={name}
            direction="column"
          />
        }
        breadcrumbs={[{ label: listLabel, to: listPath }, { label: <ResourceName name={name} /> }]}
        actions={
          <RemoveResourceAction
            resourceType={resourceType}
            resourceName={name}
            listPath={stack ? `/stacks/${stack}` : listPath}
            onRemove={onRemove}
            canDelete={allowedMethods.has("DELETE")}
            disabled={services.length > 0}
            disabledTitle={`Cannot remove a ${resourceType} that is in use by services`}
          />
        }
      />

      <MetadataGrid>
        <ResourceId
          label="ID"
          id={id}
        />
        <ResourceLink
          label="Stack"
          name={stack}
          to={`/stacks/${stack}`}
        />
        <Timestamp
          label="Created"
          date={createdAt}
        />
        <Timestamp
          label="Updated"
          date={updatedAt}
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
          const updated = await onPatchLabels(ops);
          setLabels(updated);

          return updated;
        }}
      />

      {children}

      <ServiceRefList
        services={services}
        label="Used by Services"
        emptyMessage={`No services using this ${resourceType}.`}
      />

      <ActivitySection
        entries={history}
        hideType
      />
    </div>
  );
}

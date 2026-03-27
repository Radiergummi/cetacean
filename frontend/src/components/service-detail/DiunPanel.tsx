import type { DiunIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import KeyValuePills from "@/components/data/KeyValuePills";
import { Input } from "@/components/ui/input";
import { NumberField } from "@/components/ui/number-field";
import { Switch } from "@/components/ui/switch";
import { badgeBlue, saveIntegrationLabels } from "@/lib/integrationLabels";
import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { IntegrationSection } from "./IntegrationSection";

const docsUrl = "https://crazymax.dev/diun/providers/swarm/#docker-labels";

const notifyOnLabels: Record<string, string> = {
  new: "New image",
  update: "Updated tag",
};

const sortTagsOptions = [
  "default",
  "reverse",
  "semver",
  "lexicographical",
] as const;

function NotifyOnBadges({ value }: { value: string }) {
  const triggers = value
    .split(";")
    .map((trigger) => trigger.trim())
    .filter(Boolean);

  return (
    <span className="inline-flex flex-wrap gap-1.5">
      {triggers.map((trigger) => (
        <span key={trigger} className={badgeBlue}>
          {notifyOnLabels[trigger] ?? trigger}
        </span>
      ))}
    </span>
  );
}

/**
 * Panel displaying parsed Diun image update notifier configuration,
 * with optional inline editing support.
 */
export function DiunPanel({
  integration,
  rawLabels,
  serviceId,
  onSaved,
  editable,
}: {
  integration: DiunIntegration;
  rawLabels: [string, string][];
  serviceId: string;
  onSaved: (updated: Record<string, string>) => void;
  editable?: boolean;
}) {
  const {
    enabled,
    watchRepo,
    notifyOn,
    maxTags,
    includeTags,
    excludeTags,
    sortTags,
    regopt,
    hubLink,
    platform,
    metadata,
  } = integration;

  const hasMetadata = metadata && Object.keys(metadata).length > 0;

  const initialTriggers = (integration.notifyOn ?? "new;update").split(";").map((trigger) => trigger.trim());

  const [formEnabled, setFormEnabled] = useState(integration.enabled);
  const [formRegopt, setFormRegopt] = useState(integration.regopt ?? "");
  const [formWatchRepo, setFormWatchRepo] = useState(integration.watchRepo ?? false);
  const [formNotifyNew, setFormNotifyNew] = useState(initialTriggers.includes("new"));
  const [formNotifyUpdate, setFormNotifyUpdate] = useState(initialTriggers.includes("update"));
  const [formSortTags, setFormSortTags] = useState(integration.sortTags ?? "");
  const [formMaxTags, setFormMaxTags] = useState(integration.maxTags ?? 0);
  const [formIncludeTags, setFormIncludeTags] = useState(integration.includeTags ?? "");
  const [formExcludeTags, setFormExcludeTags] = useState(integration.excludeTags ?? "");
  const [formHubLink, setFormHubLink] = useState(integration.hubLink ?? "");
  const [formPlatform, setFormPlatform] = useState(integration.platform ?? "");
  const [formMetadata, setFormMetadata] = useState<[string, string][]>(Object.entries(integration.metadata ?? {}));

  function resetForm() {
    setFormEnabled(integration.enabled);
    setFormRegopt(integration.regopt ?? "");
    setFormWatchRepo(integration.watchRepo ?? false);

    const triggers = (integration.notifyOn ?? "new;update")
      .split(";")
      .map((trigger) => trigger.trim());
    setFormNotifyNew(triggers.includes("new"));
    setFormNotifyUpdate(triggers.includes("update"));

    setFormSortTags(integration.sortTags ?? "");
    setFormMaxTags(integration.maxTags ?? 0);
    setFormIncludeTags(integration.includeTags ?? "");
    setFormExcludeTags(integration.excludeTags ?? "");
    setFormHubLink(integration.hubLink ?? "");
    setFormPlatform(integration.platform ?? "");
    setFormMetadata(Object.entries(integration.metadata ?? {}));
  }

  function serializeToLabels(): Record<string, string> {
    const labels: Record<string, string> = {
      "diun.enable": String(formEnabled),
    };

    if (formRegopt.trim()) {
      labels["diun.regopt"] = formRegopt;
    }

    if (formWatchRepo) {
      labels["diun.watch_repo"] = "true";
    }

    const notifyParts: string[] = [];

    if (formNotifyNew) {
      notifyParts.push("new");
    }

    if (formNotifyUpdate) {
      notifyParts.push("update");
    }

    if (notifyParts.length > 0) {
      labels["diun.notify_on"] = notifyParts.join(";");
    }

    if (formSortTags) {
      labels["diun.sort_tags"] = formSortTags;
    }

    if (formMaxTags > 0) {
      labels["diun.max_tags"] = String(formMaxTags);
    }

    if (formIncludeTags.trim()) {
      labels["diun.include_tags"] = formIncludeTags;
    }

    if (formExcludeTags.trim()) {
      labels["diun.exclude_tags"] = formExcludeTags;
    }

    if (formHubLink.trim()) {
      labels["diun.hub_link"] = formHubLink;
    }

    if (formPlatform.trim()) {
      labels["diun.platform"] = formPlatform;
    }

    for (const [key, value] of formMetadata) {
      if (key.trim()) {
        labels[`diun.metadata.${key}`] = value;
      }
    }

    return labels;
  }

  async function handleSave() {
    await saveIntegrationLabels(rawLabels, serializeToLabels(), serviceId, onSaved);
  }

  function addMetadataEntry() {
    setFormMetadata((previous) => [...previous, ["", ""]]);
  }

  function removeMetadataEntry(index: number) {
    setFormMetadata((previous) =>
      previous.filter((_, entryIndex) => entryIndex !== index),
    );
  }

  function updateMetadataEntry(index: number, field: 0 | 1, value: string) {
    setFormMetadata((previous) =>
      previous.map((entry, entryIndex) => {
        if (entryIndex !== index) {
          return entry;
        }

        const updated: [string, string] = [...entry];
        updated[field] = value;
        return updated;
      }),
    );
  }

  const editForm = (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-x-6 gap-y-3">
        <div className="flex flex-col gap-1.5">
          <label className="flex items-center gap-2">
            <Switch checked={formEnabled} onCheckedChange={setFormEnabled} />
            <span className="text-xs font-medium text-foreground">Enabled</span>
          </label>
          <p className="text-xs text-muted-foreground">
            Enable Diun image update monitoring
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <label className="flex items-center gap-2">
            <Switch
              checked={formWatchRepo}
              onCheckedChange={setFormWatchRepo}
            />
            <span className="text-xs font-medium text-foreground">
              Watch repo
            </span>
          </label>
          <p className="text-xs text-muted-foreground">
            Watch all tags, not just the deployed tag
          </p>
        </div>
      </div>

      <div className="grid items-start gap-x-4 gap-y-3 lg:grid-cols-3">
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-foreground">
            Sort tags
          </label>
          <select
            className="h-8 w-full rounded-md border bg-background px-2 text-sm"
            value={formSortTags}
            onChange={(event) => setFormSortTags(event.target.value)}
          >
            <option value="">—</option>
            {sortTagsOptions.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
          <p className="text-xs text-muted-foreground">
            How to sort tags when watch repo is enabled
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <NumberField
            label="Max tags"
            value={formMaxTags || undefined}
            onChange={(value) => setFormMaxTags(value ?? 0)}
            min={0}
          />
          <p className="text-xs text-muted-foreground">
            Maximum tags to watch (0 = unlimited)
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <span className="text-xs font-medium text-foreground">Notify on</span>
          <div className="flex items-center gap-3">
            <label className="flex items-center gap-1.5 text-xs">
              <Switch
                checked={formNotifyNew}
                onCheckedChange={setFormNotifyNew}
              />
              New image
            </label>
            <label className="flex items-center gap-1.5 text-xs">
              <Switch
                checked={formNotifyUpdate}
                onCheckedChange={setFormNotifyUpdate}
              />
              Updated tag
            </label>
          </div>
          <p className="text-xs text-muted-foreground">
            When to send notifications
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-foreground">
            Registry options
          </label>
          <Input
            value={formRegopt}
            onChange={(event) => setFormRegopt(event.target.value)}
            placeholder="my-registry"
          />
          <p className="text-xs text-muted-foreground">
            Registry options from Diun configuration
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-foreground">
            Hub link
          </label>
          <Input
            value={formHubLink}
            onChange={(event) => setFormHubLink(event.target.value)}
            placeholder="https://hub.example.com"
          />
          <p className="text-xs text-muted-foreground">
            Override the registry hub link
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-foreground">
            Platform
          </label>
          <Input
            value={formPlatform}
            onChange={(event) => setFormPlatform(event.target.value)}
            placeholder="linux/amd64"
          />
          <p className="text-xs text-muted-foreground">
            Platform for image analysis
          </p>
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <div className="flex items-center justify-between">
          <span className="text-xs font-medium text-foreground">Metadata</span>
          <button
            type="button"
            onClick={addMetadataEntry}
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <Plus className="size-3" />
            Add
          </button>
        </div>

        {formMetadata.map(([key, value], index) => (
          <div key={`${index}-${key}`} className="flex items-center gap-2">
            <Input
              className="flex-1"
              value={key}
              onChange={(event) =>
                updateMetadataEntry(index, 0, event.target.value)
              }
              placeholder="key"
            />
            <Input
              className="flex-1"
              value={value}
              onChange={(event) =>
                updateMetadataEntry(index, 1, event.target.value)
              }
              placeholder="value"
            />
            <button
              type="button"
              onClick={() => removeMetadataEntry(index)}
              className="text-muted-foreground hover:text-destructive"
            >
              <Trash2 className="size-3.5" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );

  return (
    <IntegrationSection
      title="Diun"
      defaultOpen={enabled}
      enabled={enabled}
      rawLabels={rawLabels}
      docsUrl={docsUrl}
      editable={editable}
      editContent={editForm}
      onEditStart={resetForm}
      onSave={handleSave}
      serviceId={serviceId}
      onRawSave={onSaved}
    >
      <div className="flex flex-col gap-3">
          <KVTable
            rows={[
              watchRepo && ["Watch repo", "Entire repository"],
              notifyOn && [
                "Notify on",
                <NotifyOnBadges key="notify-on" value={notifyOn} />,
              ],
              maxTags != null && maxTags > 0 && ["Max tags", String(maxTags)],
              includeTags && ["Include tags", includeTags],
              excludeTags && ["Exclude tags", excludeTags],
              sortTags && ["Sort tags", sortTags],
              regopt && ["Registry options", regopt],
              hubLink && ["Hub link", hubLink],
              platform && ["Platform", platform],
            ]}
          />

          {hasMetadata && (
            <div className="flex flex-col gap-1.5">
              <div className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Metadata
              </div>
              <KeyValuePills entries={Object.entries(metadata)} />
            </div>
          )}
        </div>
    </IntegrationSection>
  );
}

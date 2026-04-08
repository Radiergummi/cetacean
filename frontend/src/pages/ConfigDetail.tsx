import { api } from "../api/client";
import CodeBlock from "../components/CodeBlock";
import CollapsibleSection from "../components/CollapsibleSection";
import DataResourceDetail from "../components/DataResourceDetail";
import FetchError from "../components/FetchError";
import { IconButton } from "../components/IconButton";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { useDetailResource } from "../hooks/useDetailResource";
import { Copy } from "lucide-react";
import { useParams } from "react-router-dom";

export default function ConfigDetail() {
  const { id } = useParams<{ id: string }>();
  const { data, history, error, retry, allowedMethods } = useDetailResource(
    id,
    api.config,
    `/configs/${id}`,
  );

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
  let decoded: string | null = null;

  if (config.Spec.Data) {
    try {
      decoded = atob(config.Spec.Data);
    } catch {
      decoded = null;
    }
  }

  return (
    <DataResourceDetail
      resourceType="config"
      listLabel="Configs"
      listPath="/configs"
      id={config.ID}
      name={config.Spec.Name || config.ID}
      labels={config.Spec.Labels ?? {}}
      createdAt={config.CreatedAt}
      updatedAt={config.UpdatedAt}
      services={services}
      history={history}
      allowedMethods={allowedMethods}
      onRemove={() => api.removeConfig(config.ID)}
      onPatchLabels={(ops) => api.patchConfigLabels(config.ID, ops)}
    >
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
    </DataResourceDetail>
  );
}

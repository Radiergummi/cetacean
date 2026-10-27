import { api } from "../api/client";
import CodeBlock from "../components/CodeBlock";
import CollapsibleSection from "../components/CollapsibleSection";
import DataResourceDetail from "../components/DataResourceDetail";
import DetailError from "../components/DetailError";
import { IconButton } from "../components/IconButton";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { useDetailResource } from "../hooks/useDetailResource";
import { decodeBase64Text } from "@/lib/base64";
import { Copy } from "lucide-react";
import { useParams } from "react-router-dom";

export default function ConfigDetail() {
  const { id } = useParams<{ id: string }>();
  const { data, history, error, retry, allowedMethods, editable } = useDetailResource(
    id,
    api.config,
    "/configs",
  );

  if (error) {
    return (
      <DetailError
        error={error}
        fallback="Failed to load config"
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
      decoded = decodeBase64Text(config.Spec.Data);
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
      editable={editable}
      onRemove={() => api.removeConfig(config.ID)}
      onPatchLabels={(ops, ifMatch) => api.patchConfigLabels(config.ID, ops, ifMatch)}
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

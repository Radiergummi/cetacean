import { api } from "../api/client";
import DataResourceDetail from "../components/DataResourceDetail";
import DetailError from "../components/DetailError";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { useDetailResource } from "../hooks/useDetailResource";
import { useParams } from "react-router-dom";

export default function SecretDetail() {
  const { id } = useParams<{ id: string }>();
  const { data, history, error, retry, allowedMethods, editable } = useDetailResource(
    id,
    api.secret,
    "/secrets",
  );

  if (error) {
    return (
      <DetailError
        error={error}
        fallback="Failed to load secret"
        onRetry={retry}
      />
    );
  }

  if (!data) {
    return <LoadingDetail />;
  }

  const { secret } = data;
  const services = data.services ?? [];

  return (
    <DataResourceDetail
      resourceType="secret"
      listLabel="Secrets"
      listPath="/secrets"
      id={secret.ID}
      name={secret.Spec.Name || secret.ID}
      labels={secret.Spec.Labels ?? {}}
      createdAt={secret.CreatedAt}
      updatedAt={secret.UpdatedAt}
      services={services}
      history={history}
      allowedMethods={allowedMethods}
      editable={editable}
      onRemove={() => api.removeSecret(secret.ID)}
      onPatchLabels={(ops, ifMatch) => api.patchSecretLabels(secret.ID, ops, ifMatch)}
    />
  );
}

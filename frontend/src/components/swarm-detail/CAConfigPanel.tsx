import { api } from "../../api/client";
import type { SwarmInfo } from "../../api/types";
import { useAsyncAction } from "../../hooks/useAsyncAction";
import { formatDuration } from "../../lib/format";
import { KVTable } from "../data";
import { EditablePanel } from "../service-detail/EditablePanel";
import { Spinner } from "../Spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "../ui/alert-dialog";
import { Button } from "../ui/button";
import { DurationInput } from "../ui/duration-input";
import { RefreshCw } from "lucide-react";
import { useState } from "react";

type SwarmSpec = SwarmInfo["swarm"]["Spec"];

interface CAConfigPanelProps {
  spec: SwarmSpec;
  rootRotationInProgress: boolean;
  canEdit: boolean;
  onSaved: () => void;
}

export function CAConfigPanel({
  spec,
  rootRotationInProgress,
  canEdit,
  onSaved,
}: CAConfigPanelProps) {
  const [draftCertExpiry, setDraftCertExpiry] = useState(0);
  const forceRotateCA = useAsyncAction({ toast: true });

  return (
    <EditablePanel
      title="CA Configuration"
      canEdit={canEdit}
      headerActions={
        <AlertDialog>
          <AlertDialogTrigger
            render={
              <Button
                variant="outline"
                size="xs"
                disabled={forceRotateCA.loading}
              >
                {forceRotateCA.loading ? (
                  <Spinner className="size-3" />
                ) : (
                  <RefreshCw className="size-3" />
                )}
                Force Rotate
              </Button>
            }
          />
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Force CA certificate rotation?</AlertDialogTitle>
              <AlertDialogDescription>
                This will trigger an immediate rotation of all TLS certificates across the cluster.
                All nodes will need to re-issue their certificates.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction
                onClick={() =>
                  void forceRotateCA.execute(async () => {
                    await api.forceRotateCA();
                    onSaved();
                  }, "Failed to force CA rotation")
                }
              >
                Rotate
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      }
      display={
        <div className="space-y-2">
          <KVTable
            rows={[
              spec.CAConfig.NodeCertExpiry !== 0 && [
                "Node Certificate Expiry",
                formatDuration(spec.CAConfig.NodeCertExpiry),
              ],
              ["Force Rotate", String(spec.CAConfig.ForceRotate ?? 0)],
              ["Root Rotation In Progress", rootRotationInProgress ? "Yes" : "No"],
              ...(spec.CAConfig.ExternalCAs?.map(({ Protocol, URL }, index): [string, string] => [
                `External CA ${index + 1}`,
                `${Protocol} — ${URL}`,
              ]) ?? []),
            ]}
          />
        </div>
      }
      edit={
        <div className="space-y-3">
          <label className="block space-y-1">
            <span className="text-xs text-muted-foreground">Node Certificate Expiry</span>
            <DurationInput
              value={draftCertExpiry}
              onChange={setDraftCertExpiry}
            />
          </label>

          <KVTable
            rows={[
              ["Force Rotate", String(spec.CAConfig.ForceRotate ?? 0)],
              ["Root Rotation In Progress", rootRotationInProgress ? "Yes" : "No"],
              ...(spec.CAConfig.ExternalCAs?.map(({ Protocol, URL }, index): [string, string] => [
                `External CA ${index + 1}`,
                `${Protocol} — ${URL}`,
              ]) ?? []),
            ]}
          />
        </div>
      }
      onOpen={() => {
        setDraftCertExpiry(spec.CAConfig.NodeCertExpiry);
      }}
      onSave={async () => {
        await api.patchSwarmCAConfig({ NodeCertExpiry: draftCertExpiry });
        onSaved();
      }}
    />
  );
}

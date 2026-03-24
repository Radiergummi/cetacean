import { api } from "../api/client";
import type { PluginPrivilege } from "../api/types";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { Spinner } from "./Spinner";
import { Button } from "./ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "./ui/dialog";
import { useState } from "react";

function normalizeReference(ref: string): string {
  const trimmed = ref.trim();
  const namePart = trimmed.split(":")[0].split("@")[0];
  const hasRegistry = namePart.includes(".") || namePart.includes(":");

  if (hasRegistry) {
    return trimmed;
  }

  return `docker.io/${trimmed}`;
}

interface InstallPluginDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onInstalled: () => void;
  mode?: "install" | "upgrade";
  pluginName?: string;
  currentReference?: string;
}

export default function InstallPluginDialog({
  open,
  onOpenChange,
  onInstalled,
  mode = "install",
  pluginName,
  currentReference,
}: InstallPluginDialogProps) {
  const [remote, setRemote] = useState(currentReference ?? "");
  const [privileges, setPrivileges] = useState<PluginPrivilege[] | null>(null);
  const checkPrivileges = useAsyncAction({ toast: true });
  const installAction = useAsyncAction({ toast: true });

  const isUpgrade = mode === "upgrade";
  const actionLabel = isUpgrade ? "Upgrade" : "Install";

  function reset() {
    setRemote(currentReference ?? "");
    setPrivileges(null);
  }

  function handleOpenChange(next: boolean) {
    reset();
    onOpenChange(next);
  }

  function handleCheckPrivileges() {
    void checkPrivileges.execute(async () => {
      const normalized = normalizeReference(remote);
      const result = await api.pluginPrivileges(normalized);
      setRemote(normalized);
      setPrivileges(result);
    }, "Failed to fetch plugin privileges");
  }

  function handleInstall() {
    void installAction.execute(
      async () => {
        const normalized = normalizeReference(remote);

        if (isUpgrade && pluginName) {
          await api.upgradePlugin(pluginName, normalized);
        } else {
          await api.installPlugin(normalized);
        }

        onInstalled();
        handleOpenChange(false);
      },
      `Failed to ${isUpgrade ? "upgrade" : "install"} plugin`,
    );
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{actionLabel} Plugin</DialogTitle>
          <DialogDescription>
            {isUpgrade
              ? "Enter the new remote reference for the plugin upgrade."
              : "Enter the remote reference for the plugin to install."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <label className="block space-y-1">
            <span className="text-xs text-muted-foreground">Remote Reference</span>
            <input
              type="text"
              value={remote}
              onChange={(event) => setRemote(event.target.value)}
              placeholder="docker.io/library/plugin:latest"
              className="w-full rounded-md border bg-transparent px-3 py-2 text-sm transition outline-none focus:ring-2 focus:ring-ring"
              disabled={checkPrivileges.loading || installAction.loading}
            />
          </label>

          {privileges && (
            <div className="space-y-2">
              <span className="text-xs font-medium text-muted-foreground">Required Privileges</span>

              {privileges.length === 0 ? (
                <p className="text-xs text-muted-foreground">No special privileges required.</p>
              ) : (
                <div className="overflow-x-auto rounded-lg border">
                  <table className="w-full">
                    <thead>
                      <tr className="border-b text-left text-xs font-medium tracking-wider text-muted-foreground uppercase">
                        <th className="p-2">Name</th>
                        <th className="p-2">Description</th>
                        <th className="p-2">Values</th>
                      </tr>
                    </thead>
                    <tbody>
                      {privileges.map(({ Name, Description, Value }) => (
                        <tr
                          key={Name}
                          className="border-b last:border-b-0"
                        >
                          <td className="p-2 font-mono text-xs">{Name}</td>
                          <td className="p-2 text-xs text-muted-foreground">{Description}</td>
                          <td className="p-2 font-mono text-xs">{(Value ?? []).join(", ")}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </div>

        <DialogFooter>
          {!privileges ? (
            <Button
              onClick={handleCheckPrivileges}
              disabled={!remote.trim() || checkPrivileges.loading}
            >
              {checkPrivileges.loading && <Spinner className="size-3" />}
              Check Privileges
            </Button>
          ) : (
            <Button
              onClick={handleInstall}
              disabled={installAction.loading}
            >
              {installAction.loading && <Spinner className="size-3" />}
              {actionLabel}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

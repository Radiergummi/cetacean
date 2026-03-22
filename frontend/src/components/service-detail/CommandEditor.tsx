import { EditablePanel } from "./EditablePanel";
import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { DescriptionRow } from "@/components/data";
import { Input } from "@/components/ui/input";
import { useState } from "react";

export function CommandEditor({
  serviceId,
  config,
  onSaved,
}: {
  serviceId: string;
  config: ContainerConfig;
  onSaved: (updated: ContainerConfig) => void;
}) {
  const [commandInput, setCommandInput] = useState("");
  const [argsInput, setArgsInput] = useState("");
  const [dirInput, setDirInput] = useState("");
  const [userInput, setUserInput] = useState("");

  function resetForm() {
    setCommandInput(config.command?.join(" ") ?? "");
    setArgsInput(config.args?.join(" ") ?? "");
    setDirInput(config.dir);
    setUserInput(config.user);
  }

  async function save() {
    const cmd = commandInput.trim() ? commandInput.trim().split(/\s+/) : null;
    const argsList = argsInput.trim() ? argsInput.trim().split(/\s+/) : null;
    const updated = await api.patchServiceContainerConfig(serviceId, {
      command: cmd,
      args: argsList,
      dir: dirInput,
      user: userInput,
    });
    onSaved(updated);
  }

  return (
    <EditablePanel
      title="Command"
      onOpen={resetForm}
      onSave={save}
      display={
        <dl className="grid gap-y-2 text-sm">
          <DescriptionRow
            label="Command"
            value={config.command?.join(" ")}
            mono
          />
          <DescriptionRow
            label="Args"
            value={config.args?.join(" ")}
            mono
          />
          <DescriptionRow
            label="Working Dir"
            value={config.dir}
          />
          <DescriptionRow
            label="User"
            value={config.user}
          />
        </dl>
      }
      edit={
        <>
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Command</label>
            <Input
              value={commandInput}
              onChange={(event) => setCommandInput(event.target.value)}
              placeholder="/bin/my-entrypoint"
              className="font-mono"
            />
            <p className="text-xs text-muted-foreground">Space-separated list of tokens</p>
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Args</label>
            <Input
              value={argsInput}
              onChange={(event) => setArgsInput(event.target.value)}
              placeholder="--flag value"
              className="font-mono"
            />
            <p className="text-xs text-muted-foreground">Space-separated list of tokens</p>
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Working Dir</label>
            <Input
              value={dirInput}
              onChange={(event) => setDirInput(event.target.value)}
              placeholder="/app"
              className="font-mono"
            />
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">User</label>
            <Input
              value={userInput}
              onChange={(event) => setUserInput(event.target.value)}
              placeholder="nobody"
              className="font-mono"
            />
          </div>
        </>
      }
    />
  );
}

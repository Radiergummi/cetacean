import { DockerDocsLink } from "./DockerDocsLink";
import { EditablePanel } from "./EditablePanel";
import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { DescriptionRow } from "@/components/data";
import { Combobox } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { formatDuration } from "@/lib/format";
import { renderSwarmTemplate } from "@/lib/swarmTemplates";
import { useCallback, useState } from "react";

function formatInit(init: boolean | undefined): string {
  if (init === undefined) {
    return "Default";
  }

  return init ? "Yes" : "No";
}

const signalOptions = [
  { value: "SIGTERM", label: "SIGTERM", description: "Graceful termination (default)" },
  { value: "SIGKILL", label: "SIGKILL", description: "Immediate kill, cannot be caught" },
  { value: "SIGINT", label: "SIGINT", description: "Interrupt (Ctrl+C)" },
  { value: "SIGQUIT", label: "SIGQUIT", description: "Quit with core dump" },
  { value: "SIGHUP", label: "SIGHUP", description: "Hangup, often used for config reload" },
  { value: "SIGUSR1", label: "SIGUSR1", description: "User-defined signal 1" },
  { value: "SIGUSR2", label: "SIGUSR2", description: "User-defined signal 2" },
  { value: "SIGABRT", label: "SIGABRT", description: "Abort" },
  { value: "SIGALRM", label: "SIGALRM", description: "Timer alarm" },
  { value: "SIGBUS", label: "SIGBUS", description: "Bus error" },
  { value: "SIGCHLD", label: "SIGCHLD", description: "Child process status change" },
  { value: "SIGCONT", label: "SIGCONT", description: "Continue if stopped" },
  { value: "SIGFPE", label: "SIGFPE", description: "Floating-point exception" },
  { value: "SIGILL", label: "SIGILL", description: "Illegal instruction" },
  { value: "SIGIO", label: "SIGIO", description: "I/O possible" },
  { value: "SIGPIPE", label: "SIGPIPE", description: "Broken pipe" },
  { value: "SIGPROF", label: "SIGPROF", description: "Profiling timer expired" },
  { value: "SIGSEGV", label: "SIGSEGV", description: "Segmentation fault" },
  { value: "SIGSTOP", label: "SIGSTOP", description: "Stop process, cannot be caught" },
  { value: "SIGSYS", label: "SIGSYS", description: "Bad system call" },
  { value: "SIGTRAP", label: "SIGTRAP", description: "Trace/breakpoint trap" },
  { value: "SIGTSTP", label: "SIGTSTP", description: "Stop from terminal (Ctrl+Z)" },
  { value: "SIGTTIN", label: "SIGTTIN", description: "Background process read from terminal" },
  { value: "SIGTTOU", label: "SIGTTOU", description: "Background process write to terminal" },
  { value: "SIGURG", label: "SIGURG", description: "Urgent socket condition" },
  { value: "SIGVTALRM", label: "SIGVTALRM", description: "Virtual timer expired" },
  { value: "SIGWINCH", label: "SIGWINCH", description: "Window size change" },
  { value: "SIGXCPU", label: "SIGXCPU", description: "CPU time limit exceeded" },
  { value: "SIGXFSZ", label: "SIGXFSZ", description: "File size limit exceeded" },
];

export function RuntimeEditor({
  serviceId,
  config,
  onSaved,
}: {
  serviceId: string;
  config: ContainerConfig;
  onSaved: (updated: ContainerConfig) => void;
}) {
  const [hostnameInput, setHostnameInput] = useState("");
  const [initValue, setInitValue] = useState<boolean | undefined>(undefined);
  const [ttyInput, setTtyInput] = useState(false);
  const [readOnlyInput, setReadOnlyInput] = useState(false);
  const [stopSignalInput, setStopSignalInput] = useState("");
  const [gracePeriodInput, setGracePeriodInput] = useState("");

  function resetForm() {
    setHostnameInput(config.hostname);
    setInitValue(config.init);
    setTtyInput(config.tty);
    setReadOnlyInput(config.readOnly);
    setStopSignalInput(config.stopSignal);
    setGracePeriodInput(config.stopGracePeriod != null ? String(config.stopGracePeriod / 1e9) : "");
  }

  async function save() {
    const patch: Record<string, unknown> = {
      hostname: hostnameInput,
      tty: ttyInput,
      readOnly: readOnlyInput,
      stopSignal: stopSignalInput,
    };

    if (initValue === undefined) {
      patch.init = null;
    } else {
      patch.init = initValue;
    }

    if (gracePeriodInput !== "") {
      patch.stopGracePeriod = parseFloat(gracePeriodInput) * 1e9;
    } else {
      patch.stopGracePeriod = null;
    }

    const updated = await api.patchServiceContainerConfig(serviceId, patch);
    onSaved(updated);
  }

  return (
    <EditablePanel
      title="Runtime"
      onOpen={resetForm}
      onSave={save}
      display={
        <dl className="grid gap-y-2 text-sm">
          <div className="grid grid-cols-[8rem_1fr] items-baseline gap-x-2">
            <dt className="text-muted-foreground">Hostname</dt>
            <dd className="font-mono">
              {config.hostname ? renderSwarmTemplate(config.hostname) : "—"}
            </dd>
          </div>
          <DescriptionRow
            label="Init"
            value={formatInit(config.init)}
          />
          <DescriptionRow
            label="TTY"
            value={config.tty ? "Yes" : "No"}
          />
          <DescriptionRow
            label="Read Only"
            value={config.readOnly ? "Yes" : "No"}
          />
          <DescriptionRow
            label="Stop Signal"
            value={config.stopSignal || undefined}
          />
          <DescriptionRow
            label="Stop Grace Period"
            value={
              config.stopGracePeriod != null ? formatDuration(config.stopGracePeriod) : undefined
            }
          />
        </dl>
      }
      edit={
        <>
          <div className="flex flex-col gap-1">
            <label className="flex items-center gap-1 text-xs text-foreground">
              Hostname <DockerDocsLink anchor="hostname" />
            </label>
            <Input
              value={hostnameInput}
              onChange={(event) => setHostnameInput(event.target.value)}
              placeholder="{{.Node.Hostname}}-{{.Task.Slot}}"
              className="font-mono"
            />
            <p className="text-xs text-muted-foreground">
              Supports swarm templates: {"{{.Node.Hostname}}"}, {"{{.Task.Slot}}"}, etc.
            </p>
          </div>

          <div className="flex flex-col gap-1">
            <label className="flex items-center gap-1 text-xs text-foreground">
              Stop Signal <DockerDocsLink anchor="stop-signal" />
            </label>
            <Combobox
              value={stopSignalInput}
              onChange={setStopSignalInput}
              placeholder="Select signal..."
              options={signalOptions}
              allowCustom={false}
            />
          </div>

          <div className="flex flex-col gap-1">
            <label className="flex items-center gap-1 text-xs text-foreground">
              Stop Grace Period (seconds) <DockerDocsLink anchor="stop-grace-period" />
            </label>
            <Input
              type="number"
              value={gracePeriodInput}
              onChange={(event) => setGracePeriodInput(event.target.value)}
              placeholder="10"
              min={0}
              step={0.1}
              className="font-mono"
            />

            <p className="text-xs text-muted-foreground">
              Time to wait before force killing a container
            </p>
          </div>

          <div className="flex flex-col gap-3">
            <label className="flex gap-2 text-sm">
              <input
                type="checkbox"
                ref={useCallback(
                  (element: HTMLInputElement | null) => {
                    if (element) {
                      element.indeterminate = initValue === undefined;
                    }
                  },
                  [initValue],
                )}
                checked={initValue === true}
                onChange={() => {
                  if (initValue === undefined) {
                    setInitValue(true);
                  } else if (initValue === true) {
                    setInitValue(false);
                  } else {
                    setInitValue(undefined);
                  }
                }}
                className="mt-0.5 size-4"
              />
              <span>
                <span className="inline-flex items-center gap-1">
                  Init <DockerDocsLink anchor="init" />
                </span>
                {initValue === undefined && (
                  <span className="text-xs text-muted-foreground"> (default)</span>
                )}
                <p className="text-xs text-muted-foreground">
                  Run an init process inside the container to forward signals and reap zombie
                  processes.
                </p>
              </span>
            </label>

            <label className="flex gap-2 text-sm">
              <input
                type="checkbox"
                checked={ttyInput}
                onChange={(event) => setTtyInput(event.target.checked)}
                className="mt-0.5 size-4"
              />
              <span>
                <span className="inline-flex items-center gap-1">
                  TTY <DockerDocsLink anchor="tty" />
                </span>
                <p className="text-xs text-muted-foreground">
                  Allocate a pseudo-TTY for the container. Required for interactive or TUI-based
                  applications.
                </p>
              </span>
            </label>

            <label className="flex gap-2 text-sm">
              <input
                type="checkbox"
                checked={readOnlyInput}
                onChange={(event) => setReadOnlyInput(event.target.checked)}
                className="mt-0.5 size-4"
              />
              <span>
                <span className="inline-flex items-center gap-1">
                  Read Only <DockerDocsLink anchor="read-only" />
                </span>
                <p className="text-xs text-muted-foreground">
                  Mount the container's root filesystem as read-only. Use volumes or tmpfs for
                  writable paths.
                </p>
              </span>
            </label>
          </div>
        </>
      }
    />
  );
}

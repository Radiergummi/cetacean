import { useCetaceanHost } from "../bridge";
import { LogTail } from "./LogTail";
import { type LogTailArguments, useLogTail } from "./useLogTail";

/**
 * Reads the arguments the host called `get_logs` with.
 *
 * A widget renders for one tool call, and the host hands it that call's
 * arguments — which is the only way this widget can know which service it is
 * tailing. Anything but a named service is nothing to show, so an argument set
 * without one yields no read at all rather than a guess.
 */
function logArgumentsFrom(
  input: Record<string, unknown> | undefined,
): LogTailArguments | undefined {
  const service = input?.service;

  if (typeof service !== "string" || service === "") {
    return undefined;
  }

  const tail = input?.tail;
  const level = input?.level;

  return {
    service,
    tail: typeof tail === "number" ? tail : undefined,
    level: typeof level === "string" ? level : undefined,
  };
}

/**
 * Renders a service's logs as a live tail.
 *
 * Lines come from Cetacean's own `get_logs` tool through the host, never from
 * its HTTP API, so the widget sees exactly what the calling identity's ACL
 * grants allow — and it keeps working in a sandbox with no route to the server.
 */
export function LogWidget() {
  const { callTool, error: connectionError, isConnected, toolInput } = useCetaceanHost();

  const args = logArgumentsFrom(toolInput);
  const { error, lines } = useLogTail(callTool, isConnected ? args : undefined);

  if (connectionError) {
    return <Message text={`Could not reach the host: ${connectionError.message}`} />;
  }

  if (!isConnected) {
    return <Message text="Connecting to the host…" />;
  }

  if (!args) {
    return <Message text="The host has not said which service to tail." />;
  }

  return (
    <LogTail
      service={args.service}
      lines={lines}
      error={error}
    />
  );
}

function Message({ text }: { text: string }) {
  return <p className="p-3 text-sm opacity-70">{text}</p>;
}

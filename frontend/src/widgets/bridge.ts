import { useApp, useHostStyleVariables } from "@modelcontextprotocol/ext-apps/react";
import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Connects a widget to its MCP Apps host.
 *
 * Widgets never call Cetacean's HTTP API directly. Every read goes through
 * callTool, so the host mediates it and the server applies the caller's ACL
 * grants on the one audited path — a widget works even when the browser has no
 * route to the Cetacean host, and cannot see more than the caller may.
 *
 * The handshake, teardown and size reporting are the SDK's; this only narrows
 * the surface to what Cetacean's widgets use and gives tool calls a typed,
 * structured-content-first shape.
 */
export function useCetaceanHost() {
  // The arguments of the tool call this widget is rendering for. A widget is
  // shown for one result, and this is the only channel that says which — the
  // host does not put them in the frame's URL.
  const [toolInput, setToolInput] = useState<Record<string, unknown> | undefined>(undefined);

  const { app, isConnected, error } = useApp({
    appInfo: { name: "cetacean-widget", version: "1" },
    capabilities: {},
    // Registered before the handshake, because the host may send the tool input
    // the moment it completes and a listener attached afterwards would miss it.
    onAppCreated: (created) => {
      created.addEventListener("toolinput", ({ arguments: args }) => setToolInput(args ?? {}));
    },
  });

  // Applies the host's theme and style variables to the document, so a widget
  // matches the client it is rendered in rather than imposing its own palette.
  useHostStyleVariables(app);

  /**
   * Invokes an MCP tool on the Cetacean server and resolves its structured
   * content.
   *
   * Cetacean's schema'd tools always return structuredContent — output-schema
   * validation is on server-wide, so a result that lacks it would have failed
   * before reaching us — but the fallback keeps a schemaless tool usable.
   */
  const callTool = useCallback(
    async <T>(name: string, args: Record<string, unknown> = {}): Promise<T> => {
      if (!app) {
        throw new Error("host bridge is not connected yet");
      }

      const result = await app.callServerTool({ name, arguments: args });

      if (result.isError) {
        const text = result.content?.find((item) => item.type === "text");

        throw new Error(text?.text ?? `tool ${name} failed`);
      }

      return (result.structuredContent ?? result) as T;
    },
    [app],
  );

  return { app, isConnected, error, callTool, toolInput };
}

/** What a widget gets back from {@link useCetaceanHost}. */
export type CetaceanHost = ReturnType<typeof useCetaceanHost>;

/**
 * Loads data from an MCP tool once the host bridge is connected.
 *
 * The connection is passed in rather than opened here: every useCetaceanHost
 * call builds its own App and runs its own ui/initialize handshake over the
 * same postMessage channel, so a widget calling both hooks would talk to the
 * host twice, with two size-reporting loops and two transports seeing each
 * other's replies. One host per widget, shared with whatever needs it.
 *
 * Widgets are rendered in a sandboxed iframe whose lifetime the host controls,
 * so a call can still be in flight when the widget is torn down; the result of
 * a superseded or late call is discarded rather than set on an unmounted tree.
 */
export function useToolData<T>(
  host: Pick<CetaceanHost, "callTool" | "isConnected">,
  toolName: string,
  args: Record<string, unknown> = {},
): { data: T | undefined; error: Error | undefined; isLoading: boolean } {
  const { isConnected, callTool } = host;

  // Serialised so a caller can pass an object literal without re-firing the
  // effect on every render.
  const argsKey = JSON.stringify(args);
  const requestKey = `${toolName}:${argsKey}`;

  // One piece of state, stamped with the request it answers. Loading is then
  // derived — "the settled result is not for the request we are making" — which
  // needs no setState while the effect runs, and cannot leave a stale result
  // visible for a render when the arguments change.
  const [settled, setSettled] = useState<{
    key: string;
    data?: T;
    error?: Error;
  }>({ key: "" });

  const activeRef = useRef("");

  useEffect(() => {
    if (!isConnected) {
      return;
    }

    activeRef.current = requestKey;

    callTool<T>(toolName, JSON.parse(argsKey) as Record<string, unknown>)
      .then((data) => {
        // A widget's iframe is torn down at the host's discretion, and the
        // arguments can change mid-flight; either way a superseded answer must
        // not overwrite the current one.
        if (activeRef.current === requestKey) {
          setSettled({ key: requestKey, data });
        }
      })
      .catch((cause: unknown) => {
        if (activeRef.current === requestKey) {
          setSettled({
            key: requestKey,
            error: cause instanceof Error ? cause : new Error(String(cause)),
          });
        }
      });
  }, [isConnected, callTool, toolName, argsKey, requestKey]);

  const isCurrent = settled.key === requestKey;

  return {
    data: isCurrent ? settled.data : undefined,
    error: isCurrent ? settled.error : undefined,
    isLoading: !isCurrent,
  };
}

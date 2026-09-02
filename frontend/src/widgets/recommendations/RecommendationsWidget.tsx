import { useCetaceanHost, useToolData } from "../bridge";
import { RecommendationList } from "./RecommendationList";
import type { Recommendation, RecommendationsResult } from "./types";

/**
 * Shows what the recommendation engine currently finds.
 *
 * Picking a finding hands it back to the model as a message rather than calling
 * a tool: a finding is the start of a conversation ("why is this restarting?"),
 * and a widget has neither the room nor the mandate to answer that itself. The
 * host may refuse — not every client accepts messages from a widget — and the
 * list stays readable either way.
 */
export function RecommendationsWidget() {
  const host = useCetaceanHost();
  const { app, error: connectionError, isConnected } = host;

  const { data, error, isLoading } = useToolData<RecommendationsResult>(
    host,
    "get_recommendations",
  );

  function investigate(finding: Recommendation) {
    void app?.sendMessage({
      role: "user",
      content: [
        {
          type: "text",
          text: `Look into this recommendation for ${finding.scope} ${finding.targetName}: ${finding.message} (${finding.category}).`,
        },
      ],
    });
  }

  if (connectionError) {
    return <Message text={`Could not reach the host: ${connectionError.message}`} />;
  }

  if (!isConnected) {
    return <Message text="Connecting to the host…" />;
  }

  if (error) {
    return <Message text={error.message} />;
  }

  if (isLoading || !data) {
    return <Message text="Loading recommendations…" />;
  }

  return (
    <RecommendationList
      items={data.items}
      onInvestigate={investigate}
    />
  );
}

function Message({ text }: { text: string }) {
  return <p className="p-3 text-sm opacity-70">{text}</p>;
}

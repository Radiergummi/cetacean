import CollapsibleSection from "@/components/CollapsibleSection";
import KeyValuePills from "@/components/data/KeyValuePills";
import { Button } from "@/components/ui/button";
import { Code, Layers } from "lucide-react";
import { useState } from "react";

/**
 * Wrapper for integration panels that provides a toggle between
 * the structured view and raw label display.
 */
export function IntegrationSection({
  title,
  defaultOpen,
  rawLabels,
  children,
}: {
  title: string;
  defaultOpen: boolean;
  rawLabels: [string, string][];
  children: React.ReactNode;
}) {
  const [showRaw, setShowRaw] = useState(false);

  return (
    <CollapsibleSection
      title={title}
      defaultOpen={defaultOpen}
      controls={
        <Button
          variant="outline"
          size="xs"
          onClick={() => setShowRaw((previous) => !previous)}
        >
          {showRaw ? <Layers className="size-3" /> : <Code className="size-3" />}
          {showRaw ? "Structured" : "Labels"}
        </Button>
      }
    >
      {showRaw ? (
        <KeyValuePills entries={rawLabels} />
      ) : (
        children
      )}
    </CollapsibleSection>
  );
}

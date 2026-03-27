import CollapsibleSection from "@/components/CollapsibleSection";
import KeyValuePills from "@/components/data/KeyValuePills";
import { Button } from "@/components/ui/button";
import { Code, ExternalLink, Layers } from "lucide-react";
import { useState } from "react";

/**
 * Wrapper for integration panels that provides a toggle between
 * the structured view and raw label display, plus a docs link.
 */
export function IntegrationSection({
  title,
  defaultOpen,
  rawLabels,
  docsUrl,
  children,
}: {
  title: string;
  defaultOpen: boolean;
  rawLabels: [string, string][];
  docsUrl: string;
  children: React.ReactNode;
}) {
  const [showRaw, setShowRaw] = useState(false);

  return (
    <CollapsibleSection
      title={title}
      defaultOpen={defaultOpen}
      controls={
        <>
          <a
            href={docsUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
            onClick={(event) => event.stopPropagation()}
          >
            Docs
            <ExternalLink className="size-3" />
          </a>
          <Button
            variant="outline"
            size="xs"
            onClick={() => setShowRaw((previous) => !previous)}
          >
            {showRaw ? <Layers className="size-3" /> : <Code className="size-3" />}
            {showRaw ? "Structured" : "Labels"}
          </Button>
        </>
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

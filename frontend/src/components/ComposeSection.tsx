import CodeBlock from "./CodeBlock";
import { SectionToggle, useSectionCollapse } from "./CollapsibleSection";
import { IconButton } from "./IconButton";
import { useQuery } from "@tanstack/react-query";
import { Copy, Download } from "lucide-react";

const title = "Compose file";

type Fetcher = (signal?: AbortSignal) => Promise<string>;

/**
 * Renders a stack or service as a compose document. The file redeploys to the
 * same running state on the same cluster; it is not the file that created the
 * stack, and its secrets and configs are referenced rather than exported.
 *
 * The section drives its own collapse state rather than using
 * CollapsibleSection, so the query can wait for the first expand: most visits
 * to a detail page do not want the export.
 */
export default function ComposeSection({
  name,
  queryKey,
  fetcher,
}: {
  name: string;
  queryKey: string;
  fetcher: Fetcher;
}) {
  const { open, toggle } = useSectionCollapse(title, false);
  const { data, error, isPending } = useQuery({
    queryKey: ["compose", queryKey],
    queryFn: ({ signal }) => fetcher(signal),
    enabled: open,
  });

  const filename = `${name}.yaml`;

  function download() {
    if (!data) {
      return;
    }

    const url = URL.createObjectURL(new Blob([data], { type: "application/yaml" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = filename;
    link.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div>
      <div className="mb-3 flex min-h-8 flex-wrap items-center gap-2">
        <SectionToggle
          title={title}
          open={open}
          onToggle={toggle}
        />
        {open && data && (
          <div className="flex items-center gap-2 sm:ms-auto">
            <IconButton
              onClick={() => {
                navigator.clipboard.writeText(data).catch(() => {});
              }}
              title="Copy"
              icon={<Copy className="size-3.5" />}
            />
            <IconButton
              onClick={download}
              title={`Download ${filename}`}
              icon={<Download className="size-3.5" />}
            />
          </div>
        )}
      </div>

      {open && isPending && (
        <p className="text-sm text-muted-foreground">Rendering the compose document…</p>
      )}
      {open && error && (
        <p className="text-sm text-destructive">
          {error instanceof Error ? error.message : "Failed to render the compose document"}
        </p>
      )}
      {open && data && <CodeBlock code={data} />}
    </div>
  );
}

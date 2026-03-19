import { useRef, useEffect } from "react";
import { Play } from "lucide-react";
import { Spinner } from "@/components/Spinner";

interface Props {
  value: string;
  onChange: (value: string) => void;
  onRun: () => void;
  loading: boolean;
}

/**
 * PromQL query input with auto-resize textarea and a Run button.
 * Cmd+Enter or Ctrl+Enter triggers onRun.
 */
export function QueryInput({ value, onChange, onRun, loading }: Props) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const textarea = textareaRef.current;

    if (!textarea) {
      return;
    }

    textarea.style.height = "auto";

    const lineHeight = parseInt(getComputedStyle(textarea).lineHeight, 10) || 20;
    const paddingTop = parseInt(getComputedStyle(textarea).paddingTop, 10) || 0;
    const paddingBottom = parseInt(getComputedStyle(textarea).paddingBottom, 10) || 0;
    const minHeight = lineHeight * 1 + paddingTop + paddingBottom;
    const maxHeight = lineHeight * 5 + paddingTop + paddingBottom;

    textarea.style.height = `${Math.min(Math.max(textarea.scrollHeight, minHeight), maxHeight)}px`;
  }, [value]);

  const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const isModifier = event.metaKey || event.ctrlKey;

    if (isModifier && event.key === "Enter") {
      event.preventDefault();
      onRun();
    }
  };

  return (
    <div className="flex gap-2">
      <textarea
        ref={textareaRef}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="Enter a PromQL expression..."
        rows={1}
        className="flex-1 resize-none overflow-hidden rounded-md border bg-background px-3 py-1.5 font-mono text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
      />

      <button
        onClick={onRun}
        disabled={loading}
        className="inline-flex h-8 items-center gap-1.5 self-start rounded-md border bg-background px-2.5 text-xs hover:bg-muted disabled:opacity-50"
        title="Run query (Cmd+Enter)"
      >
        {loading ? <Spinner className="size-3.5" /> : <Play className="size-3.5" />}
        Run
      </button>
    </div>
  );
}

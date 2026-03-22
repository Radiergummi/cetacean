import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { Check, ChevronsUpDown, Search, X } from "lucide-react";
import { useRef, useState } from "react";

interface MultiComboboxOption {
  value: string;
  label: string;
  description?: string;
}

interface MultiComboboxProps {
  values: string[];
  onChange: (values: string[]) => void;
  options: MultiComboboxOption[];
  placeholder?: string;
  className?: string;
  /** Transform custom input before adding (e.g. toUpperCase for capabilities) */
  transformInput?: (value: string) => string;
}

function Chips({ values, onRemove }: { values: string[]; onRemove: (value: string) => void }) {
  return values.map((value) => (
    <span
      key={value}
      className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
    >
      {value}
      <button
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          onRemove(value);
        }}
        className="text-muted-foreground hover:text-foreground"
        aria-label={`Remove ${value}`}
      >
        <X className="size-3" />
      </button>
    </span>
  ));
}

export function MultiCombobox({
  values,
  onChange,
  options,
  placeholder,
  className,
  transformInput,
}: MultiComboboxProps) {
  const [input, setInput] = useState("");
  const [open, setOpen] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const transform = transformInput ?? ((value: string) => value);

  function add(value: string) {
    if (!values.includes(value)) {
      onChange([...values, value]);
    }

    setInput("");
  }

  function remove(value: string) {
    onChange(values.filter((v) => v !== value));
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter" && input.trim()) {
      event.preventDefault();
      add(transform(input.trim()));
    }
  }

  if (options.length === 0) {
    return (
      <div
        className={cn(
          "flex min-h-8 w-full flex-wrap items-center gap-1 rounded-md border border-input bg-transparent px-2 py-1 text-sm",
          "focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50",
          className,
        )}
      >
        <Chips
          values={values}
          onRemove={remove}
        />
        <input
          value={input}
          onChange={(event) => setInput(event.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={values.length === 0 ? placeholder : undefined}
          className="min-w-20 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
        />
      </div>
    );
  }

  const filtered = options.filter(
    (option) =>
      !values.includes(option.value) &&
      (option.value.toLowerCase().includes(input.toLowerCase()) ||
        option.label.toLowerCase().includes(input.toLowerCase()) ||
        option.description?.toLowerCase().includes(input.toLowerCase())),
  );

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);

        if (nextOpen) {
          setInput("");
          requestAnimationFrame(() => inputRef.current?.focus());
        }
      }}
    >
      <PopoverTrigger
        className={cn(
          "flex min-h-8 w-full flex-wrap items-center gap-1 rounded-md border border-input bg-transparent px-2 py-1 text-sm",
          "hover:bg-muted outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
          className,
        )}
      >
        <Chips
          values={values}
          onRemove={remove}
        />

        {values.length === 0 && (
          <span className="text-muted-foreground">{placeholder || "Select..."}</span>
        )}

        <ChevronsUpDown className="ml-auto size-3 shrink-0 opacity-50" />
      </PopoverTrigger>

      <PopoverContent
        align="start"
        className="w-64 gap-0 p-0"
      >
        <div className="flex items-center gap-2 border-b px-3 py-2">
          <Search className="size-3.5 shrink-0 text-muted-foreground" />

          <input
            ref={inputRef}
            value={input}
            onChange={(event) => setInput(event.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Search or enter custom..."
            className="h-6 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>

        <div className="max-h-52 overflow-y-auto p-1">
          {filtered.map((option) => (
            <button
              key={option.value}
              type="button"
              onClick={() => add(option.value)}
              className="flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted"
            >
              <Check className="mt-0.5 size-3.5 shrink-0 opacity-0" />

              <div className="min-w-0 flex-1">
                <div className="font-mono text-sm">{option.label}</div>
                {option.description && (
                  <div className="text-xs leading-snug text-muted-foreground">
                    {option.description}
                  </div>
                )}
              </div>
            </button>
          ))}

          {filtered.length === 0 && input.trim() && !values.includes(transform(input.trim())) && (
            <button
              type="button"
              onClick={() => add(transform(input.trim()))}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted"
            >
              <Check className="size-3.5 shrink-0 opacity-0" />

              <span>
                Add "<span className="font-mono">{transform(input.trim())}</span>"
              </span>
            </button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

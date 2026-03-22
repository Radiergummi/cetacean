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
}

export function MultiCombobox({
  values,
  onChange,
  options,
  placeholder,
  className,
}: MultiComboboxProps) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  const filtered = options.filter(
    (option) =>
      !values.includes(option.value) &&
      (option.value.toLowerCase().includes(search.toLowerCase()) ||
        option.label.toLowerCase().includes(search.toLowerCase()) ||
        option.description?.toLowerCase().includes(search.toLowerCase())),
  );

  function add(value: string) {
    if (!values.includes(value)) {
      onChange([...values, value]);
    }

    setSearch("");
  }

  function remove(value: string) {
    onChange(values.filter((v) => v !== value));
  }

  return (
    <div className="flex flex-col gap-1.5">
      {values.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {values.map((value) => (
            <span
              key={value}
              className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
            >
              {value}
              <button
                type="button"
                onClick={() => remove(value)}
                className="text-muted-foreground hover:text-foreground"
                aria-label={`Remove ${value}`}
              >
                <X className="size-3" />
              </button>
            </span>
          ))}
        </div>
      )}

      <Popover
        open={open}
        onOpenChange={(nextOpen) => {
          setOpen(nextOpen);

          if (nextOpen) {
            setSearch("");
            requestAnimationFrame(() => inputRef.current?.focus());
          }
        }}
      >
        <PopoverTrigger
          className={cn(
            "flex h-8 items-center justify-between rounded-md border border-input bg-transparent px-3 text-sm",
            "hover:bg-muted outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
            "font-sans text-muted-foreground",
            className,
          )}
        >
          <span className="truncate">{placeholder || "Select..."}</span>
          <ChevronsUpDown className="ml-2 size-3 shrink-0 opacity-50" />
        </PopoverTrigger>

        <PopoverContent
          align="start"
          className="w-64 gap-0 p-0"
        >
          <div className="flex items-center gap-2 border-b px-3 py-2">
            <Search className="size-3.5 shrink-0 text-muted-foreground" />

            <input
              ref={inputRef}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && search.trim()) {
                  event.preventDefault();
                  add(search.trim().toUpperCase());
                }
              }}
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

            {filtered.length === 0 &&
              search.trim() &&
              !values.includes(search.trim().toUpperCase()) && (
                <button
                  type="button"
                  onClick={() => add(search.trim().toUpperCase())}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted"
                >
                  <Check className="size-3.5 shrink-0 opacity-0" />

                  <span>
                    Add "<span className="font-mono">{search.trim().toUpperCase()}</span>"
                  </span>
                </button>
              )}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { Check, ChevronsUpDown, Search } from "lucide-react";
import { useRef, useState } from "react";

export interface ComboboxOption {
  value: string;
  label: string;
  description?: string;
}

interface ComboboxProps {
  value: string;
  onChange: (value: string) => void;
  options: ComboboxOption[];
  placeholder?: string;
  className?: string;
  /** Allow typing custom values not in the options list (default true) */
  allowCustom?: boolean;
}

export function Combobox({
  value,
  onChange,
  options,
  placeholder,
  className,
  allowCustom = true,
}: ComboboxProps) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  const filtered = options.filter(
    ({ description, label, value: optionValue }) =>
      optionValue.toLowerCase().includes(search.toLowerCase()) ||
      label.toLowerCase().includes(search.toLowerCase()) ||
      description?.toLowerCase().includes(search.toLowerCase()),
  );

  function select(selected: string) {
    onChange(selected);
    setSearch("");
    setOpen(false);
  }

  return (
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
          "flex w-full h-8 items-center justify-between rounded-md border border-input bg-transparent px-3 text-sm",
          "hover:bg-muted outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
          !value && "font-sans text-muted-foreground",
          className,
        )}
      >
        <span className="truncate">
          {(value && options.find((option) => option.value === value)?.label) ||
            value ||
            placeholder ||
            "Select…"}
        </span>
        <ChevronsUpDown className="ml-2 size-3 shrink-0 opacity-50" />
      </PopoverTrigger>

      <PopoverContent
        align="start"
        className="w-full max-w-84 gap-0 p-0"
      >
        <div className="flex items-center gap-2 border-b px-3 py-2">
          <Search className="size-3.5 shrink-0 text-muted-foreground" />

          <input
            ref={inputRef}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                event.stopPropagation();
              }

              if (event.key === "Enter" && search.trim() && allowCustom) {
                event.preventDefault();
                select(search.trim());
              }
            }}
            placeholder={allowCustom ? "Search or enter custom…" : "Search…"}
            className="h-6 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>

        <div className="max-h-52 overflow-y-auto p-1">
          {filtered.map((option) => (
            <button
              key={option.value}
              type="button"
              onClick={() => select(option.value)}
              className={cn(
                "flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted",
                option.value === value && "bg-muted",
              )}
            >
              <Check
                className={cn(
                  "mt-0.5 size-3.5 shrink-0",
                  option.value === value ? "text-primary opacity-100" : "opacity-0",
                )}
              />

              <div className="min-w-0 flex-1">
                <div className="truncate text-sm">{option.label}</div>
                {option.description && (
                  <div className="truncate text-xs text-muted-foreground">{option.description}</div>
                )}
              </div>
            </button>
          ))}

          {filtered.length === 0 && search.trim() && allowCustom && (
            <button
              type="button"
              onClick={() => select(search.trim())}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted"
            >
              <Check className="size-3.5 shrink-0 opacity-0" />

              <span>
                Use "<span>{search.trim()}</span>"
              </span>
            </button>
          )}

          {filtered.length === 0 && !allowCustom && (
            <p className="px-2 py-1.5 text-sm text-muted-foreground">No matches</p>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

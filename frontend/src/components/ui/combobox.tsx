import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { Check, ChevronsUpDown, Search } from "lucide-react";
import { useRef, useState } from "react";

interface ComboboxOption {
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
}

export function Combobox({ value, onChange, options, placeholder, className }: ComboboxProps) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  const filtered = options.filter(
    (option) =>
      option.value.toLowerCase().includes(search.toLowerCase()) ||
      option.label.toLowerCase().includes(search.toLowerCase()) ||
      option.description?.toLowerCase().includes(search.toLowerCase()),
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
          "flex h-8 items-center justify-between rounded-md border border-input bg-transparent px-3 font-mono text-sm",
          "hover:bg-muted outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
          !value && "font-sans text-muted-foreground",
          className,
        )}
      >
        <span className="truncate">{value || placeholder || "Select..."}</span>
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
                select(search.trim());
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
              onClick={() => select(option.value)}
              className={cn(
                "flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted",
                value === option.value && "bg-muted",
              )}
            >
              <Check
                className={cn(
                  "mt-0.5 size-3.5 shrink-0",
                  value === option.value ? "text-primary opacity-100" : "opacity-0",
                )}
              />

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

          {filtered.length === 0 && search.trim() && (
            <button
              type="button"
              onClick={() => select(search.trim())}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted"
            >
              <Check className="size-3.5 shrink-0 opacity-0" />

              <span>
                Use "<span className="font-mono">{search.trim()}</span>"
              </span>
            </button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

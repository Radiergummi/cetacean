import { Search, X } from "lucide-react";

export default function SearchInput({
  value,
  onChange,
  placeholder,
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
}) {
  return (
    <div className={`relative w-full ${className}`}>
      <Search className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground pointer-events-none" />

      <input
        type="text"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder || "Search…"}
        className="w-full pl-9 pr-8 py-2 border rounded-md text-sm bg-background focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent"
      />

      {value && (
        <button
          onClick={() => onChange("")}
          className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 rounded hover:bg-muted text-muted-foreground"
        >
          <X className="size-3.5" />
        </button>
      )}
    </div>
  );
}

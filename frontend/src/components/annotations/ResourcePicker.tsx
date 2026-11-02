import { api } from "@/api/client";
import type { AnnotationScope } from "@/api/types";
import { Input } from "@/components/ui/input";
import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useEffect, useState } from "react";

type ResourceScope = Exclude<AnnotationScope, "cluster">;

const debounceMilliseconds = 300;

const searchedTypes = [
  ["services", "service"],
  ["stacks", "stack"],
  ["nodes", "node"],
] as const;

/**
 * Finds a service, stack or node to annotate through the search API. A stack
 * is addressed by its name.
 */
export default function ResourcePicker({
  value,
  onChange,
}: {
  value: ResourceScope | null;
  onChange: (scope: ResourceScope | null) => void;
}) {
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query.trim()), debounceMilliseconds);

    return () => clearTimeout(timer);
  }, [query]);

  const { data } = useQuery({
    queryKey: ["annotation-resource-search", debounced],
    queryFn: ({ signal }) => api.search(debounced, 10, signal),
    enabled: debounced !== "" && value === null,
  });

  if (value) {
    return (
      <div className="flex items-center gap-2 rounded-md border px-2 py-1 text-sm">
        <span className="text-xs text-muted-foreground">{value.type}</span>
        <span className="font-medium">{value.name}</span>
        <button
          type="button"
          aria-label={`Clear ${value.name}`}
          onClick={() => onChange(null)}
          className="ms-auto text-muted-foreground hover:text-foreground"
        >
          <X className="size-3.5" />
        </button>
      </div>
    );
  }

  const options = searchedTypes.flatMap(([plural, type]) =>
    (data?.results[plural] ?? []).map(({ id: resultId, name }) => ({
      type,
      id: type === "stack" ? name : resultId,
      name,
    })),
  );

  return (
    <div className="flex flex-col gap-1">
      <Input
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        placeholder="Search services, stacks and nodes…"
        autoComplete="off"
      />

      {options.length > 0 && (
        <ul className="max-h-40 overflow-y-auto rounded-md border text-sm">
          {options.map((option) => (
            <li key={`${option.type}-${option.id}`}>
              <button
                type="button"
                onClick={() => onChange(option)}
                className="flex w-full items-center gap-2 px-2 py-1 text-start hover:bg-muted"
              >
                <span className="text-xs text-muted-foreground">{option.type}</span>
                {option.name}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

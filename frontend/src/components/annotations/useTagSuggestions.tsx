import { api } from "@/api/client";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useEffect, useId, useState, type ChangeEvent } from "react";

const debounceMilliseconds = 200;

/**
 * How many tags a suggestion list offers.
 */
const suggestionLimit = 20;

const normalize = (typed: string) => typed.trim().toLowerCase();

/**
 * The tags in use matching what is typed, most used first, without those
 * already `chosen`: as a list, and as a datalist for a tag input. `picked` is
 * the tag a change took from the datalist, or null for typing.
 */
export function useTagSuggestions(typed: string, chosen: string[]) {
  const listId = useId();
  const [query, setQuery] = useState(() => normalize(typed));

  useEffect(() => {
    const timer = setTimeout(() => setQuery(normalize(typed)), debounceMilliseconds);

    return () => clearTimeout(timer);
  }, [typed]);

  const { data } = useQuery({
    queryKey: ["annotations", "tags", "suggestions", query],
    queryFn: ({ signal }) =>
      api.annotationTagsPage(api.annotationTagsPath(query, suggestionLimit), signal),
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });

  const suggestions = (data?.items ?? [])
    .map(({ tag }) => tag)
    .filter((tag) => !chosen.includes(tag));

  const picked = ({ nativeEvent, target: { value } }: ChangeEvent<HTMLInputElement>) =>
    (!(nativeEvent instanceof InputEvent) || nativeEvent.inputType === "insertReplacementText") &&
    suggestions.includes(value)
      ? value
      : null;

  const datalist = (
    <datalist id={listId}>
      {suggestions.map((tag) => (
        <option
          key={tag}
          value={tag}
        />
      ))}
    </datalist>
  );

  return { listId, datalist, picked, suggestions };
}

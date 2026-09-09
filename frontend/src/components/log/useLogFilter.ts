import type { Level, LogLine } from "./log-utils";
import { useMemo, useState } from "react";

export function useLogFilter(lines: LogLine[]) {
  const [search, setSearch] = useState("");
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [matchIndex, setMatchIndex] = useState(0);
  const [useRegex, setUseRegex] = useState(false);
  const [levelFilter, setLevelFilter] = useState<Level | "all">("all");
  const [taskFilter, setTaskFilter] = useState<string | null>(null);

  const searchMatcher = useMemo(() => {
    if (!search) {
      return null;
    }

    if (useRegex) {
      try {
        const expression = new RegExp(search, caseSensitive ? "" : "i");

        return (message: string) => expression.test(message);
      } catch {
        // fall through to plain-text match
      }
    }

    const query = caseSensitive ? search : search.toLowerCase();

    return (message: string) => (caseSensitive ? message : message.toLowerCase()).includes(query);
  }, [search, caseSensitive, useRegex]);

  const filtered = useMemo(() => {
    let result = lines;

    if (levelFilter !== "all") {
      result = result.filter(({ level }) => level === levelFilter);
    }

    if (taskFilter) {
      result = result.filter(({ attrs }) => attrs?.taskId === taskFilter);
    }

    if (searchMatcher) {
      result = result.filter(({ message }) => searchMatcher(message));
    }

    return result;
  }, [lines, searchMatcher, levelFilter, taskFilter]);

  // The cursor used to reset on `filtered`'s identity, which a live tail changes
  // on every frame — so stepping through matches on a streaming log was pulled
  // back to the first hit before it could be read. What should reset it is the
  // *criteria* changing, which is a different question from the list growing.
  // Adjusted during render rather than in an effect so no pass ever commits a
  // cursor pointing past the end.
  const criteria = `${search}|${caseSensitive}|${useRegex}|${levelFilter}|${taskFilter ?? ""}`;
  const [previousCriteria, setPreviousCriteria] = useState(criteria);

  if (previousCriteria !== criteria) {
    setPreviousCriteria(criteria);
    setMatchIndex(0);
  } else if (matchIndex > 0 && matchIndex >= filtered.length) {
    setMatchIndex(Math.max(filtered.length - 1, 0));
  }

  return {
    search,
    setSearch,
    caseSensitive,
    setCaseSensitive,
    matchIndex,
    setMatchIndex,
    useRegex,
    setUseRegex,
    levelFilter,
    setLevelFilter,
    taskFilter,
    setTaskFilter,
    filtered,
  };
}

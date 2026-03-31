import type { Level, LogLine } from "./log-utils";
import { useEffect, useMemo, useState } from "react";

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

  useEffect(() => {
    setMatchIndex(0);
  }, [filtered]);

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

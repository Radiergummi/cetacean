import type { Level, LogLine } from "./log-utils";
import { useEffect, useMemo, useState } from "react";

export function useLogFilter(lines: LogLine[]) {
  const [search, setSearch] = useState("");
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [matchIndex, setMatchIndex] = useState(0);
  const [useRegex, setUseRegex] = useState(false);
  const [levelFilter, setLevelFilter] = useState<Level | "all">("all");
  const [taskFilter, setTaskFilter] = useState<string | null>(null);

  const filtered = useMemo(() => {
    let result = lines;

    if (levelFilter !== "all") {
      result = result.filter(({ level }) => level === levelFilter);
    }
    if (taskFilter) {
      result = result.filter(({ attrs }) => attrs?.taskId === taskFilter);
    }
    if (search) {
      if (useRegex) {
        try {
          const expression = new RegExp(search, caseSensitive ? "" : "i");
          result = result.filter(({ message }) => expression.test(message));
        } catch {
          const query = caseSensitive ? search : search.toLowerCase();
          result = result.filter(({ message }) =>
            (caseSensitive ? message : message.toLowerCase()).includes(query),
          );
        }
      } else {
        const query = caseSensitive ? search : search.toLowerCase();
        result = result.filter(({ message }) =>
          (caseSensitive ? message : message.toLowerCase()).includes(query),
        );
      }
    }

    return result;
  }, [lines, search, caseSensitive, useRegex, levelFilter, taskFilter]);

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

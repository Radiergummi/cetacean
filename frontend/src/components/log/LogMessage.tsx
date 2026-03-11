import type {LogLine} from "./log-utils";
import {isJSON, prettyJSON} from "./log-utils";

export function LogMessage({
    line,
    search,
    caseSensitive,
    useRegex,
    prettyJson,
}: {
    line: LogLine;
    search: string;
    caseSensitive: boolean;
    useRegex: boolean;
    prettyJson: boolean;
}) {
    const msg = line.message;

    // If searching, highlight matches
    if (search) {
        const text = prettyJson && isJSON(msg) ? prettyJSON(msg) : msg;
        return <HighlightedText text={text} search={search} caseSensitive={caseSensitive} useRegex={useRegex}/>;
    }

    // Auto-format JSON when pretty-printing is enabled
    if (isJSON(msg)) {
        const text = prettyJson ? prettyJSON(msg) : msg;
        return <span className="text-emerald-700 dark:text-emerald-300">{text}</span>;
    }

    // Color error-level lines
    if (line.level === "error") {
        return <span className="text-red-600 dark:text-red-300">{msg}</span>;
    }
    if (line.level === "warn") {
        return <span className="text-yellow-700 dark:text-yellow-300">{msg}</span>;
    }
    if (line.level === "debug") {
        return <span className="text-muted-foreground">{msg}</span>;
    }

    return <>{msg}</>;
}

export function HighlightedText({
    text,
    search,
    caseSensitive,
    useRegex,
}: {
    text: string;
    search: string;
    caseSensitive: boolean;
    useRegex: boolean;
}) {
    const pattern = useRegex ? search : search.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    let re: RegExp;
    try {
        re = new RegExp(`(${pattern})`, caseSensitive ? "g" : "gi");
    } catch {
        return <>{text}</>;
    }
    const parts = text.split(re);

    return (
        <>
            {parts.map((part, index) => {
                const isMatch = re.test(part) && part.length > 0;
                re.lastIndex = 0;
                return isMatch ? (
                    <mark
                        key={index}
                        className="bg-yellow-200 dark:bg-yellow-500/40 text-yellow-900 dark:text-yellow-200 rounded-xs px-px"
                    >
                        {part}
                    </mark>
                ) : (
                    <span key={index}>{part}</span>
                );
            })}
        </>
    );
}

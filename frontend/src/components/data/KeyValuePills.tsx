export default function KeyValuePills({entries}: { entries: [string, string][] }) {
    return (
        <ul className="flex flex-wrap gap-2">
            {entries.map(([key, value]) => (
                <li
                    key={key}
                    className="inline-flex items-baseline rounded-md border text-xs font-mono overflow-hidden"
                >
                    <span className="px-2 py-1 text-muted-foreground bg-muted/50 ">
                        {key}
                    </span>
                    {value ? <span className="px-2 py-1">{value}</span> : undefined}
                </li>
            ))}
        </ul>
    );
}

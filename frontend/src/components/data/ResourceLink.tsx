import InfoCard from "../InfoCard";

export default function ResourceLink({
    label,
    name,
    to,
}: {
    label: string;
    name?: string;
    to: string;
}) {
    if (!name) return null;

    return <InfoCard label={label} value={name} href={to} />;
}

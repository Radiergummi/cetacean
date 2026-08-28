import InfoCard from "../InfoCard";
import type React from "react";

export default function ResourceLink({
  label,
  name,
  to,
}: {
  label: string;
  name?: string | React.ReactNode | undefined;
  to?: string | undefined;
}) {
  if (!name) {
    return null;
  }

  return (
    <InfoCard
      label={label}
      value={name}
      href={to}
    />
  );
}

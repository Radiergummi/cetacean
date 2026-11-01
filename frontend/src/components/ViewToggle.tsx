import SegmentedControl from "./SegmentedControl";
import type { ReactNode } from "react";

export interface View<T extends string> {
  value: T;
  label: string;
  icon: ReactNode;
}

/**
 * Switches between views of the same content, one icon per view; the label is
 * its accessible name and its title.
 */
export default function ViewToggle<T extends string>({
  views,
  value,
  onChange,
  className,
}: {
  views: View<T>[];
  value: T;
  onChange: (value: T) => void;
  className?: string | undefined;
}) {
  return (
    <SegmentedControl
      segments={views}
      value={value}
      onChange={onChange}
      className={className}
    />
  );
}

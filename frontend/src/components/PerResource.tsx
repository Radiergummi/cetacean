import { Fragment, type ReactNode } from "react";
import { useParams } from "react-router-dom";

// Remounts a detail page when the resource it shows changes, so no draft, open
// editor or captured ETag carries over from one resource to the next.
export default function PerResource({ children }: { children: ReactNode }) {
  const params = useParams();

  return <Fragment key={Object.values(params).join("/")}>{children}</Fragment>;
}

import type { ServiceRef } from "../api/types";
import CollapsibleSection from "./CollapsibleSection";
import ResourceName from "./ResourceName";
import { Badge } from "./ui/badge";
import { Link } from "react-router-dom";

interface Props {
  services: ServiceRef[];
  label: string;
  emptyMessage: string;
}

export default function ServiceRefList({ services, label, emptyMessage }: Props) {
  return (
    <CollapsibleSection title={label}>
      {services.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {services.map(({ id, name }) => (
            <Badge
              key={id}
              variant="outline"
              className="h-auto! px-4! py-2! text-sm!"
              render={<Link to={`/services/${id}`} />}
            >
              <ResourceName name={name || id.slice(0, 12)} />
            </Badge>
          ))}
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">{emptyMessage}</p>
      )}
    </CollapsibleSection>
  );
}

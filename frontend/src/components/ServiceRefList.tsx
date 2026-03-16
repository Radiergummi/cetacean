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
          {services.map((svc) => (
            <Badge
              key={svc.id}
              variant="outline"
              className="!h-auto !px-4 !py-2 !text-sm"
              render={<Link to={`/services/${svc.id}`} />}
            >
              <ResourceName name={svc.name || svc.id.slice(0, 12)} />
            </Badge>
          ))}
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">{emptyMessage}</p>
      )}
    </CollapsibleSection>
  );
}

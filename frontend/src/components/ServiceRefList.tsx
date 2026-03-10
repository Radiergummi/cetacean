import { Link } from "react-router-dom";
import type { ServiceRef } from "../api/types";
import { Badge } from "./ui/badge";
import ResourceName from "./ResourceName";

interface Props {
  services: ServiceRef[];
  label: string;
  emptyMessage: string;
}

export default function ServiceRefList({ services, label, emptyMessage }: Props) {
  return (
    <div>
      <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
        {label}
      </h2>
      {services.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {services.map((svc) => (
            <Badge key={svc.id} variant="outline" render={<Link to={`/services/${svc.id}`} />}>
              <ResourceName name={svc.name || svc.id.slice(0, 12)} />
            </Badge>
          ))}
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">{emptyMessage}</p>
      )}
    </div>
  );
}

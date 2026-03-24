import type { Plugin } from "../api/types";
import { Link } from "react-router-dom";

export default function PluginTable({ plugins }: { plugins: Plugin[] }) {
  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full">
        <thead>
          <tr className="border-b text-left text-xs font-medium tracking-wider text-muted-foreground uppercase">
            <th className="p-3">Name</th>
            <th className="p-3">Type</th>
            <th className="p-3">Status</th>
          </tr>
        </thead>
        <tbody>
          {plugins.map(({ Config: { Interface }, Enabled, Id, Name }) => (
            <tr
              key={Id ?? Name}
              className="border-b last:border-b-0"
            >
              <td className="p-3 font-mono text-xs">
                <Link
                  to={`/plugins/${encodeURIComponent(Name)}`}
                  className="text-link hover:underline"
                >
                  {Name}
                </Link>
              </td>
              <td className="p-3 text-sm text-muted-foreground">
                {Interface.Types?.map(({ Capability }) => Capability).join(", ") || "—"}
              </td>
              <td className="p-3">
                <span
                  data-enabled={Enabled || undefined}
                  className="inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground data-enabled:bg-green-500/10 data-enabled:text-green-500"
                >
                  {Enabled ? "Enabled" : "Disabled"}
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

import { formatBytes } from "../../lib/formatBytes";
import Sparkline from "./Sparkline";

interface Props {
  data: number[] | undefined;
  currentValue: number | null | undefined;
  type: "cpu" | "memory";
}

const COLORS = {
  cpu: "#4f8cf6",
  memory: "#34d399",
};

function formatValue(value: number | null | undefined, type: "cpu" | "memory"): string {
  if (value == null) return "\u2014";
  if (type === "cpu") return `${value.toFixed(1)}%`;
  return formatBytes(value);
}

export default function TaskSparkline({ data, currentValue, type }: Props) {
  if (!data?.length) {
    return <span className="text-xs text-muted-foreground">{"\u2014"}</span>;
  }

  return (
    <span className="inline-flex items-center gap-1.5">
      <Sparkline
        data={data}
        color={COLORS[type]}
      />
      <span className="text-xs whitespace-nowrap tabular-nums">
        {formatValue(currentValue, type)}
      </span>
    </span>
  );
}

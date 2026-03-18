import Sparkline from "./Sparkline";
import { formatBytes, formatPercentage } from "@/lib/format.ts";

interface Props {
  data: number[] | undefined;
  currentValue: number | null | undefined;
  type: "cpu" | "memory";
}

const CHART_CLASSES = {
  cpu: "text-chart-cpu",
  memory: "text-chart-memory",
};

function formatValue(value: number | null | undefined, type: "cpu" | "memory"): string {
  if (value == null) {
    return "\u2014";
  }

  if (type === "cpu") {
    return formatPercentage(value);
  }

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
        className={CHART_CLASSES[type]}
      />
      <span className="text-xs whitespace-nowrap tabular-nums">
        {formatValue(currentValue, type)}
      </span>
    </span>
  );
}

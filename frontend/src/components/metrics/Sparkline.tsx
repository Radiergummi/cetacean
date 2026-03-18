interface Props {
  data: number[];
  width?: number;
  height?: number;
  className?: string;
}

export default function Sparkline({data, width = 80, height = 24, className = "text-chart-1"}: Props) {
  if (data.length < 2) {
    return <div style={{width, height}} />;
  }

  const min = Math.min(...data);
  const max = Math.max(...data);
  const range = max - min || 1;
  const pad = 1;

  const points = data
    .map((value, index) => {
      const x = pad + (index / (data.length - 1)) * (width - pad * 2);
      const y = pad + (1 - (value - min) / range) * (height - pad * 2);
      return `${x},${y}`;
    })
    .join(" ");

  return (
    <svg
      width={width}
      height={height}
      className={`inline-block align-middle ${className}`}
    >
      <polyline
        points={points}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

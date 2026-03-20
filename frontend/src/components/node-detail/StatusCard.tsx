import InfoCard from "@/components/InfoCard";
import type React from "react";

const size = 50;

const statuses: Record<string, { fill: string; icon: React.ReactNode }> = {
  ready: {
    fill: "fill-green-500",
    icon: (
      <path
        d="M15 25.5 L21.5 32 L35 19"
        fill="none"
        stroke="white"
        strokeWidth={3}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    ),
  },
  down: {
    fill: "fill-red-500",
    icon: (
      <path
        d="M17 17 L33 33 M33 17 L17 33"
        fill="none"
        stroke="white"
        strokeWidth={3}
        strokeLinecap="round"
      />
    ),
  },
  disconnected: {
    fill: "fill-amber-500",
    icon: (
      <path
        d="M25 16 L25 28 M25 33 L25 33.5"
        fill="none"
        stroke="white"
        strokeWidth={3}
        strokeLinecap="round"
      />
    ),
  },
  unknown: {
    fill: "fill-muted-foreground",
    icon: (
      <>
        <path
          d="M20.5 19 C20.5 15.5 29.5 15.5 29.5 20.5 C29.5 24 25 23.5 25 27.5"
          fill="none"
          stroke="white"
          strokeWidth={2.5}
          strokeLinecap="round"
        />
        <circle
          cx={25}
          cy={33}
          r={1.5}
          fill="white"
        />
      </>
    ),
  },
};

export function StatusCard({ state }: { state: string }) {
  const status = statuses[state] ?? statuses.unknown;

  return (
    <InfoCard
      label="Status"
      value={<span className="capitalize">{state}</span>}
      right={
        <svg
          width={size}
          height={size}
          viewBox={`0 0 ${size} ${size}`}
        >
          <circle
            cx={size / 2}
            cy={size / 2}
            r={size / 2}
            className={status.fill}
          />
          {status.icon}
        </svg>
      }
    />
  );
}

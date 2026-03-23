import type { Healthcheck } from "@/api/types";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatDuration } from "@/lib/format";
import { useState } from "react";

const labelStyle = { fontSize: 7 } as const;
type HoverGroup = "failed" | "ghost" | null;

/**
 * Visualizes the healthcheck timing as a 1D horizontal timeline,
 * from container start (0) through the start period to the point
 * where `retries` consecutive failures would mark the container unhealthy.
 */
export function HealthcheckTimeline({ healthcheck }: { healthcheck: Healthcheck }) {
  const [hoverGroup, setHoverGroup] = useState<HoverGroup>(null);

  const interval = healthcheck.Interval || 30e9;
  const timeout = healthcheck.Timeout || 30e9;
  const startPeriod = healthcheck.StartPeriod || 0;
  const startInterval = healthcheck.StartInterval || interval;
  const retries = healthcheck.Retries || 3;

  // When timeout > interval, Docker waits for the check to finish before
  // starting the next one, so the effective spacing is max(interval, timeout).
  const effectiveInterval = Math.max(interval, timeout);
  const firstCheck = startPeriod > 0 ? startPeriod : effectiveInterval;
  const totalDuration = firstCheck + retries * effectiveInterval;

  if (totalDuration <= 0) {
    return null;
  }

  // Extend the axis a bit past the last check so offset labels don't crowd the end label
  const axisDuration = totalDuration + effectiveInterval * 0.3;

  const viewWidth = 650;
  const viewHeight = 44;
  const marginLeft = 4;
  const marginRight = 4;
  const axisY = 15;
  const usable = viewWidth - marginLeft - marginRight;
  const x = (t: number) => marginLeft + (t / axisDuration) * usable;

  // Checks during the start period (failures forgiven)
  const startChecks: number[] = [];

  if (startPeriod > 0) {
    for (let t = startInterval; t < startPeriod && startChecks.length < 30; t += startInterval) {
      startChecks.push(t);
    }
  }

  // All interval ticks — the regular heartbeat cadence, including start-period checks.
  // Only rendered when timeout > interval (checks get delayed past the next tick).
  const hasGhostTicks = timeout > interval;
  const intervalTicks: number[] = [...startChecks];

  if (hasGhostTicks) {
    for (let t = firstCheck; t <= totalDuration && intervalTicks.length < 60; t += interval) {
      intervalTicks.push(t);
    }
  }

  // Failure checks — the first check fires at firstCheck (shown as an interval tick),
  // so timeout-delayed checks start one effectiveInterval later.
  const regularChecks: number[] = [];

  for (let i = 1; i <= retries; i++) {
    regularChecks.push(firstCheck + i * effectiveInterval);
  }

  const showFailed = hoverGroup === "failed";
  const showGhost = hoverGroup === "ghost";

  return (
    <div className="mt-1 overflow-x-auto">
    <svg
      viewBox={`0 0 ${viewWidth} ${viewHeight}`}
      width={viewWidth}
      height={viewHeight}
      role="img"
      aria-label={`Healthcheck timeline: ${
        startPeriod > 0 ? `${formatDuration(startPeriod)} start period, then ` : ""
      }checks every ${formatDuration(effectiveInterval)}, ${retries} retries to unhealthy`}
    >
      {/* Start period background */}
      {startPeriod > 0 && (
        <Tooltip>
          <TooltipTrigger
            render={
              <rect
                x={x(0)}
                y={axisY - 9}
                width={x(startPeriod) - x(0)}
                height={18}
                rx={2}
                tabIndex={0}
                className="fill-amber-100 outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring/50 dark:fill-amber-950"
              />
            }
          />
          <TooltipContent>
            <p className="font-medium">Start period</p>
            <p>Failures during this grace period do not count toward retries</p>
            <p className="text-muted-foreground">0 – {formatDuration(startPeriod, true)}</p>
          </TooltipContent>
        </Tooltip>
      )}

      {/* Main axis */}
      <line
        x1={x(0)}
        y1={axisY}
        x2={x(axisDuration)}
        y2={axisY}
        className="stroke-zinc-200 dark:stroke-zinc-700"
        strokeWidth={1}
      />

      {/* Hover track bars — behind all dots */}
      <defs>
        <linearGradient id="hc-grad-amber-light">
          <stop
            offset="0%"
            style={{ stopColor: "var(--color-amber-300)" }}
          />
          <stop
            offset="100%"
            style={{ stopColor: "var(--color-zinc-200)" }}
          />
        </linearGradient>
        <linearGradient id="hc-grad-amber-dark">
          <stop
            offset="0%"
            style={{ stopColor: "var(--color-amber-800)" }}
          />
          <stop
            offset="100%"
            style={{ stopColor: "var(--color-zinc-700)" }}
          />
        </linearGradient>
        <linearGradient id="hc-grad-red-light">
          <stop
            offset="8%"
            style={{ stopColor: "var(--color-zinc-300)" }}
          />
          <stop
            offset="25%"
            style={{ stopColor: "var(--color-red-400)" }}
          />
        </linearGradient>
        <linearGradient id="hc-grad-red-dark">
          <stop
            offset="8%"
            style={{ stopColor: "var(--color-zinc-600)" }}
          />
          <stop
            offset="25%"
            style={{ stopColor: "var(--color-red-900)" }}
          />
        </linearGradient>
      </defs>
      {intervalTicks.length > 1 && (
        <g
          className="pointer-events-none transition-opacity"
          opacity={showGhost ? 1 : 0}
        >
          {startChecks.length > 0 && (
            <>
              <rect
                x={x(intervalTicks[0])}
                y={axisY - 1}
                width={x(startChecks[startChecks.length - 1]) - x(intervalTicks[0])}
                height={2}
                rx={1}
                className="fill-amber-300 dark:fill-amber-800"
              />
              <rect
                x={x(startChecks[startChecks.length - 1])}
                y={axisY - 1}
                width={x(firstCheck) - x(startChecks[startChecks.length - 1])}
                height={2}
                rx={1}
                fill="url(#hc-grad-amber-light)"
                className="dark:hidden"
              />
              <rect
                x={x(startChecks[startChecks.length - 1])}
                y={axisY - 1}
                width={x(firstCheck) - x(startChecks[startChecks.length - 1])}
                height={2}
                rx={1}
                fill="url(#hc-grad-amber-dark)"
                className="hidden dark:block"
              />
            </>
          )}
          <rect
            x={x(firstCheck)}
            y={axisY - 1}
            width={x(axisDuration) - x(firstCheck)}
            height={2}
            rx={1}
            className="fill-zinc-200 dark:fill-zinc-700"
          />
        </g>
      )}
      {regularChecks.length > 0 && (
        <g
          className="pointer-events-none transition-opacity"
          opacity={showFailed ? 1 : 0}
        >
          <rect
            x={x(firstCheck)}
            y={axisY - 1}
            width={x(regularChecks[0]) - x(firstCheck)}
            height={2}
            rx={1}
            fill="url(#hc-grad-red-light)"
            className="dark:hidden"
          />
          <rect
            x={x(firstCheck)}
            y={axisY - 1}
            width={x(regularChecks[0]) - x(firstCheck)}
            height={2}
            rx={1}
            fill="url(#hc-grad-red-dark)"
            className="hidden dark:block"
          />
          <rect
            x={x(regularChecks[0])}
            y={axisY - 1}
            width={x(regularChecks[regularChecks.length - 1]) - x(regularChecks[0])}
            height={2}
            rx={1}
            className="fill-red-400 dark:fill-red-900"
          />
        </g>
      )}

      {/* Start period checks (forgiven) */}
      {startChecks.map((t) => (
        <Tooltip key={`s${t}`}>
          <TooltipTrigger
            render={
              <circle
                cx={x(t)}
                cy={axisY}
                r={2.5}
                className="fill-amber-400 outline-none transition-transform hover:scale-150 focus-visible:scale-150 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring/50 dark:fill-amber-500"
                style={{ transformOrigin: `${x(t)}px ${axisY}px` }}
                tabIndex={0}
                stroke="transparent"
                strokeWidth={6}
                onMouseEnter={() => setHoverGroup("ghost")}
                onMouseLeave={() => setHoverGroup(null)}
                onFocus={() => setHoverGroup("ghost")}
                onBlur={() => setHoverGroup(null)}
              />
            }
          />
          <TooltipContent>
            <p className="font-medium">Start period check</p>
            <p>Failure forgiven during grace period</p>
            <p className="text-muted-foreground">{formatDuration(t, true)}</p>
          </TooltipContent>
        </Tooltip>
      ))}

      {/* Interval tick dots (skip start-period checks — already rendered as amber) */}
      {intervalTicks
        .filter((t) => t >= startPeriod)
        .map((t, i) => {
          const isFirst = i === 0;

          return (
            <Tooltip key={`g${t}`}>
              <TooltipTrigger
                render={
                  <circle
                    cx={x(t)}
                    cy={axisY}
                    r={2}
                    className="fill-zinc-300 outline-none transition-transform hover:scale-150 focus-visible:scale-150 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring/50 dark:fill-zinc-600"
                    style={{ transformOrigin: `${x(t)}px ${axisY}px` }}
                    tabIndex={0}
                    stroke="transparent"
                    strokeWidth={6}
                    onMouseEnter={() => setHoverGroup("ghost")}
                    onMouseLeave={() => setHoverGroup(null)}
                    onFocus={() => setHoverGroup("ghost")}
                    onBlur={() => setHoverGroup(null)}
                  />
                }
              />
              <TooltipContent>
                <p className="font-medium">{isFirst ? "Initial check" : "Interval tick"}</p>
                <p>
                  {isFirst
                    ? startPeriod > 0
                      ? "First check after start period"
                      : "First healthcheck"
                    : "Healthcheck executed"}
                </p>
                <p className="text-muted-foreground">{formatDuration(t, true)}</p>
              </TooltipContent>
            </Tooltip>
          );
        })}

      {/* Failure check dots */}
      {regularChecks.map((t, i) => {
        const failureNumber = i + 1;
        const isLast = failureNumber === retries;

        return (
          <Tooltip key={`r${t}`}>
            <TooltipTrigger render={<g />}>
              <circle
                cx={x(t)}
                cy={axisY}
                r={isLast ? 3.5 : 2.5}
                className="fill-red-600 outline-none transition-transform hover:scale-150 focus-visible:scale-150 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring/50 dark:fill-red-500"
                style={{ transformOrigin: `${x(t)}px ${axisY}px` }}
                tabIndex={0}
                stroke="transparent"
                strokeWidth={6}
                onMouseEnter={() => setHoverGroup("failed")}
                onMouseLeave={() => setHoverGroup(null)}
                onFocus={() => setHoverGroup("failed")}
                onBlur={() => setHoverGroup(null)}
              />
              <text
                x={x(t)}
                y={axisY - 5}
                textAnchor="middle"
                className="fill-muted-foreground"
                style={labelStyle}
              >
                {failureNumber}
              </text>
            </TooltipTrigger>
            <TooltipContent>
              <p className="font-medium">
                Retry {failureNumber}/{retries}
              </p>
              <p>{isLast ? "Container marked unhealthy" : "Failure counts toward retry limit"}</p>
              <p className="text-muted-foreground">{formatDuration(t, true)}</p>
            </TooltipContent>
          </Tooltip>
        );
      })}

      {/* Hover label pills — on top of dots */}
      {intervalTicks.length > 1 && (
        <g
          className="pointer-events-none transition-opacity"
          opacity={showGhost ? 1 : 0}
        >
          {intervalTicks[0] > 0 && (() => {
            const midX = (x(0) + x(intervalTicks[0])) / 2;
            const inStartPeriod = intervalTicks[0] <= startPeriod;

            return (
              <g>
                <rect
                  x={midX - 11}
                  y={axisY - 4}
                  width={22}
                  height={8}
                  rx={2.5}
                  className={
                    inStartPeriod
                      ? "fill-amber-300 dark:fill-amber-800"
                      : "fill-zinc-300 dark:fill-zinc-600"
                  }
                />
                <text
                  x={midX}
                  y={axisY}
                  textAnchor="middle"
                  dominantBaseline="central"
                  style={{ ...labelStyle, fontWeight: 500, fill: "var(--background)" }}
                >
                  +{Math.round(intervalTicks[0] / 1e9)}s
                </text>
              </g>
            );
          })()}
          {intervalTicks.map((t, i) => {
            if (i >= intervalTicks.length - 1) {
              return null;
            }

            const midX = (x(t) + x(intervalTicks[i + 1])) / 2;
            const inStartPeriod = t < startPeriod;

            return (
              <g key={`go${t}`}>
                <rect
                  x={midX - 11}
                  y={axisY - 4}
                  width={22}
                  height={8}
                  rx={2.5}
                  className={
                    inStartPeriod
                      ? "fill-amber-300 dark:fill-amber-800"
                      : "fill-zinc-300 dark:fill-zinc-600"
                  }
                />
                <text
                  x={midX}
                  y={axisY}
                  textAnchor="middle"
                  dominantBaseline="central"
                  style={{ ...labelStyle, fontWeight: 500, fill: "var(--background)" }}
                >
                  +{Math.round((intervalTicks[i + 1] - t) / 1e9)}s
                </text>
              </g>
            );
          })}
        </g>
      )}
      {regularChecks.length > 0 && (
        <g
          className="pointer-events-none transition-opacity"
          opacity={showFailed ? 1 : 0}
        >
          {[firstCheck, ...regularChecks].map((t, i, all) => {
            if (i >= all.length - 1) {
              return null;
            }

            const midX = (x(t) + x(all[i + 1])) / 2;

            return (
              <g key={`fo${t}`}>
                <rect
                  x={midX - 11}
                  y={axisY - 4}
                  width={22}
                  height={8}
                  rx={2.5}
                  className="fill-red-300 dark:fill-red-800"
                />
                <text
                  x={midX}
                  y={axisY}
                  textAnchor="middle"
                  dominantBaseline="central"
                  style={{ ...labelStyle, fontWeight: 500, fill: "var(--background)" }}
                >
                  +{Math.round(timeout / 1e9)}s
                </text>
              </g>
            );
          })}
        </g>
      )}

      {/* Time labels */}
      <text
        x={x(0)}
        y={axisY + 16}
        className="fill-muted-foreground"
        style={labelStyle}
      >
        0s
      </text>

      {startPeriod > 0 && (
        <text
          x={x(startPeriod)}
          y={axisY + 16}
          textAnchor="middle"
          className="fill-muted-foreground"
          style={labelStyle}
        >
          {formatDuration(startPeriod)}
        </text>
      )}

      <text
        x={x(totalDuration)}
        y={axisY + 16}
        textAnchor="middle"
        className="fill-muted-foreground"
        style={labelStyle}
      >
        {formatDuration(totalDuration)}
      </text>
    </svg>
    </div>
  );
}

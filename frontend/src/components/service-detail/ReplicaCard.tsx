import {useState} from "react";
import {api} from "../../api/client";
import type {Service, Task} from "../../api/types";
import InfoCard from "../InfoCard";
import {Spinner} from "../Spinner";

function ReplicaDoughnut({running, desired}: { running: number; desired: number }) {
  const size = 50;
  const stroke = 5;
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const ratio = desired > 0 ? Math.min(running / desired, 1) : 0;
  const offset = circumference * (1 - ratio);
  const healthy = running >= desired;

  if (healthy) {
    return (
      <svg
        width={size}
        height={size}
        viewBox={`0 0 ${size} ${size}`}
      >
        <circle
          cx={size / 2}
          cy={size / 2}
          r={size / 2}
          className="fill-green-500"
        />
        <path
          d="M15 25.5 L21.5 32 L35 19"
          fill="none"
          stroke="white"
          strokeWidth={3}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    );
  }

  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
    >
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth={stroke}
        className="text-muted"
      />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth={stroke}
        strokeDasharray={circumference}
        strokeDashoffset={offset}
        strokeLinecap="round"
        transform={`rotate(-90 ${size / 2} ${size / 2})`}
        className="text-red-500"
      />
    </svg>
  );
}

export function ReplicaCard({service, tasks}: { service: Service; tasks: Task[] }) {
  const [scaleOpen, setScaleOpen] = useState(false);
  const [scaleValue, setScaleValue] = useState("");
  const [scaleLoading, setScaleLoading] = useState(false);
  const [scaleError, setScaleError] = useState<string | null>(null);

  const replicated = service.Spec.Mode.Replicated;
  if (!replicated) {
    return <InfoCard label="Mode" value="global"/>;
  }

  const desired = replicated.Replicas ?? 0;
  const running = tasks.filter((t) => t.Status.State === "running").length;
  const healthy = running >= desired;

  function openScale() {
    setScaleValue(String(desired));
    setScaleError(null);
    setScaleOpen(true);
  }

  function cancelScale() {
    setScaleOpen(false);
    setScaleError(null);
  }

  async function submitScale() {
    const n = parseInt(scaleValue, 10);
    if (isNaN(n) || n < 0) {
      setScaleError("Enter a valid replica count");
      return;
    }
    setScaleLoading(true);
    setScaleError(null);
    try {
      await api.scaleService(service.ID, n);
      setScaleOpen(false);
    } catch (err) {
      setScaleError(err instanceof Error ? err.message : "Failed to scale");
    } finally {
      setScaleLoading(false);
    }
  }

  const value = (
    <>
      <span className="tabular-nums">
        <span className="text-2xl font-bold">{running}</span>
        <span className="text-lg font-normal text-muted-foreground">/{desired}</span>
      </span>

      {!healthy && (
        <div className="mt-1 text-xs text-red-600 dark:text-red-400">
          {desired - running} replica{desired - running !== 1 ? "s" : ""} not running
        </div>
      )}
    </>
  );

  const scaleControl = (
    <div className="relative flex items-center gap-2">
      {desired > 0 && <ReplicaDoughnut running={running} desired={desired}/>}
      <button
        type="button"
        onClick={openScale}
        className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
        title="Scale service"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
          <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
        </svg>
      </button>

      {scaleOpen && (
        <div className="absolute right-0 top-full z-50 mt-1 w-52 rounded-lg border bg-card p-3 shadow-lg">
          <p className="mb-2 text-xs font-medium text-muted-foreground">Scale replicas</p>
          <input
            type="number"
            min={0}
            value={scaleValue}
            onChange={(e) => setScaleValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                void submitScale();
              }
              if (e.key === "Escape") {
                cancelScale();
              }
            }}
            className="mb-2 w-full rounded border bg-background px-2 py-1 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
            autoFocus
          />
          {scaleError && (
            <p className="mb-2 text-xs text-red-600 dark:text-red-400">{scaleError}</p>
          )}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => void submitScale()}
              disabled={scaleLoading}
              className="flex flex-1 items-center justify-center gap-1 rounded bg-primary px-2 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
            >
              {scaleLoading && <Spinner className="size-3"/>}
              Scale
            </button>
            <button
              type="button"
              onClick={cancelScale}
              disabled={scaleLoading}
              className="flex-1 rounded border px-2 py-1 text-xs font-medium disabled:opacity-50"
            >
              Cancel
            </button>
          </div>
        </div>
      )}
    </div>
  );

  return (
    <InfoCard
      label="Replicas"
      value={value}
      right={scaleControl}
    />
  );
}

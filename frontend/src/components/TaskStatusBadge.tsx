const STATE_COLORS: Record<string, string> = {
  running: "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300",
  ready: "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300",
  failed: "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300",
  rejected: "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300",
  down: "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300",
  preparing: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900/40 dark:text-yellow-300",
  starting: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900/40 dark:text-yellow-300",
  pending: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900/40 dark:text-yellow-300",
  shutdown: "bg-gray-100 text-gray-600 dark:bg-gray-800/40 dark:text-gray-400",
  complete: "bg-gray-100 text-gray-600 dark:bg-gray-800/40 dark:text-gray-400",
};

export default function TaskStatusBadge({ state }: { state?: string }) {
  const color =
    (state && STATE_COLORS[state]) ||
    "bg-gray-100 text-gray-800 dark:bg-gray-800/40 dark:text-gray-300";
  return (
    <span className={`inline-block px-2 py-0.5 rounded text-xs font-medium ${color}`}>
      {state || "unknown"}
    </span>
  );
}

export default function TaskStatusBadge({ state }: { state?: string }) {
  return (
    <span
      data-state={state || "unknown"}
      className="inline-block rounded bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-800 data-[state=complete]:bg-gray-100 data-[state=complete]:text-gray-600 data-[state=down]:bg-red-100 data-[state=down]:text-red-800 data-[state=failed]:bg-red-100 data-[state=failed]:text-red-800 data-[state=pending]:bg-yellow-100 data-[state=pending]:text-yellow-800 data-[state=preparing]:bg-yellow-100 data-[state=preparing]:text-yellow-800 data-[state=ready]:bg-green-100 data-[state=ready]:text-green-800 data-[state=rejected]:bg-red-100 data-[state=rejected]:text-red-800 data-[state=running]:bg-green-100 data-[state=running]:text-green-800 data-[state=shutdown]:bg-gray-100 data-[state=shutdown]:text-gray-600 data-[state=starting]:bg-yellow-100 data-[state=starting]:text-yellow-800 dark:bg-gray-800/40 dark:text-gray-300 dark:data-[state=complete]:bg-gray-800/40 dark:data-[state=complete]:text-gray-400 dark:data-[state=down]:bg-red-900/40 dark:data-[state=down]:text-red-300 dark:data-[state=failed]:bg-red-900/40 dark:data-[state=failed]:text-red-300 dark:data-[state=pending]:bg-yellow-900/40 dark:data-[state=pending]:text-yellow-300 dark:data-[state=preparing]:bg-yellow-900/40 dark:data-[state=preparing]:text-yellow-300 dark:data-[state=ready]:bg-green-900/40 dark:data-[state=ready]:text-green-300 dark:data-[state=rejected]:bg-red-900/40 dark:data-[state=rejected]:text-red-300 dark:data-[state=running]:bg-green-900/40 dark:data-[state=running]:text-green-300 dark:data-[state=shutdown]:bg-gray-800/40 dark:data-[state=shutdown]:text-gray-400 dark:data-[state=starting]:bg-yellow-900/40 dark:data-[state=starting]:text-yellow-300"
    >
      {state || "unknown"}
    </span>
  );
}

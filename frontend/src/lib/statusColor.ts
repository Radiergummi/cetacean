export function statusColor(state: string): string {
  switch (state) {
    case "running":
    case "ready":
    case "complete":
      return "bg-green-500";
    case "failed":
    case "rejected":
    case "down":
    case "orphaned":
      return "bg-red-500";
    case "preparing":
    case "starting":
    case "pending":
    case "assigned":
    case "accepted":
      return "bg-yellow-500";
    case "shutdown":
    case "remove":
      return "bg-gray-300 dark:bg-gray-600";
    default:
      return "bg-gray-300 dark:bg-gray-600";
  }
}

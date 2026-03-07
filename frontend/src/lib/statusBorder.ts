export function statusBorder(state: string): string {
  switch (state) {
    case "running":
    case "ready":
    case "complete":
      return "border-l-[3px] border-l-green-500";
    case "failed":
    case "rejected":
    case "down":
    case "orphaned":
      return "border-l-[3px] border-l-red-500";
    case "preparing":
    case "starting":
    case "pending":
    case "assigned":
    case "accepted":
      return "border-l-[3px] border-l-yellow-500";
    case "shutdown":
    case "remove":
      return "border-l-[3px] border-l-gray-300 dark:border-l-gray-600";
    default:
      return "";
  }
}

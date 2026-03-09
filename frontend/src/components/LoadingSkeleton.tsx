export function LoadingPage() {
  return (
    <div className="space-y-6 animate-pulse">
      <div className="h-8 w-48 bg-muted rounded" />
      <div className="h-10 w-80 bg-muted rounded" />
      <div className="space-y-3">
        <div className="h-10 bg-muted rounded" />
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="h-12 bg-muted/60 rounded" />
        ))}
      </div>
    </div>
  );
}

export function SkeletonTable({ columns, rows = 5 }: { columns: number; rows?: number }) {
  return (
    <div className="overflow-x-auto rounded-lg border animate-pulse">
      <table className="w-full">
        <thead>
          <tr className="border-b bg-muted/50">
            {Array.from({ length: columns }).map((_, i) => (
              <th key={i} className="p-3">
                <div className="h-4 w-20 bg-muted rounded" />
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Array.from({ length: rows }).map((_, i) => (
            <tr key={i} className="border-b last:border-b-0">
              {Array.from({ length: columns }).map((_, j) => (
                <td key={j} className="p-3">
                  <div className={`h-4 bg-muted/60 rounded ${j === 0 ? "w-32" : "w-20"}`} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function LoadingDetail() {
  return (
    <div className="space-y-6 animate-pulse">
      <div>
        <div className="h-4 w-32 bg-muted rounded mb-2" />
        <div className="h-8 w-64 bg-muted rounded" />
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="rounded-lg border p-4">
            <div className="h-3 w-16 bg-muted rounded mb-2" />
            <div className="h-5 w-32 bg-muted rounded" />
          </div>
        ))}
      </div>
    </div>
  );
}

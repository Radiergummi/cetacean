export function LoadingPage() {
  return (
    <div className="animate-pulse space-y-6">
      <div className="h-8 w-48 rounded bg-muted" />
      <div className="h-10 w-80 rounded bg-muted" />
      <div className="space-y-3">
        <div className="h-10 rounded bg-muted" />
        {Array.from({ length: 5 }).map((_, index) => (
          <div
            key={index}
            className="h-12 rounded bg-muted/60"
          />
        ))}
      </div>
    </div>
  );
}

export function SkeletonTable({ columns, rows = 5 }: { columns: number; rows?: number }) {
  return (
    <div className="animate-pulse overflow-x-auto rounded-lg border">
      <table className="w-full">
        <thead>
          <tr className="border-b bg-muted/50">
            {Array.from({ length: columns }).map((_, index) => (
              <th
                key={index}
                className="p-3"
              >
                <div className="h-4 w-20 rounded bg-muted" />
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Array.from({ length: rows }).map((_, rowIndex) => (
            <tr
              key={rowIndex}
              className="border-b last:border-b-0"
            >
              {Array.from({ length: columns }).map((_, columnIndex) => (
                <td
                  key={columnIndex}
                  className="p-3"
                >
                  <div
                    data-first={columnIndex === 0 || undefined}
                    className="h-4 w-20 rounded bg-muted/60 data-first:w-32"
                  />
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
    <div className="animate-pulse space-y-6">
      <div>
        <div className="mb-2 h-4 w-32 rounded bg-muted" />
        <div className="h-8 w-64 rounded bg-muted" />
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        {Array.from({ length: 4 }).map((_, index) => (
          <div
            key={index}
            className="rounded-lg border p-4"
          >
            <div className="mb-2 h-3 w-16 rounded bg-muted" />
            <div className="h-5 w-32 rounded bg-muted" />
          </div>
        ))}
      </div>
    </div>
  );
}

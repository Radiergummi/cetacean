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

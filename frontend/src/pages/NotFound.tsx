import { Link } from "react-router-dom";

export default function NotFound() {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-24">
      <span className="font-mono text-[10rem] leading-none font-light tracking-tighter text-muted-foreground/50">
        404
      </span>

      <p className="text-lg text-muted-foreground">Page not found</p>

      <Link
        to="/"
        className="mt-16 text-sm font-medium text-link hover:underline"
      >
        Go to dashboard
      </Link>
    </div>
  );
}

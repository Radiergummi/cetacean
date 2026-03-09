import { Link } from "react-router-dom";

export default function NotFound() {
  return (
    <div className="flex flex-col items-center justify-center py-24">
      <span className="text-[10rem] leading-none font-black tracking-tighter text-muted-foreground/25">
        404
      </span>
      <p className="mt-2 text-lg text-muted-foreground">Page not found</p>
      <Link to="/" className="mt-6 text-sm font-medium text-link hover:underline">
        Go to dashboard
      </Link>
    </div>
  );
}

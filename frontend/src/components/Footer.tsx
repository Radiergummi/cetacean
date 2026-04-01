import { api, type HealthInfo } from "@/api/client";
import { apiPath } from "@/lib/basePath";
import { Book, ExternalLink, Github } from "lucide-react";
import { useEffect, useState } from "react";

function Footer() {
  const [health, setHealth] = useState<HealthInfo | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    api
      .health(controller.signal)
      .then(setHealth)
      .catch(() => {});

    return () => controller.abort();
  }, []);

  const version = health?.version ?? "dev";
  const commit = health?.commit;
  const shortCommit = commit && commit !== "unknown" ? commit.slice(0, 7) : null;

  return (
    <footer className="py-6 text-xs text-muted-foreground">
      <div className="mx-auto flex max-w-7xl flex-col items-center gap-3 px-4 sm:flex-row sm:justify-between sm:px-6 lg:px-8">
        <div className="flex items-center gap-1.5">
          <span className="font-medium">Cetacean</span>
          <span>{version}</span>
          {shortCommit && (
            <a
              href={`https://github.com/radiergummi/cetacean/commit/${commit}`}
              target="_blank"
              rel="noopener noreferrer"
              className="font-mono text-muted-foreground/70 transition hover:text-foreground"
            >
              ({shortCommit})
            </a>
          )}
        </div>

        <nav
          className="flex items-center gap-4"
          aria-label="Footer"
        >
          <a
            href="https://github.com/radiergummi/cetacean"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 transition hover:text-foreground"
          >
            <Github className="size-3.5" />
            GitHub
          </a>
          <a
            href="https://github.com/radiergummi/cetacean/tree/main/docs"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 transition hover:text-foreground"
          >
            <Book className="size-3.5" />
            Docs
          </a>
          <a
            href={apiPath("/api")}
            className="inline-flex items-center gap-1 transition hover:text-foreground"
          >
            <ExternalLink className="size-3.5" />
            API
          </a>
        </nav>
      </div>
    </footer>
  );
}

export default Footer;

import { api, type HealthInfo } from "@/api/client";
import { apiPath } from "@/lib/basePath";
import { Book, ExternalLink } from "lucide-react";
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
            <svg
              className="size-3.5"
              viewBox="0 0 24 24"
              fill="currentColor"
              aria-hidden="true"
            >
              <path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z" />
            </svg>
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

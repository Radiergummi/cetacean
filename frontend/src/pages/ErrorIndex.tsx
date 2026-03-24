import PageHeader from "../components/PageHeader";
import FetchError from "../components/FetchError";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { Link } from "react-router-dom";
import { useEffect, useState } from "react";

interface ErrorDef {
  code: string;
  title: string;
  status: number;
  description: string;
  suggestion: string;
}

export default function ErrorIndex() {
  const [errors, setErrors] = useState<ErrorDef[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch("/api/errors", { headers: { Accept: "application/json" } })
      .then((res) => {
        if (!res.ok) {
          throw new Error(`${res.status} ${res.statusText}`);
        }
        return res.json();
      })
      .then(setErrors)
      .catch((err) => setError(err.message));
  }, []);

  if (error) {
    return <FetchError message={error} />;
  }

  if (!errors) {
    return <LoadingDetail />;
  }

  const sectionTitles: Record<string, string> = {
    API: "API & Protocol",
    AUT: "Authentication",
    OPS: "Operations Level",
    FLT: "Filters",
    SEA: "Search",
    MTR: "Metrics & Prometheus",
    LOG: "Log Streaming",
    SSE: "SSE Connections",
    ENG: "Docker Engine",
    SWM: "Swarm",
    PLG: "Plugins",
    NOD: "Nodes",
    SVC: "Services",
    TSK: "Tasks",
    STK: "Stacks",
    VOL: "Volumes",
    NET: "Networks",
    CFG: "Configs",
    SEC: "Secrets",
  };

  const grouped = new Map<string, ErrorDef[]>();
  for (const err of errors) {
    const prefix = err.code.slice(0, 3);
    const list = grouped.get(prefix);

    if (list) {
      list.push(err);
    } else {
      grouped.set(prefix, [err]);
    }
  }

  return (
    <>
      <PageHeader
        title="Error Reference"
        subtitle={`${errors.length} error codes`}
      />

      {[...grouped.entries()].map(([prefix, codes]) => (
        <section
          key={prefix}
          id={prefix.toLowerCase()}
          className="mb-8"
        >
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">
            <a
              href={`#${prefix.toLowerCase()}`}
              className="hover:underline"
            >
              {sectionTitles[prefix] ?? prefix}
            </a>
          </h2>
          <div className="overflow-hidden rounded-md border">
            <table className="w-full text-sm">
              <tbody>
                {codes.map(({ code, title, status }) => (
                  <tr
                    key={code}
                    className="border-b last:border-b-0 hover:bg-muted/50"
                  >
                    <td className="w-24 px-3 py-2">
                      <Link
                        to={`/api/errors/${code}`}
                        className="font-mono text-link hover:underline"
                      >
                        {code}
                      </Link>
                    </td>
                    <td className="px-3 py-2">{title}</td>
                    <td className="w-16 px-3 py-2 text-right text-muted-foreground">{status}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ))}
    </>
  );
}

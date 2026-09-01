import { type Recommendation, type Severity, severityOrder } from "./types";

interface Props {
  items: Recommendation[];
  /** Called when a finding is picked; omit to render the list read-only. */
  onInvestigate?: ((finding: Recommendation) => void) | undefined;
}

/**
 * Severity styling. The colour is a marker beside a label, never the label
 * itself: a reader who cannot separate red from amber still reads "1 critical"
 * and "1 warning" in the group headings.
 */
const severityBar: Record<Severity, string> = {
  critical: "bg-red-500",
  warning: "bg-yellow-500",
  info: "bg-blue-400",
};

/**
 * The presentational half of the recommendations widget: findings grouped by
 * severity, most serious first, each one pickable.
 *
 * Split from the widget so it can be exercised without a host bridge — see the
 * table and topology widgets, which split the same way.
 */
export function RecommendationList({ items, onInvestigate }: Props) {
  if (items.length === 0) {
    return <p className="p-3 text-sm opacity-70">Nothing to report — no recommendations.</p>;
  }

  const groups = severityOrder
    .map((severity) => ({
      severity,
      findings: items.filter((item) => item.severity === severity),
    }))
    .filter(({ findings }) => findings.length > 0);

  return (
    <div className="flex flex-col gap-3 p-3">
      {groups.map(({ findings, severity }) => (
        <section
          key={severity}
          role="group"
          aria-label={`${severity} recommendations`}
          className="flex flex-col gap-1.5"
        >
          <h2 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
            {findings.length} {severity}
          </h2>

          {findings.map((finding) => (
            <Finding
              key={`${finding.category}:${finding.targetId}:${finding.message}`}
              finding={finding}
              onInvestigate={onInvestigate}
            />
          ))}
        </section>
      ))}
    </div>
  );
}

function Finding({
  finding,
  onInvestigate,
}: { finding: Recommendation } & Pick<Props, "onInvestigate">) {
  const body = (
    <>
      <span
        aria-hidden="true"
        className={`w-0.75 shrink-0 self-stretch rounded-full ${severityBar[finding.severity]}`}
      />

      <span className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm">{finding.message}</span>
        <span className="text-xs text-muted-foreground">
          {finding.scope} {finding.targetName} · {finding.category}
          {finding.suggested !== undefined && ` · suggested ${finding.suggested}`}
        </span>
      </span>
    </>
  );

  const className = "flex w-full items-start gap-2 rounded-md border p-2 text-left";

  if (!onInvestigate) {
    return <div className={className}>{body}</div>;
  }

  return (
    <button
      type="button"
      onClick={() => onInvestigate(finding)}
      className={`${className} hover:bg-muted`}
    >
      {body}
    </button>
  );
}

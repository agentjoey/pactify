import { formatDuration, parseQuotaWindows } from "./statistics";

export interface QuotaData {
  exhaustedRoutes: {
    key: string;
    harness: string;
    model?: string;
    resetAt: string | null;
  }[];
  quotaSnapshots?: {
    harness: string;
    jobId: string;
    observedAt: string;
    rateLimit: string;
    buckets?: {
      id: string;
      window: string;
      remainingFraction: number;
      resetAt: string;
    }[];
  }[];
}
function resetText(resetAt: string | null, now: number) {
  return resetAt && Number.isFinite(Date.parse(resetAt))
    ? `resets in ${formatDuration((Date.parse(resetAt) - now) / 1000)}`
    : "resets unknown";
}
function ResetTime({ value }: { value: string | null }) {
  return value && Number.isFinite(Date.parse(value)) ? (
    <time dateTime={value}>{new Date(value).toLocaleString("en-US")}</time>
  ) : null;
}
export function Quota({ report, now }: { report: QuotaData; now: number }) {
  const latest = new Map<string, NonNullable<QuotaData["quotaSnapshots"]>[number]>();
  for (const snapshot of report.quotaSnapshots || []) {
    const previous = latest.get(snapshot.harness);
    if (!previous || Date.parse(snapshot.observedAt) > Date.parse(previous.observedAt))
      latest.set(snapshot.harness, snapshot);
  }
  const groups = [...latest.values()]
    .map((snapshot) => ({
      harness: snapshot.harness,
      windows:
        snapshot.harness === "agy"
          ? (snapshot.buckets || [])
              .filter(
                (bucket) =>
                  Number.isFinite(bucket.remainingFraction) &&
                  bucket.remainingFraction >= 0 &&
                  bucket.remainingFraction <= 1,
              )
              .map((bucket) => ({
                label: bucket.id,
                remaining: Math.round(bucket.remainingFraction * 100),
                resetAt: bucket.resetAt,
              }))
          : snapshot.harness === "codex"
            ? parseQuotaWindows(snapshot.rateLimit, snapshot.observedAt)
            : [],
    }))
    .filter((group) => group.windows.length);
  if (!report.exhaustedRoutes.length && !groups.length)
    return <p className="panel-caption">No quota data</p>;
  return (
    <div className="quota-content">
      {!!report.exhaustedRoutes.length && (
        <div>
          <h3>Exhausted routes</h3>
          {report.exhaustedRoutes.map((route) => (
            <div className="routing-read-row" key={route.key}>
              <strong>{route.key}</strong>
              <span>{resetText(route.resetAt, now)}</span>
              <ResetTime value={route.resetAt} />
            </div>
          ))}
        </div>
      )}
      {groups.map(({ harness, windows }) => (
        <div className="quota-windows" key={harness}>
          <h3>{harness} quota snapshots</h3>
          {windows.map((window) => (
            <div className="quota-window" key={window.label}>
              <div className="quota-window-heading">
                <strong>{window.label}</strong>
                <span>
                  {window.remaining}% remaining ·{" "}
                  {resetText(window.resetAt, now)}
                </span>
              </div>
              <progress
                aria-label={`${harness} ${window.label} remaining`}
                value={window.remaining}
                max="100"
              />
              <ResetTime value={window.resetAt} />
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

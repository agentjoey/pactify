import { dailyTokens, shortNumber } from "./statistics";

const chartColor = (harness: string) => ({
  codex: "var(--chart-codex)",
  kimi: "var(--chart-kimi)",
  opencode: "var(--chart-opencode)",
}[harness] || "var(--text-3)");
export function TokenChart({ days }: { days: ReturnType<typeof dailyTokens> }) {
  const harnesses = [
    ...new Set(days.flatMap((day) => Object.keys(day.harnesses))),
  ].sort();
  const maximum = Math.max(
    1,
    ...days.map((day) =>
      Object.values(day.harnesses).reduce((a, b) => a + b, 0),
    ),
  );
  const rawStep = maximum / 4;
  const magnitude = 10 ** Math.floor(Math.log10(rawStep));
  const step = Math.max(
    1,
    ([1, 2, 5, 10].find((n) => n * magnitude >= rawStep) || 10) * magnitude,
  );
  const domain = step * 4;
  return (
    <>
      <div className="token-plot">
        <div className="chart-y-axis" aria-hidden="true">
          {[0, 1, 2, 3, 4].map((i) => (
            <span
              className="chart-label"
              key={i}
              style={{ bottom: `${i * 25}%` }}
            >
              {shortNumber(i * step)}
            </span>
          ))}
        </div>
        <svg
          viewBox="0 0 840 200"
          preserveAspectRatio="none"
          role="img"
          aria-label="Token usage · last 14 days"
        >
          {[0, 1, 2, 3].map((i) => (
            <line
              className="chart-gridline"
              key={i}
              x1="0"
              x2="840"
              y1={i * 50}
              y2={i * 50}
              vectorEffect="non-scaling-stroke"
            />
          ))}
          <line
            className="chart-axis"
            x1="0"
            x2="840"
            y1="200"
            y2="200"
            vectorEffect="non-scaling-stroke"
          />
          {days.map((day, i) => {
            let offset = 0;
            return (
              <g key={day.day}>
                {harnesses.map((h) => {
                  const value = day.harnesses[h] || 0;
                  const height = (value / domain) * 200;
                  offset += height;
                  return value ? (
                    <rect
                      key={h}
                      x={i * 60 + 12}
                      y={200 - offset}
                      width="36"
                      height={height}
                      fill={chartColor(h)}
                      tabIndex={0}
                      aria-label={`${day.day} · ${h}: ${value.toLocaleString("en-US")} tokens`}
                    >
                      <title>
                        {day.day} · {h}: {value.toLocaleString("en-US")} tokens
                      </title>
                    </rect>
                  ) : null;
                })}
              </g>
            );
          })}
        </svg>
        <div className="chart-x-axis" aria-hidden="true">
          {days.map((day) => (
            <span key={day.day} className="chart-label">
              {day.day.slice(5)}
            </span>
          ))}
        </div>
      </div>
      <div className="chart-legend">
        {harnesses.map((h) => (
          <span key={h}>
            <i style={{ background: chartColor(h) }} />
            {h}
          </span>
        ))}
        {!harnesses.length && <span>No token usage recorded</span>}
      </div>
    </>
  );
}

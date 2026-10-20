import { useCallback, useEffect, useState } from "react";
import type { JobsSource } from "./source";
import type { Job } from "./types";
export const validJobs = (value: unknown): value is Job[] =>
  Array.isArray(value) &&
  value.every(
    (row) =>
      row &&
      typeof row.id === "string" &&
      typeof row.title === "string" &&
      typeof row.status === "string" &&
      Array.isArray(row.attention) &&
      Array.isArray(row.resumedBy),
  );

export function useResource<T>(
  source: JobsSource,
  path: string | undefined,
  interval = 30000,
  validate?: (value: unknown) => boolean,
) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const [updated, setUpdated] = useState<Date | null>(null);
  const [revision, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision((n) => n + 1), []);
  useEffect(() => {
    if (!path) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    let ended = false;
    let loading = false;
    let delay = interval;
    async function load() {
      if (document.hidden || ended || loading || controller.signal.aborted) return;
      loading = true;
      try {
        const response = await source.fetch(path!, controller.signal);
        const value: T = await response.json();
        if (validate && !validate(value))
          throw new Error("Invalid response. Retrying shortly.");
        if (!controller.signal.aborted) {
          delay = interval;
          setData(value);
          setError("");
          setUpdated(new Date());
        }
      } catch (e) {
        delay = Math.min(delay * 2, 30000);
        ended = (e as { status?: number }).status === 401;
        if (!controller.signal.aborted) setError((e as Error).message);
      } finally {
        loading = false;
        if (!controller.signal.aborted && !ended && !document.hidden) timer = setTimeout(load, delay);
      }
    }
    const visibility = () => {
      clearTimeout(timer);
      if (!document.hidden) void load();
    };
    document.addEventListener("visibilitychange", visibility);
    void load();
    return () => {
      document.removeEventListener("visibilitychange", visibility);
      controller.abort();
      clearTimeout(timer);
    };
  }, [source, path, interval, revision, validate]);
  return { data, error, updated, refresh };
}
export async function responseError(response: Response) {
  const body = await response.text();
  try {
    return JSON.parse(body).error || `Request failed (${response.status})`;
  } catch {
    return body || `Request failed (${response.status})`;
  }
}
export function useHash() {
  const [hash, setHash] = useState(location.hash);
  useEffect(() => {
    const update = () => setHash(location.hash);
    window.addEventListener("hashchange", update);
    return () => window.removeEventListener("hashchange", update);
  }, []);
  return hash;
}
export function jobsLink(id = "", tab = "") {
  const query = location.hash.split("?")[1] || "";
  const params = new URLSearchParams(query);
  params.delete("tab");
  if (tab) params.set("tab", tab);
  return `#/jobs${id ? "/" + encodeURIComponent(id) : ""}${params.size ? "?" + params : ""}`;
}

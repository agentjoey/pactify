import { expect, it, vi } from "vitest";
import { liveStream } from "./liveStreams";
import type { JobsSource } from "./source";

it("shares four slots, cancels queued jobs and releases readers before reusing slots", async () => {
  const canceled: string[] = [];
  const fetch = vi.fn(
    async (path: string) =>
      new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(
              new TextEncoder().encode(
                'data: {"kind":"state","state":"turn_started"}\n\n',
              ),
            );
          },
          cancel() {
            canceled.push(path);
          },
        }),
      ),
  );
  const source: JobsSource = { jobsPath: "/jobs", costPath: "/cost", fetch };
  const event = vi.fn(),
    status = vi.fn();
  const stops = Array.from({ length: 6 }, (_, i) =>
    liveStream(source, String(i), event, status),
  );
  await vi.waitFor(() => expect(event).toHaveBeenCalledTimes(4));
  expect(fetch).toHaveBeenCalledTimes(4);
  stops[4](); // A queued job must never open later.
  stops[0]();
  await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(5));
  expect(canceled).toContain("/jobs/0/events");
  expect(fetch.mock.calls.map(([path]) => path)).not.toContain(
    "/jobs/4/events",
  );
  expect(fetch.mock.calls[4][0]).toBe("/jobs/5/events");
  for (const stop of stops) stop();
  await vi.waitFor(() => expect(canceled).toHaveLength(5));
  // All slots remain usable after an overlapping mount/unmount sequence.
  const stop = liveStream(source, "next", event, status);
  await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(6));
  stop();
  await vi.waitFor(() => expect(canceled).toHaveLength(6));
});

import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

// Old hosted deployments may leave this build-time setting behind.
vi.hoisted(() => vi.stubEnv("VITE_PACTIFY_RELAY_URL", "https://retired.invalid"));

import App from "./App";

describe("local dashboard startup", () => {
  it("loads the local registry and acting seat even with a retired relay setting", async () => {
    localStorage.clear();
    const fetchMock = vi.fn(async (url: string) => {
      if (url === "/api/acting-seat") return { ok: true, json: async () => ({ seat: "claude" }) };
      return { ok: true, json: async () => [] };
    });
    vi.stubGlobal("fetch", fetchMock);
    try {
      render(<App />);
      await waitFor(() => expect(screen.getByTestId("no-projects-form")).toBeInTheDocument());
      expect(fetchMock).toHaveBeenCalledWith("/api/projects");
      expect(fetchMock).toHaveBeenCalledWith("/api/acting-seat");
      expect(fetchMock.mock.calls.every(([url]) => url.startsWith("/api/"))).toBe(true);
    } finally {
      vi.unstubAllGlobals();
      vi.unstubAllEnvs();
    }
  });
});

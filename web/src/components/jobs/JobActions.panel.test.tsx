import { expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
vi.mock("../../buildTarget", () => ({ panelBuild: true }));
import { JobActions } from "./JobActions";
import type { Job } from "./types";
it("removes Dismiss from the panel build even with a writable source", () => {
  const write = vi.fn();
  const { container } = render(<JobActions job={{ id: "failed", actions: ["dismiss"] } as Job}
    source={{ jobsPath: "/jobs", costPath: "/cost", fetch: vi.fn(), write }}
    onChanged={vi.fn()} notify={vi.fn()} />);
  expect(container).toBeEmptyDOMElement();
  expect(write).not.toHaveBeenCalled();
});

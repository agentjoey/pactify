import { render, screen, cleanup } from "@testing-library/react";
import { describe, it, expect, afterEach } from "vitest";
import { Skeleton } from "./Skeleton";

afterEach(cleanup);

describe("Skeleton primitives", () => {
  it("Skeleton renders the shimmer block with the .skeleton class", () => {
    render(<Skeleton width={40} height={8} />);
    const el = screen.getByTestId("skeleton");
    expect(el.className).toContain("skeleton");
    expect(el.getAttribute("aria-hidden")).toBe("true");
  });

});

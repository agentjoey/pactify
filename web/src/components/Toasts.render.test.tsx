import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { Toasts } from "./Toasts";

afterEach(cleanup);

describe("Toasts render", () => {
  it("renders the default (review) variant with the design accent", () => {
    render(<Toasts toasts={[{ id: 1, text: "t1 ready for review" }]} />);
    const el = screen.getByTestId("toast");
    expect(el.textContent).toBe("t1 ready for review");
    expect(el.className).toContain("border-l-[var(--color-role-design)]");
  });

  it("renders the error variant with a danger left border", () => {
    render(<Toasts toasts={[{ id: 2, text: "accept failed", kind: "error" }]} />);
    const el = screen.getByTestId("toast-error");
    expect(el.textContent).toBe("accept failed");
    expect(el.className).toContain("border-l-[var(--color-danger)]");
    expect(el.className).not.toContain("border-l-[var(--color-role-design)]");
  });

  it("renders nothing when the stack is empty", () => {
    const { container } = render(<Toasts toasts={[]} />);
    expect(container.firstChild).toBeNull();
  });
});

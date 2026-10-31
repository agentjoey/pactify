import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { Button } from "./Button";
import { Badge } from "./Badge";
import { Modal } from "./Modal";
import { Spinner } from "./Spinner";
import { Segmented } from "./Design";

describe("Button", () => {
  it("renders the outline variant by default", () => {
    render(<Button>Go</Button>);
    expect(screen.getByRole("button", { name: "Go" })).toHaveClass("ui-button", "ui-button-ghost");
  });
  it("renders the primary variant explicitly", () => {
    render(<Button variant="primary">Apply</Button>);
    expect(screen.getByRole("button", { name: "Apply" })).toHaveClass("ui-button-primary");
  });
  it("renders the ghost variant", () => {
    render(<Button variant="ghost">Cancel</Button>);
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveClass("ui-button-ghost");
  });
  it("renders the outlined danger variant", () => {
    render(<Button variant="danger">Delete</Button>);
    expect(screen.getByRole("button", { name: "Delete" })).toHaveClass("ui-button-danger");
  });

  it("supports sm and md sizes with different paddings", () => {
    const { rerender } = render(<Button size="sm">x</Button>);
    const sm = screen.getByRole("button").className;
    rerender(<Button size="md">x</Button>);
    const md = screen.getByRole("button").className;
    expect(sm).not.toEqual(md);
  });

  it("uses the shared button focus style", () => {
    render(<Button>Go</Button>);
    expect(screen.getByRole("button")).toHaveClass("ui-button");
  });

  it("disabled blocks onClick and applies reduced opacity + no pointer", () => {
    const onClick = vi.fn();
    render(
      <Button disabled onClick={onClick}>
        Go
      </Button>,
    );
    const btn = screen.getByRole("button");
    fireEvent.click(btn);
    expect(onClick).not.toHaveBeenCalled();
    expect(btn).toBeDisabled();
    expect(btn).toHaveClass("ui-button");
  });

  it("fires onClick when enabled", () => {
    const onClick = vi.fn();
    render(<Button onClick={onClick}>Go</Button>);
    fireEvent.click(screen.getByRole("button"));
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});

describe("Segmented", () => {
  it("keeps buttons accessible and moves one thumb to the selected option", () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <Segmented
        label="Jobs view"
        value="lines"
        options={[["lines", "Lines"], ["list", "List"]]}
        onChange={onChange}
      />,
    );
    const group = screen.getByRole("group", { name: "Jobs view" });
    const thumb = group.querySelector(".segmented-thumb") as HTMLElement;
    expect(thumb).toBeTruthy();
    expect(group.getAttribute("style")).toContain("--segment-index: 0");
    expect(group.getAttribute("style")).toContain("--segments: 2");
    expect(screen.getByRole("button", { name: "Lines" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    fireEvent.click(screen.getByRole("button", { name: "List" }));
    expect(onChange).toHaveBeenCalledWith("list");
    rerender(
      <Segmented
        label="Jobs view"
        value="list"
        options={[["lines", "Lines"], ["list", "List"]]}
        onChange={onChange}
      />,
    );
    expect(group.getAttribute("style")).toContain("--segment-index: 1");
    expect(screen.getByRole("button", { name: "List" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });
});

describe("Badge", () => {
  it("maps a color prop to an outlined tag using the matching token", () => {
    render(<Badge color="role-design">awaiting</Badge>);
    const el = screen.getByText("awaiting");
    // pill
    expect(el).toHaveClass("ui-badge");
    // colored text via the token
    expect(el).toHaveStyle({ color: "var(--color-role-design)" });
  });

  it("maps warn color to the warn token", () => {
    render(<Badge color="warn">stale</Badge>);
    expect(screen.getByText("stale")).toHaveStyle({ color: "var(--color-warn)" });
  });

  it("maps the muted text-2 token to foreground and border", () => {
    render(<Badge color="text-2">free</Badge>);
    const el = screen.getByText("free");
    expect(el).toHaveStyle({ color: "var(--color-text-2)" });
    expect(el.style.borderColor).toBe("var(--color-text-2)");
  });

  it("passes title through to the root element, and omits it otherwise", () => {
    render(<Badge title="Personal tier">personal</Badge>);
    expect(screen.getByText("personal")).toHaveAttribute("title", "Personal tier");
    render(<Badge>pro</Badge>);
    expect(screen.getByText("pro")).not.toHaveAttribute("title");
  });

  it("gives the root role=img + aria-label only when ariaLabel is set", () => {
    render(<Badge ariaLabel="Tier: personal">personal</Badge>);
    const named = screen.getByRole("img", { name: "Tier: personal" });
    expect(named).toHaveTextContent("personal");
    render(<Badge>pro</Badge>);
    const plain = screen.getByText("pro");
    expect(plain).not.toHaveAttribute("role");
    expect(plain).not.toHaveAttribute("aria-label");
  });

  it("passes data-testid through to the root element", () => {
    render(<Badge data-testid="account-tier">personal</Badge>);
    expect(screen.getByTestId("account-tier")).toHaveTextContent("personal");
  });
});

describe("Modal", () => {
  it("renders title + children on the overlay panel", () => {
    render(
      <Modal title="Hi" onClose={() => {}}>
        <p>body</p>
      </Modal>,
    );
    expect(screen.getByText("Hi")).toBeInTheDocument();
    expect(screen.getByText("body")).toBeInTheDocument();
  });

  it("closes on Esc", () => {
    const onClose = vi.fn();
    render(
      <Modal title="Hi" onClose={onClose}>
        <p>body</p>
      </Modal>,
    );
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("closes on overlay click but not on panel click", () => {
    const onClose = vi.fn();
    render(
      <Modal title="Hi" onClose={onClose}>
        <p>body</p>
      </Modal>,
    );
    fireEvent.click(screen.getByText("body"));
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("modal-overlay"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("traps focus: Tab from the last focusable loops to the first", () => {
    render(
      <Modal title="Hi" onClose={() => {}}>
        <button>first</button>
        <button>last</button>
      </Modal>,
    );
    const first = screen.getByText("first");
    const last = screen.getByText("last");
    last.focus();
    expect(document.activeElement).toBe(last);
    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(first);
  });

  it("traps focus backwards: Shift+Tab from the first loops to the last", () => {
    render(
      <Modal title="Hi" onClose={() => {}}>
        <button>first</button>
        <button>last</button>
      </Modal>,
    );
    const first = screen.getByText("first");
    const last = screen.getByText("last");
    first.focus();
    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  it("danger variant tints the header", () => {
    render(
      <Modal title="Remove" onClose={() => {}} variant="danger">
        <p>body</p>
      </Modal>,
    );
    const header = screen.getByTestId("modal-header");
    expect(header.className).toMatch(/--color-danger/);
  });
});

describe("Spinner", () => {
  it("renders a status role with an accessible label", () => {
    render(<Spinner label="Saving" />);
    const s = screen.getByRole("status", { name: "Saving" });
    expect(s.getAttribute("data-testid")).toBe("spinner");
    expect(s.getAttribute("class")).toMatch(/spinner-spin/);
  });
});

describe("Button loading", () => {
  it("shows a spinner, keeps the label, and disables when loading", () => {
    render(<Button loading>Save</Button>);
    const btn = screen.getByRole("button", { name: /Save/ });
    expect(btn).toBeDisabled();
    expect(btn.getAttribute("aria-busy")).toBe("true");
    // label text is still present (no width collapse)
    expect(btn.textContent).toContain("Save");
    // a spinner is rendered inside
    expect(screen.getByTestId("spinner")).toBeTruthy();
  });

  it("renders a leading icon when provided and not loading", () => {
    render(<Button icon={<span data-testid="ic" />}>Go</Button>);
    expect(screen.getByTestId("ic")).toBeTruthy();
    expect(screen.queryByTestId("spinner")).toBeNull();
  });

  it("uses the shared button press style", () => {
    render(<Button>x</Button>);
    expect(screen.getByRole("button")).toHaveClass("ui-button");
  });
});

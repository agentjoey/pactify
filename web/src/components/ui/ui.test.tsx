import { render, screen, fireEvent, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { Button } from "./Button";
import { Input } from "./Input";
import { Select } from "./Select";
import { Badge } from "./Badge";
import { Kbd } from "./Kbd";
import { Modal } from "./Modal";
import { Popover } from "./Popover";
import { Tooltip } from "./Tooltip";
import { EmptyState } from "./EmptyState";

describe("Button", () => {
  it("renders the primary variant by default with role-design bg + dark text", () => {
    render(<Button>Go</Button>);
    const btn = screen.getByRole("button", { name: "Go" });
    // primary uses the role-design token background and dark text
    expect(btn.className).toMatch(/bg-\[var\(--color-role-design\)\]/);
    expect(btn.className).toMatch(/text-\[#191b26\]/i);
  });

  it("renders the ghost variant (transparent, subtle border, text-2)", () => {
    render(<Button variant="ghost">Cancel</Button>);
    const btn = screen.getByRole("button", { name: "Cancel" });
    expect(btn.className).toMatch(/bg-transparent/);
    expect(btn.className).toMatch(/border-\[var\(--color-border-subtle\)\]/);
    expect(btn.className).toMatch(/text-\[var\(--color-text-2\)\]/);
  });

  it("renders the danger variant (danger-tinted)", () => {
    render(<Button variant="danger">Delete</Button>);
    const btn = screen.getByRole("button", { name: "Delete" });
    expect(btn.className).toMatch(/--color-danger/);
  });

  it("supports sm and md sizes with different paddings", () => {
    const { rerender } = render(<Button size="sm">x</Button>);
    const sm = screen.getByRole("button").className;
    rerender(<Button size="md">x</Button>);
    const md = screen.getByRole("button").className;
    expect(sm).not.toEqual(md);
  });

  it("carries a focus-visible ring class", () => {
    render(<Button>Go</Button>);
    expect(screen.getByRole("button").className).toMatch(/focus-visible:ring/);
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
    expect(btn.className).toMatch(/disabled:opacity-40/);
    expect(btn.className).toMatch(/disabled:pointer-events-none/);
  });

  it("fires onClick when enabled", () => {
    const onClick = vi.fn();
    render(<Button onClick={onClick}>Go</Button>);
    fireEvent.click(screen.getByRole("button"));
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});

describe("Input", () => {
  it("renders surface bg + subtle border that goes strong on focus", () => {
    render(<Input aria-label="x" />);
    const el = screen.getByLabelText("x");
    expect(el.className).toMatch(/bg-\[var\(--color-bg-surface\)\]/);
    expect(el.className).toMatch(/border-\[var\(--color-border-subtle\)\]/);
    expect(el.className).toMatch(/focus:border-\[var\(--color-border-strong\)\]/);
    expect(el.className).toMatch(/text-\[var\(--color-text-1\)\]/);
  });

  it("forwards value/onChange", () => {
    const onChange = vi.fn();
    render(<Input aria-label="x" value="hi" onChange={onChange} />);
    fireEvent.change(screen.getByLabelText("x"), { target: { value: "ho" } });
    expect(onChange).toHaveBeenCalled();
  });
});

describe("Select", () => {
  it("renders surface bg + subtle→strong focus border", () => {
    render(
      <Select aria-label="s">
        <option value="a">a</option>
      </Select>,
    );
    const el = screen.getByLabelText("s");
    expect(el.className).toMatch(/bg-\[var\(--color-bg-surface\)\]/);
    expect(el.className).toMatch(/focus:border-\[var\(--color-border-strong\)\]/);
  });
});

describe("Badge", () => {
  it("maps a color prop to a 15%-alpha bg + colored text using the matching token", () => {
    render(<Badge color="role-design">awaiting</Badge>);
    const el = screen.getByText("awaiting");
    // pill
    expect(el.className).toMatch(/rounded-full/);
    // colored text via the token
    expect(el).toHaveStyle({ color: "var(--color-role-design)" });
  });

  it("maps warn color to the warn token", () => {
    render(<Badge color="warn">stale</Badge>);
    expect(screen.getByText("stale")).toHaveStyle({ color: "var(--color-warn)" });
  });
});

describe("Kbd", () => {
  it("renders a mono key chip", () => {
    render(<Kbd>1</Kbd>);
    const el = screen.getByText("1");
    expect(el.tagName).toBe("KBD");
    expect(el.className).toMatch(/font-mono/);
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

describe("Popover", () => {
  it("renders the panel when open and calls onClose on outside click", () => {
    const onClose = vi.fn();
    render(
      <div>
        <Popover open onClose={onClose} anchor={<button>anchor</button>}>
          <div>menu</div>
        </Popover>
        <button>outside</button>
      </div>,
    );
    expect(screen.getByText("menu")).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByText("outside"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("does not call onClose on a click inside the panel", () => {
    const onClose = vi.fn();
    render(
      <Popover open onClose={onClose} anchor={<button>anchor</button>}>
        <div>menu</div>
      </Popover>,
    );
    fireEvent.mouseDown(screen.getByText("menu"));
    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes on Esc", () => {
    const onClose = vi.fn();
    render(
      <Popover open onClose={onClose} anchor={<button>anchor</button>}>
        <div>menu</div>
      </Popover>,
    );
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("renders nothing when closed", () => {
    render(
      <Popover open={false} onClose={() => {}} anchor={<button>anchor</button>}>
        <div>menu</div>
      </Popover>,
    );
    expect(screen.queryByText("menu")).toBeNull();
  });
});

describe("Tooltip", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("shows the tip only after the 400ms hover delay", () => {
    render(
      <Tooltip label="explain">
        <button>hover me</button>
      </Tooltip>,
    );
    const trigger = screen.getByText("hover me");
    fireEvent.mouseEnter(trigger);
    // not yet
    expect(screen.queryByText("explain")).toBeNull();
    act(() => vi.advanceTimersByTime(399));
    expect(screen.queryByText("explain")).toBeNull();
    act(() => vi.advanceTimersByTime(1));
    expect(screen.getByText("explain")).toBeInTheDocument();
  });

  it("cancels the pending show when the pointer leaves before the delay", () => {
    render(
      <Tooltip label="explain">
        <button>hover me</button>
      </Tooltip>,
    );
    const trigger = screen.getByText("hover me");
    fireEvent.mouseEnter(trigger);
    act(() => vi.advanceTimersByTime(200));
    fireEvent.mouseLeave(trigger);
    act(() => vi.advanceTimersByTime(400));
    expect(screen.queryByText("explain")).toBeNull();
  });

  it("shows on focus after the delay too", () => {
    render(
      <Tooltip label="explain">
        <button>hover me</button>
      </Tooltip>,
    );
    fireEvent.focus(screen.getByText("hover me"));
    act(() => vi.advanceTimersByTime(400));
    expect(screen.getByText("explain")).toBeInTheDocument();
  });
});

describe("EmptyState", () => {
  it("renders an icon slot, title and hint inside a dashed container", () => {
    render(<EmptyState icon={<span>icon</span>} title="Nothing" hint="add one" />);
    expect(screen.getByText("icon")).toBeInTheDocument();
    expect(screen.getByText("Nothing")).toBeInTheDocument();
    const hint = screen.getByText("add one");
    expect(hint.className).toMatch(/text-\[var\(--color-text-3\)\]/);
    const root = screen.getByTestId("empty-state");
    expect(root.className).toMatch(/border-dashed/);
  });
});

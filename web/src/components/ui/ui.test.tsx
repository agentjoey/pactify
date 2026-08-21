import { render, screen, fireEvent, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { Button } from "./Button";
import { Input } from "./Input";
import { Select } from "./Select";
import { Badge } from "./Badge";
import { TierBadge } from "./TierBadge";
import { Kbd } from "./Kbd";
import { Modal } from "./Modal";
import { Popover } from "./Popover";
import { Tooltip } from "./Tooltip";
import { EmptyState } from "./EmptyState";
import { Spinner } from "./Spinner";
import { Alert } from "./Alert";
import { StatusPill } from "./StatusPill";

describe("Button", () => {
  it("renders the primary variant by default with role-design bg + dark text", () => {
    render(<Button>Go</Button>);
    const btn = screen.getByRole("button", { name: "Go" });
    // primary uses the role-design token background and dark text
    expect(btn.className).toMatch(/bg-\[var\(--color-role-design\)\]/);
    expect(btn.className).toMatch(/text-\[var\(--color-bg-page\)\]/);
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

  it("maps the muted text-2 token to foreground + 15%-alpha background", () => {
    render(<Badge color="text-2">free</Badge>);
    const el = screen.getByText("free");
    expect(el).toHaveStyle({ color: "var(--color-text-2)" });
    expect(el).toHaveStyle({
      background: "color-mix(in srgb, var(--color-text-2) 15%, transparent)",
    });
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

describe("TierBadge", () => {
  it("names a tier_raw badge via role=img + aria-label (not mouse-only), a plain badge stays anonymous", () => {
    render(
      <>
        <TierBadge tier="L1" tierRaw="L9" />
        <TierBadge tier="L2" />
      </>,
    );
    // The raw-value hint is the accessible name, shared with the title.
    const raw = screen.getByRole("img", { name: 'spec 写的是 "L9"，无法识别 —— 引擎将按 L1 运行' });
    expect(raw).toHaveTextContent("L1");
    expect(raw).toHaveAttribute("title", 'spec 写的是 "L9"，无法识别 —— 引擎将按 L1 运行');
    // No conflict and no tier_raw → no role/aria-label (regression pin).
    const plain = screen.getByText("L2");
    expect(plain).not.toHaveAttribute("role");
    expect(plain).not.toHaveAttribute("aria-label");
  });

  it("conflict wins over tier_raw for both title and accessible name", () => {
    render(<TierBadge tier="L1" conflict="manifest says L3" tierRaw="L9" />);
    const badge = screen.getByRole("img", { name: "manifest says L3" });
    expect(badge).toHaveAttribute("title", "manifest says L3");
  });
});

describe("Kbd", () => {
  it("renders a mono key chip", () => {
    render(<Kbd>1</Kbd>);
    const el = screen.getByText("1");
    expect(el.tagName).toBe("KBD");
    expect(el.className).toMatch(/font-mono/);
  });

  it("uses a raised surface with shadow", () => {
    render(<Kbd>1</Kbd>);
    const el = screen.getByText("1");
    expect(el.className).toMatch(/bg-\[var\(--color-bg-raised\)\]/);
    expect(el.className).toMatch(/shadow-\[var\(--shadow-raised\)\]/);
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

  it("has an active-press transform class for tactile feedback", () => {
    render(<Button>x</Button>);
    expect(screen.getByRole("button").className).toMatch(/active:scale-\[0\.97\]/);
  });
});

describe("Alert", () => {
  it("renders the tone, title and body with role=alert", () => {
    render(<Alert tone="danger" title="Load failed">network error</Alert>);
    const a = screen.getByRole("alert");
    expect(a.getAttribute("data-tone")).toBe("danger");
    expect(a.textContent).toContain("Load failed");
    expect(a.textContent).toContain("network error");
  });

  it("renders a retry action that fires onRetry", () => {
    const onRetry = vi.fn();
    render(<Alert tone="warn" onRetry={onRetry} retryLabel="Try again">x</Alert>);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it("omits the retry button when no onRetry is given", () => {
    render(<Alert>just info</Alert>);
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("uses a 28% alpha tone border", () => {
    render(<Alert tone="danger" title="Load failed">network error</Alert>);
    const a = screen.getByRole("alert");
    expect(a.style.borderColor).toMatch(/28%/);
  });
});

describe("StatusPill", () => {
  it("renders the pact-state label + status attr, with a live dot for in-flight states", () => {
    render(<StatusPill status="in_progress" />);
    const p = screen.getByTestId("status-pill");
    expect(p.getAttribute("data-status")).toBe("in_progress");
    expect(p.textContent).toContain("working");
    expect(p.querySelector(".status-pill-dot-live")).toBeTruthy();
  });

  it("renders shipped as a filled pill (no live dot)", () => {
    render(<StatusPill status="shipped" />);
    const p = screen.getByTestId("status-pill");
    expect(p.textContent).toContain("shipped");
    expect(p.querySelector(".status-pill-dot-live")).toBeNull();
  });

  it("renders shipped with dark page text on the success background", () => {
    render(<StatusPill status="shipped" />);
    const p = screen.getByTestId("status-pill");
    expect(p).toHaveStyle({ color: "var(--color-bg-page)" });
    expect(p).toHaveStyle({ background: "var(--color-success)" });
  });

  it("normalizes orchestrate phases onto pact statuses", async () => {
    const { normalizeStatus } = await import("./StatusPill");
    expect(normalizeStatus("run_owner")).toBe("in_progress");
    expect(normalizeStatus("stuck")).toBe("escalated");
    expect(normalizeStatus("done")).toBe("shipped");
    expect(normalizeStatus("accepted")).toBe("accepted");
  });
});

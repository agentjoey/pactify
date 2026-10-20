import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { JobDrawer } from "./JobDrawer";

afterEach(cleanup);

it("locks background scrolling until the drawer closes and restores prior overflow styles", () => {
  document.documentElement.style.setProperty("overflow", "scroll", "important");
  document.body.style.overflow = "auto";
  try {
    const { rerender, unmount } = render(<JobDrawer id="first" jobs={[]}><p>First run</p></JobDrawer>);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(document.documentElement.style.overflow).toBe("hidden");
    expect(document.body.style.overflow).toBe("hidden");
    rerender(<JobDrawer id="second" jobs={[]}><p>Second run</p></JobDrawer>);
    expect(document.body.style.overflow).toBe("hidden");
    unmount();
    expect(document.documentElement.style.overflow).toBe("scroll");
    expect(document.documentElement.style.getPropertyPriority("overflow")).toBe("important");
    expect(document.body.style.overflow).toBe("auto");
  } finally {
    cleanup();
    document.documentElement.style.removeProperty("overflow");
    document.body.style.removeProperty("overflow");
  }
});

it("removes temporary overflow styles when the original styles were absent", () => {
  const { unmount } = render(<JobDrawer id="job" jobs={[]}><p>Job</p></JobDrawer>);
  expect(document.body.style.overflow).toBe("hidden");
  unmount();
  expect(document.documentElement.style.overflow).toBe("");
  expect(document.body.style.overflow).toBe("");
});

it("focuses the heading, wraps from it and lets Tab move past Close", () => {
  render(<JobDrawer id="job" jobs={[]}><h1 tabIndex={-1}>Task title</h1><button>Next tab</button></JobDrawer>);
  const heading=screen.getByRole("heading",{name:"Task title"});
  const close=screen.getByRole("link",{name:"Close job details"});
  const last=screen.getByRole("button",{name:"Next tab"});
  expect(heading).toHaveFocus();
  const reverse=new KeyboardEvent("keydown",{key:"Tab",shiftKey:true,bubbles:true,cancelable:true});
  heading.dispatchEvent(reverse);
  expect(reverse.defaultPrevented).toBe(true);
  expect(last).toHaveFocus();
  close.focus();
  const forward=new KeyboardEvent("keydown",{key:"Tab",bubbles:true,cancelable:true});
  close.dispatchEvent(forward);
  expect(forward.defaultPrevented).toBe(false);
});

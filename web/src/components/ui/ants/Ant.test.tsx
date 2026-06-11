import { render } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { Ant } from "./Ant";
import type { Caste } from "../../../lib/ants";

const CASTES: Caste[] = [
  "queen", "guard", "builder", "catcher", "brewer", "painter", "scout", "keeper",
];

function svgOf(container: HTMLElement): SVGSVGElement {
  const svg = container.querySelector("svg");
  if (!svg) throw new Error("no svg rendered");
  return svg as unknown as SVGSVGElement;
}

describe("Ant", () => {
  it("renders an svg for every caste at the default size", () => {
    for (const caste of CASTES) {
      const { container } = render(<Ant caste={caste} size={34} />);
      const svg = svgOf(container);
      expect(svg.getAttribute("width")).toBe("34");
      expect(svg.getAttribute("height")).toBe("34");
      expect(svg.getAttribute("viewBox")).toBe("0 0 48 48");
      // rotated 45° clockwise group
      expect(svg.querySelector('g[transform="rotate(45 24 24)"]')).not.toBeNull();
    }
  });

  it("scales to the requested size", () => {
    const { container } = render(<Ant caste="queen" size={72} />);
    const svg = svgOf(container);
    expect(svg.getAttribute("width")).toBe("72");
    expect(svg.getAttribute("height")).toBe("72");
  });

  it("uses the full variant above 18px and the simplified variant at <=18px", () => {
    // Heuristic: the full builder has leg paths (more <path>/<circle> nodes)
    // than the bold-stroke 14px variant. Compare node counts.
    const full = render(<Ant caste="builder" size={34} />);
    const small = render(<Ant caste="builder" size={14} />);
    const fullNodes = full.container.querySelectorAll("svg *").length;
    const smallNodes = small.container.querySelectorAll("svg *").length;
    expect(smallNodes).toBeLessThan(fullNodes);
  });

  it("renders the small variant exactly at the 18px boundary", () => {
    const { container } = render(<Ant caste="guard" size={18} />);
    const full = render(<Ant caste="guard" size={22} />);
    const smallNodes = container.querySelectorAll("svg *").length;
    const fullNodes = full.container.querySelectorAll("svg *").length;
    expect(smallNodes).toBeLessThan(fullNodes);
  });

  it("is aria-hidden by default with no accessible name", () => {
    const { container } = render(<Ant caste="builder" size={22} />);
    const svg = svgOf(container);
    expect(svg.getAttribute("aria-hidden")).toBe("true");
    expect(svg.getAttribute("role")).not.toBe("img");
    expect(svg.querySelector("title")).toBeNull();
  });

  it("exposes role=img and a <title> when a title is given", () => {
    const { container } = render(<Ant caste="queen" size={22} title="Queen — orchestrator" />);
    const svg = svgOf(container);
    expect(svg.getAttribute("role")).toBe("img");
    expect(svg.getAttribute("aria-hidden")).not.toBe("true");
    const title = svg.querySelector("title");
    expect(title?.textContent).toBe("Queen — orchestrator");
  });

  it("renders as a block element", () => {
    const { container } = render(<Ant caste="builder" size={22} />);
    const svg = svgOf(container);
    expect(svg.style.display).toBe("block");
  });
});

import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { AgentLogo } from "./agentLogos";

describe("AgentLogo", () => {
  it("uses the generic logo for the retired gemini family and keeps the agy logo", () => {
    render(<>
      <AgentLogo kind="gemini" />
      <AgentLogo kind="unknown" />
      <AgentLogo kind="antigravity" />
    </>);
    const retired = screen.getByTitle("gemini");
    const generic = screen.getByTitle("unknown");
    const agy = screen.getByTitle("antigravity");
    expect(retired.innerHTML).toBe(generic.innerHTML);
    expect(retired.getAttribute("style")).toBe(generic.getAttribute("style"));
    expect(agy.innerHTML).not.toBe(generic.innerHTML);
    expect(agy.getAttribute("style")).not.toBe(generic.getAttribute("style"));
  });
});

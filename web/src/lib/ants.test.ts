import { describe, it, expect } from "vitest";
import { casteForRoles, padGradient, type Caste } from "./ants";

describe("casteForRoles", () => {
  it("maps protocol-hard roles first with orchestrator > reviewer > worker priority", () => {
    expect(casteForRoles(["orchestrator"])).toBe("queen");
    expect(casteForRoles(["reviewer"])).toBe("guard");
    expect(casteForRoles(["worker"])).toBe("builder");
    // priority among multiple protocol roles
    expect(casteForRoles(["worker", "reviewer", "orchestrator"])).toBe("queen");
    expect(casteForRoles(["worker", "reviewer"])).toBe("guard");
  });

  it("lets protocol-hard roles win over keyword roles", () => {
    // ["worker","qa"]: worker is protocol-hard → builder, NOT catcher
    expect(casteForRoles(["worker", "qa"])).toBe("builder");
    expect(casteForRoles(["reviewer", "design"])).toBe("guard");
  });

  it("matches free-form roles by case-insensitive substring", () => {
    expect(casteForRoles(["qa"])).toBe("catcher");
    expect(casteForRoles(["test"])).toBe("catcher");
    expect(casteForRoles(["tester"])).toBe("catcher"); // contains "test"
    expect(casteForRoles(["QA-Lead"])).toBe("catcher");
    expect(casteForRoles(["product"])).toBe("brewer");
    expect(casteForRoles(["PM"])).toBe("brewer");
    expect(casteForRoles(["design"])).toBe("painter");
    expect(casteForRoles(["designer"])).toBe("painter"); // contains "design"
    expect(casteForRoles(["research"])).toBe("scout");
    expect(casteForRoles(["Researcher"])).toBe("scout");
    expect(casteForRoles(["ops"])).toBe("keeper");
    expect(casteForRoles(["operation"])).toBe("keeper");
    expect(casteForRoles(["devops"])).toBe("keeper");
  });

  it("falls back to builder for unknown or empty roles", () => {
    expect(casteForRoles([])).toBe("builder");
    expect(casteForRoles(["wizard"])).toBe("builder");
    expect(casteForRoles([""])).toBe("builder");
  });

  it("uses the first matching keyword in role order", () => {
    // qa before design → catcher
    expect(casteForRoles(["qa", "design"])).toBe("catcher");
  });
});

const CASTES: Caste[] = [
  "queen", "guard", "builder", "catcher", "brewer", "painter", "scout", "keeper",
];

describe("padGradient", () => {
  it("is deterministic — same input yields the same output", () => {
    const a = padGradient("alice", "builder");
    const b = padGradient("alice", "builder");
    expect(a).toEqual(b);
  });

  it("returns valid hex colors", () => {
    for (const caste of CASTES) {
      const g = padGradient("seat-1", caste);
      expect(g.from).toMatch(/^#[0-9a-fA-F]{6}$/);
      expect(g.to).toMatch(/^#[0-9a-fA-F]{6}$/);
    }
  });

  it("differs between distinct seat ids of the same caste", () => {
    const a = padGradient("alice", "builder");
    const b = padGradient("bob", "builder");
    const c = padGradient("carol", "builder");
    // at least two of three should differ (hash jitter)
    const all = [a.from + a.to, b.from + b.to, c.from + c.to];
    expect(new Set(all).size).toBeGreaterThan(1);
  });

  it("preserves the caste's hue family (jitter only shifts lightness)", () => {
    // The builder base is green (#1F3329 → #16231C): green channel dominates.
    for (const seat of ["alice", "bob", "zzz", "x"]) {
      const g = padGradient(seat, "builder");
      const r = parseInt(g.from.slice(1, 3), 16);
      const gr = parseInt(g.from.slice(3, 5), 16);
      const b = parseInt(g.from.slice(5, 7), 16);
      expect(gr).toBeGreaterThanOrEqual(r);
      expect(gr).toBeGreaterThanOrEqual(b);
    }
  });

  it("each caste keeps its own base distinct from others", () => {
    const seat = "fixed-seat";
    const grads = CASTES.map((c) => padGradient(seat, c).from);
    // all eight castes produce different base-derived from-colors
    expect(new Set(grads).size).toBe(CASTES.length);
  });
});

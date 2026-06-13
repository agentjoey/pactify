import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const getRecipes = vi.fn();
const expandRecipe = vi.fn();
vi.mock("../lib/api", () => ({
  getRecipes: (...a: unknown[]) => getRecipes(...a),
  expandRecipe: (...a: unknown[]) => expandRecipe(...a),
}));

import { Recipes } from "./Recipes";

describe("Recipes view", () => {
  beforeEach(() => {
    getRecipes.mockReset();
    expandRecipe.mockReset();
  });

  it("lists recipes and selects the first by default", async () => {
    getRecipes.mockResolvedValue([
      { name: "add-tests", description: "补测试" },
      { name: "review-harden", description: "实现+加固" },
    ]);
    render(<Recipes />);
    await waitFor(() => {
      expect(screen.getByTestId("recipe-add-tests")).toBeTruthy();
    });
    expect(screen.getByTestId("recipe-add-tests").getAttribute("aria-pressed")).toBe("true");
  });

  it("expands the selected recipe as the goal is typed (debounced)", async () => {
    getRecipes.mockResolvedValue([{ name: "review-harden", description: "实现+加固" }]);
    expandRecipe.mockResolvedValue([
      { id: "t-impl", spec: "实现 加缓存" },
      { id: "t-harden", spec: "加固 加缓存", deps: ["t-impl"] },
    ]);
    render(<Recipes />);
    await waitFor(() => expect(screen.getByTestId("recipe-goal")).toBeTruthy());
    fireEvent.change(screen.getByTestId("recipe-goal"), { target: { value: "加缓存" } });
    await waitFor(
      () => {
        expect(screen.getAllByTestId("recipe-task")).toHaveLength(2);
      },
      { timeout: 1500 },
    );
    expect(expandRecipe).toHaveBeenCalledWith("review-harden", "加缓存");
    expect(screen.getByText(/deps: t-impl/)).toBeTruthy();
    expect(screen.getByText(/pactify plan review-harden/)).toBeTruthy();
  });

  it("shows an empty state when there are no recipes", async () => {
    getRecipes.mockResolvedValue([]);
    render(<Recipes />);
    await waitFor(() => {
      expect(screen.getByText("没有可用配方")).toBeTruthy();
    });
  });

  it("does not expand with an empty goal", async () => {
    getRecipes.mockResolvedValue([{ name: "add-tests", description: "补测试" }]);
    render(<Recipes />);
    await waitFor(() => expect(screen.getByTestId("recipe-goal")).toBeTruthy());
    // no goal typed → expand never called
    expect(expandRecipe).not.toHaveBeenCalled();
  });
});

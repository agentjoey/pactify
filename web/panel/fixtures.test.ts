import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
it("uses neutral task titles in browser and screenshot fixtures", () => {
  expect(readFileSync("e2e/jobs-fixtures.mjs", "utf8")).not.toMatch(/(?:AJ|Example)-\d+/);
});

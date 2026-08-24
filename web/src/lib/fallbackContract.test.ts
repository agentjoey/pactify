import { describe, it, expect, vi, afterEach } from "vitest";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { getFallbackProposals, approveFallback } from "./api";

/**
 * Cross-language contract for the fallback proposal endpoints.
 *
 * The dashboard and internal/serve each used to be tested against their own
 * private idea of this payload: FallbackCard.test.tsx mocked the DataSource,
 * e2e/mock-server.mjs was hand-written, and the Go handler had its own table
 * test. When the response went from one object to a list, all three stayed green
 * while the dashboard silently dropped every escalation on the floor.
 *
 * So these tests take the shape FROM THE GO SOURCE and drive the real client
 * with it. Renaming a json tag, dropping the list wrapper, or reverting to the
 * single-object response makes them fail here rather than in production.
 */
// jsdom rewrites import.meta.url to an http URL, so walk up from the cwd to the
// repo root instead of resolving relative to this file.
function repoFile(rel: string): string {
  for (let dir = process.cwd(); ; dir = dirname(dir)) {
    const p = join(dir, rel);
    if (existsSync(p)) return p;
    if (dirname(dir) === dir) throw new Error(`cannot locate ${rel} from ${process.cwd()}`);
  }
}

const GO_SRC = readFileSync(repoFile("internal/serve/fallback_proposal.go"), "utf8");
const MOCK_SRC = readFileSync(repoFile("web/e2e/mock-server.mjs"), "utf8");

/** The json tag names of a Go struct type, in declaration order. */
function goStructTags(src: string, typeName: string): string[] {
  const m = new RegExp(`type ${typeName} struct \\{([\\s\\S]*?)\\n\\}`).exec(src);
  if (!m) throw new Error(`internal/serve no longer declares "type ${typeName} struct"`);
  return [...m[1].matchAll(/json:"([^",]+)/g)].map((t) => t[1]);
}

/** The json tag names of the anonymous request struct inside a Go func. */
function goRequestTags(src: string, funcName: string): string[] {
  const m = new RegExp(`func [\\s\\S]*?${funcName}\\(([\\s\\S]*?)\\n\\}\\n`).exec(src);
  if (!m) throw new Error(`internal/serve no longer declares ${funcName}`);
  const req = /var req struct \{([\s\S]*?)\n\t\}/.exec(m[1]);
  if (!req) throw new Error(`${funcName} no longer decodes a request body struct`);
  return [...req[1].matchAll(/json:"([^",]+)/g)].map((t) => t[1]);
}

const PROPOSAL_TAGS = goStructTags(GO_SRC, "fallbackProposalDTO");
const WRAPPER_TAGS = goStructTags(GO_SRC, "fallbackProposalsDTO");
const APPROVE_TAGS = goRequestTags(GO_SRC, "handleFallbackApprove");

afterEach(() => vi.unstubAllGlobals());

describe("fallback proposal contract — serve ↔ dashboard", () => {
  it("the response is a list under one wrapper key", () => {
    // A single wrapper field holding the list. If serve ever goes back to
    // returning the proposal itself (with a `pending` flag), this is where it
    // shows up, not in a silently empty dashboard.
    expect(WRAPPER_TAGS).toEqual(["proposals"]);
    expect(GO_SRC).toContain("Proposals []fallbackProposalDTO");
  });

  it("the dashboard client reads exactly the key serve writes", async () => {
    const [wrapper] = WRAPPER_TAGS;
    const proposal = Object.fromEntries(PROPOSAL_TAGS.map((t) => [t, t]));
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({ ok: true, json: async () => ({ [wrapper]: [proposal, proposal] }) })),
    );
    const list = await getFallbackProposals("p1");
    expect(list).toHaveLength(2);
    // Every field serve declares survives the trip — nothing is dropped or
    // renamed on the way into the card.
    expect(Object.keys(list[0])).toEqual(PROPOSAL_TAGS);
  });

  it("the fields the card renders are all fields serve actually sends", () => {
    // `scope` is the partition (the filename) and is never omitted; the rest are
    // omitempty. The card reads exactly these and nothing else.
    expect(PROPOSAL_TAGS).toEqual(["scope", "task", "seat", "fromRole", "toRole", "reason"]);
  });

  it("approve posts exactly the field serve decodes", async () => {
    // One field, `task` — approval is per-decision. A body serve cannot match is
    // a 404, which the card renders as "already handled elsewhere": an approval
    // that names the wrong key is indistinguishable from a successful one.
    expect(APPROVE_TAGS).toEqual(["task"]);
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify({ status_url: "/s" }), { status: 202 })),
    );
    await approveFallback("p1", "t1");
    const f = fetch as unknown as ReturnType<typeof vi.fn>;
    expect(Object.keys(JSON.parse(f.mock.calls[0][1].body))).toEqual(APPROVE_TAGS);
  });

  it("serve answers a missing proposal with 404, which the client must not swallow", () => {
    // The card's whole "retire quietly" branch hangs off this status.
    expect(GO_SRC).toContain("http.StatusNotFound");
    expect(GO_SRC).toContain("no fallback proposal is pending for that task");
  });

  it("the e2e mock server serves the same field set as serve", () => {
    // Otherwise the Playwright gate is only testing the mock's imagination —
    // the exact failure mode recorded for the relay↔web incident.
    const m = /const FALLBACK_DTO_FIELDS = (\[[^\]]*\])/.exec(MOCK_SRC);
    if (!m) throw new Error("e2e/mock-server.mjs no longer declares FALLBACK_DTO_FIELDS");
    expect(JSON.parse(m[1])).toEqual(PROPOSAL_TAGS);
    expect(MOCK_SRC).toContain(`{ ${WRAPPER_TAGS[0]}: fallbackProposals }`);
  });
});

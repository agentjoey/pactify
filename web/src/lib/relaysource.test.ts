import { describe, it, expect, vi } from "vitest";
import { RelaySource } from "./relaysource";
import type {
  RelayClient,
  Project,
  PactEvent,
  PactEventBroadcast,
} from "@pactify-apps/relay-client";
import type { PactEvent as PactProjectEvent } from "@pactify-apps/pact-project";
import type { ProjectMeta, State } from "./types";
import type { ProjectStats } from "./api";
import type { PactEventDetail } from "./types";

type MockRelayClient = {
  listProjects: RelayClient["listProjects"];
  getProjectEvents: RelayClient["getProjectEvents"];
  decrypt: RelayClient["decrypt"];
  subscribe: RelayClient["subscribe"];
  sendRpc: RelayClient["sendRpc"];
  account: RelayClient["account"];
};

function makeClient(overrides?: Partial<MockRelayClient>): MockRelayClient {
  return {
    listProjects: vi.fn().mockResolvedValue([]),
    getProjectEvents: vi.fn().mockResolvedValue([]),
    decrypt: vi.fn().mockReturnValue({}),
    subscribe: vi.fn().mockReturnValue(() => {}),
    sendRpc: vi.fn(),
    account: vi.fn().mockReturnValue("acct1"),
    ...overrides,
  } as unknown as MockRelayClient;
}

describe("RelaySource", () => {
  it("can drive pact verbs (canWrite) but not orchestrate remotely", () => {
    const src = new RelaySource(makeClient() as RelayClient);
    expect(src.capabilities).toEqual({
      canWrite: true,
      canOrchestrate: false,
      multiMachine: true,
    });
  });

  it("verb sends the matching pact.* rpc targeting the account machine", async () => {
    const sendRpc = vi.fn();
    const src = new RelaySource(makeClient({ sendRpc }) as RelayClient);

    await src.verb("demo", "accept", { task: "t1" });
    expect(sendRpc).toHaveBeenCalledWith({ type: "pact.accept", machineId: "acct1", project: "demo", task: "t1" });

    await src.verb("demo", "changes", { task: "t1", reason: "fix" });
    expect(sendRpc).toHaveBeenCalledWith({ type: "pact.changes", machineId: "acct1", project: "demo", task: "t1", reason: "fix" });

    await src.verb("demo", "merge", { feature: "f1" });
    expect(sendRpc).toHaveBeenCalledWith({ type: "pact.merge", machineId: "acct1", project: "demo", feature: "f1" });
  });

  it("listProjects maps relay projects to ProjectMeta", async () => {
    const projects: Project[] = [
      {
        id: "p1",
        name: "Project One",
        feature: "feat-1",
        seq: 3,
        lastEventAt: 1,
      },
      { id: "p2", name: "Project Two", seq: 0, lastEventAt: 2 },
    ];
    const client = makeClient({
      listProjects: vi.fn().mockResolvedValue(projects),
    });
    const src = new RelaySource(client as RelayClient);
    const result = await src.listProjects();
    expect(result).toEqual<ProjectMeta[]>([
      {
        id: "p1",
        name: "Project One",
        path: "p1",
        project: "p1",
        feature_count: 1,
        awaiting_count: 0,
      },
      {
        id: "p2",
        name: "Project Two",
        path: "p2",
        project: "p2",
        feature_count: 0,
        awaiting_count: 0,
      },
    ]);
    expect(client.listProjects).toHaveBeenCalled();
  });

  it("getState decrypts events and projects to State", async () => {
    const events: PactEvent[] = [
      {
        projectId: "p1",
        seq: 1,
        eventType: "init",
        ts: 1,
        bodyEnc: "enc-init",
      },
      {
        projectId: "p1",
        seq: 2,
        eventType: "assign",
        feature: "f1",
        task: "t1",
        ts: 2,
        bodyEnc: "enc-assign",
      },
    ];
    const decrypted: PactProjectEvent[] = [
      {
        event_id: "e1",
        ts: "1",
        agent_id: "",
        role: "",
        event_type: "init",
        task_id: "",
        feature: "",
        payload: {
          project: "p1",
          seats: [{ id: "alice", roles: ["worker"] }],
        },
      },
      {
        event_id: "e2",
        ts: "2",
        agent_id: "alice",
        role: "worker",
        event_type: "assign",
        task_id: "t1",
        feature: "f1",
        payload: {
          owner: "alice",
          reviewer: "bob",
          spec: "spec.md",
          branch: "feat-f1",
        },
      },
    ];
    const client = makeClient({
      getProjectEvents: vi.fn().mockResolvedValue(events),
      decrypt: vi.fn().mockImplementation((_id: string, bodyEnc: string) => {
        const idx = events.findIndex((e) => e.bodyEnc === bodyEnc);
        return decrypted[idx];
      }),
    });
    const src = new RelaySource(client as RelayClient);
    const state = await src.getState("p1");
    expect(state.project).toBe("p1");
    expect(state.agents).toEqual([{ id: "alice", roles: ["worker"] }]);
    expect(state.features).toHaveLength(1);
    expect(state.features[0].id).toBe("f1");
    expect(state.features[0].tasks[0].id).toBe("t1");
    expect(state.features[0].tasks[0].owner).toBe("alice");
    expect(state.features[0].tasks[0].reviewer).toBe("bob");
    expect(state.awaiting_count).toBe(0);
    expect(client.getProjectEvents).toHaveBeenCalledWith("p1");
    expect(client.decrypt).toHaveBeenCalledWith("p1", "enc-init");
    expect(client.decrypt).toHaveBeenCalledWith("p1", "enc-assign");
  });

  it("getEvents decrypts events and preserves cleartext headers", async () => {
    const events: PactEvent[] = [
      {
        projectId: "p1",
        seq: 1,
        eventType: "assign",
        feature: "f1",
        task: "t1",
        ts: 1_700_000_000_000,
        bodyEnc: "enc-assign",
      },
      {
        projectId: "p1",
        seq: 2,
        eventType: "checkpoint",
        feature: "f1",
        task: "t1",
        ts: 1_700_000_100_000,
        bodyEnc: "enc-checkpoint",
      },
      {
        projectId: "p1",
        seq: 3,
        eventType: "accept",
        feature: "f1",
        task: "t1",
        ts: 1_700_000_200_000,
        bodyEnc: "enc-accept",
      },
    ];
    const bodies: Record<string, Record<string, unknown>> = {
      "enc-assign": {
        event_id: "e1",
        ts: "2023-11-14T22:13:20Z",
        agent_id: "alice",
        role: "orchestrator",
        event_type: "assign",
        task_id: "t1",
        feature: "f1",
        payload: { owner: "alice", reviewer: "bob", spec: "spec.md", branch: "feat-f1" },
      },
      "enc-checkpoint": {
        event_id: "e2",
        ts: "2023-11-14T22:15:00Z",
        agent_id: "alice",
        role: "worker",
        event_type: "checkpoint",
        task_id: "t1",
        feature: "f1",
        payload: { evidence: "go test ./... ok" },
      },
      "enc-accept": {
        event_id: "e3",
        ts: "2023-11-14T22:16:40Z",
        agent_id: "bob",
        role: "reviewer",
        event_type: "accept",
        task_id: "t1",
        feature: "f1",
        payload: {},
      },
    };
    const client = makeClient({
      getProjectEvents: vi.fn().mockResolvedValue(events),
      decrypt: vi.fn().mockImplementation((_id: string, bodyEnc: string) => bodies[bodyEnc]),
    });
    const src = new RelaySource(client as RelayClient);
    const result = await src.getEvents("p1");
    expect(result).toHaveLength(3);
    expect(result).toEqual<PactEventDetail[]>([
      {
        seq: 1,
        eventType: "assign",
        task: "t1",
        feature: "f1",
        ts: 1_700_000_000_000,
        body: bodies["enc-assign"],
      },
      {
        seq: 2,
        eventType: "checkpoint",
        task: "t1",
        feature: "f1",
        ts: 1_700_000_100_000,
        body: bodies["enc-checkpoint"],
      },
      {
        seq: 3,
        eventType: "accept",
        task: "t1",
        feature: "f1",
        ts: 1_700_000_200_000,
        body: bodies["enc-accept"],
      },
    ]);
    expect(client.getProjectEvents).toHaveBeenCalledWith("p1");
    expect(client.decrypt).toHaveBeenCalledWith("p1", "enc-assign");
  });

  it("subscribe fetches and projects state on new events, then unsubscribes", async () => {
    const events: PactEvent[] = [
      {
        projectId: "p1",
        seq: 1,
        eventType: "init",
        ts: 1,
        bodyEnc: "enc-init",
      },
    ];
    const decrypted: PactProjectEvent[] = [
      {
        event_id: "e1",
        ts: "1",
        agent_id: "",
        role: "",
        event_type: "init",
        task_id: "",
        feature: "",
        payload: { project: "p1", seats: [] },
      },
    ];
    let handler: ((e: PactEventBroadcast) => void) | undefined;
    const client = makeClient({
      getProjectEvents: vi.fn().mockResolvedValue(events),
      decrypt: vi.fn().mockReturnValue(decrypted[0]),
      subscribe: vi
        .fn()
        .mockImplementation(
          (_id: string, onEvent: (e: PactEventBroadcast) => void) => {
            handler = onEvent;
            return () => {
              handler = undefined;
            };
          },
        ),
    });
    const src = new RelaySource(client as RelayClient);
    const onState = vi.fn();
    const off = src.subscribe("p1", onState);

    expect(client.subscribe).toHaveBeenCalledWith(
      "p1",
      expect.any(Function),
    );

    handler!({
      projectId: "p1",
      seq: 2,
      eventType: "checkpoint",
      feature: "f1",
      task: "t1",
      ts: 3,
      bodyEnc: "enc-ckpt",
    });

    await new Promise((r) => setTimeout(r, 0));
    expect(client.getProjectEvents).toHaveBeenCalledWith("p1");
    expect(onState).toHaveBeenCalledWith(
      expect.objectContaining<Partial<State>>({
        project: "p1",
        agents: [],
        awaiting_count: 0,
      }),
    );

    off();
    expect(handler).toBeUndefined();
  });

  it("getStats derives minimal stats from projected state", async () => {
    const events: PactEvent[] = [
      {
        projectId: "p1",
        seq: 1,
        eventType: "init",
        ts: 1,
        bodyEnc: "enc-init",
      },
    ];
    const decrypted: PactProjectEvent[] = [
      {
        event_id: "e1",
        ts: "1",
        agent_id: "",
        role: "",
        event_type: "init",
        task_id: "",
        feature: "",
        payload: {
          project: "p1",
          seats: [{ id: "alice", roles: ["worker"] }],
        },
      },
    ];
    const client = makeClient({
      getProjectEvents: vi.fn().mockResolvedValue(events),
      decrypt: vi.fn().mockReturnValue(decrypted[0]),
    });
    const src = new RelaySource(client as RelayClient);
    const stats = await src.getStats("p1");
    expect(stats).toEqual<ProjectStats>({
      tasks: [],
      agents: [
        { seat: "alice", tasks: 0, duration_sec: 0, added: 0, deleted: 0, tokens: 0 },
      ],
    });
  });
});

import { describe, it, expect, vi } from "vitest";
import { RelaySource } from "./relaysource";
import type {
  RelayClient,
  Project,
  PactEvent,
  PactEventBroadcast,
} from "@pactify-apps/relay-client";
import type { PactEvent as PactProjectEvent } from "@pactify-apps/pact-project";
import type { Machine, ProjectMeta, State } from "./types";
import type { ProjectStats } from "./api";
import type { PactEventDetail } from "./types";
// RelaySource.subscribe emits the web SSE-frame PactEvent (decrypted body), which
// is a DIFFERENT type from relay-client's stored PactEvent imported above.
import type { PactEvent as SseEvent } from "./types";

type MockRelayClient = {
  listProjects: RelayClient["listProjects"];
  listMachines: RelayClient["listMachines"];
  getProjectEvents: RelayClient["getProjectEvents"];
  decryptRaw: RelayClient["decryptRaw"];
  subscribe: RelayClient["subscribe"];
  sendRpc: RelayClient["sendRpc"];
  account: RelayClient["account"];
};

function makeClient(overrides?: Partial<MockRelayClient>): MockRelayClient {
  return {
    listProjects: vi.fn().mockResolvedValue([]),
    listMachines: vi.fn().mockResolvedValue([]),
    getProjectEvents: vi.fn().mockResolvedValue([]),
    decryptRaw: vi.fn().mockReturnValue({}),
    subscribe: vi.fn().mockReturnValue(() => {}),
    sendRpc: vi.fn(),
    account: vi.fn().mockReturnValue("acct1"),
    ...overrides,
  } as unknown as MockRelayClient;
}

describe("RelaySource", () => {
  it("can drive pact verbs (canWrite) and orchestrate remotely", () => {
    const src = new RelaySource(makeClient() as RelayClient);
    expect(src.capabilities).toEqual({
      canWrite: true,
      canOrchestrate: true,
      multiMachine: true,
      cockpit: false,
    });
  });

  it("verb sends the matching pact.* rpc targeting the first online machine", async () => {
    const sendRpc = vi.fn();
    const listMachines = vi.fn().mockResolvedValue([
      { machineId: "m-offline", agentKinds: [], online: false, lastSeenAt: 0 },
      { machineId: "m-1", host: "build", agentKinds: ["opencode"], online: true, lastSeenAt: 1 },
    ]);
    const src = new RelaySource(makeClient({ sendRpc, listMachines }) as RelayClient);

    await src.verb("demo", "accept", { task: "t1" });
    expect(sendRpc).toHaveBeenCalledWith({ type: "pact.accept", machineId: "m-1", project: "demo", task: "t1" });

    await src.verb("demo", "changes", { task: "t1", reason: "fix" });
    expect(sendRpc).toHaveBeenCalledWith({ type: "pact.changes", machineId: "m-1", project: "demo", task: "t1", reason: "fix" });

    await src.verb("demo", "merge", { feature: "f1" });
    expect(sendRpc).toHaveBeenCalledWith({ type: "pact.merge", machineId: "m-1", project: "demo", feature: "f1" });

    // A pinned machine overrides the auto-pick.
    src.setTargetMachine("m-pinned");
    await src.verb("demo", "accept", { task: "t2" });
    expect(sendRpc).toHaveBeenCalledWith({ type: "pact.accept", machineId: "m-pinned", project: "demo", task: "t2" });
  });

  it("runOrchestrate sends orchestrate.run rpc targeting the first online machine", async () => {
    const sendRpc = vi.fn();
    const listMachines = vi.fn().mockResolvedValue([
      { machineId: "m-offline", agentKinds: [], online: false, lastSeenAt: 0 },
      { machineId: "m-1", host: "build", agentKinds: ["opencode"], online: true, lastSeenAt: 1 },
    ]);
    const src = new RelaySource(makeClient({ sendRpc, listMachines }) as RelayClient);

    const res = await src.runOrchestrate("demo");
    expect(sendRpc).toHaveBeenCalledWith({ type: "orchestrate.run", machineId: "m-1", project: "demo" });
    expect(res).toEqual({ status_url: "" });

    // feature + seat_kinds map to the rpc's feature/seatKinds fields.
    await src.runOrchestrate("demo", { feature: "f1", seat_kinds: { alice: "opencode" } });
    expect(sendRpc).toHaveBeenCalledWith({
      type: "orchestrate.run",
      machineId: "m-1",
      project: "demo",
      feature: "f1",
      seatKinds: { alice: "opencode" },
    });

    // A pinned machine overrides the auto-pick.
    src.setTargetMachine("m-pinned");
    await src.runOrchestrate("demo", { feature: "f2" });
    expect(sendRpc).toHaveBeenCalledWith({
      type: "orchestrate.run",
      machineId: "m-pinned",
      project: "demo",
      feature: "f2",
    });
  });

  it("resumeOrchestrate sends orchestrate.resume rpc targeting the resolved machine", async () => {
    const sendRpc = vi.fn();
    const listMachines = vi.fn().mockResolvedValue([
      { machineId: "m-offline", agentKinds: [], online: false, lastSeenAt: 0 },
      { machineId: "m-1", host: "build", agentKinds: ["opencode"], online: true, lastSeenAt: 1 },
    ]);
    const src = new RelaySource(makeClient({ sendRpc, listMachines }) as RelayClient);

    const res = await src.resumeOrchestrate("demo", { feature: "f1" });
    expect(sendRpc).toHaveBeenCalledWith({
      type: "orchestrate.resume",
      machineId: "m-1",
      project: "demo",
      feature: "f1",
    });
    expect(res).toEqual({ status_url: "" });

    src.setTargetMachine("m-pinned");
    await src.resumeOrchestrate("demo");
    expect(sendRpc).toHaveBeenCalledWith({ type: "orchestrate.resume", machineId: "m-pinned", project: "demo" });
  });

  it("generatePlan sends a one-shot plan.generate rpc targeting the resolved machine", async () => {
    const sendRpc = vi.fn();
    const listMachines = vi.fn().mockResolvedValue([
      { machineId: "m-offline", agentKinds: [], online: false, lastSeenAt: 0 },
      { machineId: "m-1", host: "build", agentKinds: ["opencode"], online: true, lastSeenAt: 1 },
    ]);
    const src = new RelaySource(makeClient({ sendRpc, listMachines }) as RelayClient);

    const res = await src.generatePlan("demo", { goal: "add 2fa", feature: "add-2fa" });
    expect(sendRpc).toHaveBeenCalledWith({
      type: "plan.generate",
      machineId: "m-1",
      project: "demo",
      goal: "add 2fa",
      feature: "add-2fa",
    });
    expect(res).toEqual({ status_url: "", feature: "add-2fa" });

    // planner_kind maps to the rpc's plannerKind field.
    await src.generatePlan("demo", { goal: "g", feature: "f", planner_kind: "claude" });
    expect(sendRpc).toHaveBeenCalledWith({
      type: "plan.generate",
      machineId: "m-1",
      project: "demo",
      goal: "g",
      feature: "f",
      plannerKind: "claude",
    });

    // A pinned machine overrides the auto-pick.
    src.setTargetMachine("m-pinned");
    await src.generatePlan("demo", { goal: "g2", feature: "f2" });
    expect(sendRpc).toHaveBeenCalledWith({
      type: "plan.generate",
      machineId: "m-pinned",
      project: "demo",
      goal: "g2",
      feature: "f2",
    });

    // RelaySource has no status/review round-trip (one-shot over the relay).
    expect((src as unknown as { getPlanGenStatus?: unknown }).getPlanGenStatus).toBeUndefined();
    expect((src as unknown as { getPlanReview?: unknown }).getPlanReview).toBeUndefined();
  });

  it("postTask sends a pact.task rpc targeting the first online machine", async () => {
    const sendRpc = vi.fn();
    const listMachines = vi.fn().mockResolvedValue([
      { machineId: "m-offline", agentKinds: [], online: false, lastSeenAt: 0 },
      { machineId: "m-1", host: "build", agentKinds: ["opencode"], online: true, lastSeenAt: 1 },
    ]);
    const src = new RelaySource(makeClient({ sendRpc, listMachines }) as RelayClient);

    // spec_md maps to the rpc's specMd field (PactTaskRequest shape).
    const res = await src.postTask("demo", { id: "t7", spec_md: "# spec body" });
    expect(sendRpc).toHaveBeenCalledWith({
      type: "pact.task",
      machineId: "m-1",
      project: "demo",
      id: "t7",
      specMd: "# spec body",
    });
    expect(res).toBeUndefined();

    // A pinned machine overrides the auto-pick.
    src.setTargetMachine("m-pinned");
    await src.postTask("demo", { id: "t8", spec_md: "x" });
    expect(sendRpc).toHaveBeenCalledWith({
      type: "pact.task",
      machineId: "m-pinned",
      project: "demo",
      id: "t8",
      specMd: "x",
    });
  });

  it("applyPlan sends plan.apply rpc targeting the resolved machine", async () => {
    const sendRpc = vi.fn();
    const listMachines = vi.fn().mockResolvedValue([
      { machineId: "m-1", host: "build", agentKinds: ["opencode"], online: true, lastSeenAt: 1 },
    ]);
    const src = new RelaySource(makeClient({ sendRpc, listMachines }) as RelayClient);

    const res = await src.applyPlan("demo", "f1");
    expect(sendRpc).toHaveBeenCalledWith({ type: "plan.apply", machineId: "m-1", project: "demo", feature: "f1" });
    expect(res).toEqual({ assigned: 0 });
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

  it("getMachines maps relay MachineInfo to Machine", async () => {
    const machines = [
      {
        machineId: "m1",
        host: "laptop.local",
        agentKinds: ["opencode", "claude-code"],
        workdirs: ["/Users/x/p1"],
        online: true,
        lastSeenAt: 1_700_000_000_000,
      },
      {
        machineId: "m2",
        agentKinds: ["gemini"],
        online: false,
        lastSeenAt: 1_700_000_000_000 - 3_600_000,
      },
    ];
    const client = makeClient({
      listMachines: vi.fn().mockResolvedValue(machines),
    });
    const src = new RelaySource(client as RelayClient);
    const result = await src.getMachines();
    expect(result).toEqual<Machine[]>([
      {
        machineId: "m1",
        host: "laptop.local",
        agentKinds: ["opencode", "claude-code"],
        workdirs: ["/Users/x/p1"],
        online: true,
        lastSeenAt: 1_700_000_000_000,
      },
      {
        machineId: "m2",
        agentKinds: ["gemini"],
        online: false,
        lastSeenAt: 1_700_000_000_000 - 3_600_000,
      },
    ]);
    expect(client.listMachines).toHaveBeenCalled();
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
      decryptRaw: vi.fn().mockImplementation((_id: string, bodyEnc: string) => {
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
    expect(client.decryptRaw).toHaveBeenCalledWith("p1", "enc-init");
    expect(client.decryptRaw).toHaveBeenCalledWith("p1", "enc-assign");
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
      decryptRaw: vi.fn().mockImplementation((_id: string, bodyEnc: string) => bodies[bodyEnc]),
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
    expect(client.decryptRaw).toHaveBeenCalledWith("p1", "enc-assign");
  });

  it("fetchEventsLog returns decrypted PactEvent frames (SSE-frame shape)", async () => {
    const events: PactEvent[] = [
      { projectId: "p1", seq: 1, eventType: "assign", feature: "f1", task: "t1", ts: 1, bodyEnc: "enc-a" },
      { projectId: "p1", seq: 2, eventType: "checkpoint", feature: "f1", task: "t1", ts: 2, bodyEnc: "enc-b" },
      { projectId: "p1", seq: 3, eventType: "accept", feature: "f1", task: "t1", ts: 3, bodyEnc: "enc-c" },
    ];
    const bodies: Record<string, PactProjectEvent> = {
      "enc-a": { event_id: "e1", ts: "1", agent_id: "alice", role: "orchestrator", event_type: "assign", task_id: "t1", feature: "f1", payload: { owner: "alice" } },
      "enc-b": { event_id: "e2", ts: "2", agent_id: "alice", role: "worker", event_type: "checkpoint", task_id: "t1", feature: "f1", payload: { evidence: "ok" } },
      "enc-c": { event_id: "e3", ts: "3", agent_id: "bob", role: "reviewer", event_type: "accept", task_id: "t1", feature: "f1", payload: {} },
    };
    const client = makeClient({
      getProjectEvents: vi.fn().mockResolvedValue(events),
      decryptRaw: vi.fn().mockImplementation((_id: string, bodyEnc: string) => bodies[bodyEnc]),
    });
    const src = new RelaySource(client as RelayClient);

    const all = await src.fetchEventsLog("p1");
    expect(all).toEqual([bodies["enc-a"], bodies["enc-b"], bodies["enc-c"]]);
    expect(client.getProjectEvents).toHaveBeenCalledWith("p1");

    // n slices to the last n events (matches the local /events/log cap semantics).
    const tail = await src.fetchEventsLog("p1", undefined, 2);
    expect(tail).toEqual([bodies["enc-b"], bodies["enc-c"]]);
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
      decryptRaw: vi.fn().mockReturnValue(decrypted[0]),
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
      undefined, // onLive omitted by this caller → passed through as undefined
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
      decryptRaw: vi.fn().mockReturnValue(decrypted[0]),
    });
    const src = new RelaySource(client as RelayClient);
    const stats = await src.getStats("p1");
    expect(stats).toEqual<ProjectStats>({
      tasks: [],
      agents: [
        { seat: "alice", tasks: 0, duration_sec: 0, added: 0, deleted: 0, tokens: 0, accepted: 0, reworked: 0 },
      ],
    });
  });

  // subscribe must forward BOTH the liveness callback (drives the online/offline
  // header — previously dropped, so hosted was stuck "offline") and the decrypted
  // event (drives the Live event stream — previously never forwarded).
  it("subscribe forwards liveness and decrypted events to the caller", async () => {
    const frame = { projectId: "p1", bodyEnc: "enc" } as PactEventBroadcast;
    const decodedEvent = { event_id: "e1", event_type: "checkpoint" } as unknown as SseEvent;
    let clientOnEvent: ((e: PactEventBroadcast) => void) | undefined;
    let clientOnConn: ((live: boolean) => void) | undefined;
    const client = makeClient({
      subscribe: vi.fn((_id, onEvent, onConn) => {
        clientOnEvent = onEvent;
        clientOnConn = onConn;
        return () => {};
      }),
      decryptRaw: vi.fn().mockReturnValue(decodedEvent),
      getProjectEvents: vi.fn().mockResolvedValue([]),
    });
    const src = new RelaySource(client as RelayClient);

    const states: State[] = [];
    const events: SseEvent[] = [];
    const live: boolean[] = [];
    src.subscribe(
      "p1",
      (s) => states.push(s),
      (e) => events.push(e),
      () => {},
      (v) => live.push(v),
    );

    // Liveness callback is wired straight through to the client's onConn.
    expect(clientOnConn).toBeTypeOf("function");
    clientOnConn!(true);
    clientOnConn!(false);
    expect(live).toEqual([true, false]);

    // A frame decrypts to a PactEvent forwarded to onEvent, and re-folds state.
    await clientOnEvent!(frame);
    expect(events).toEqual([decodedEvent]);
    expect(states.length).toBe(1);
  });

  it("subscribe skips an undecryptable frame without throwing", async () => {
    let clientOnEvent: ((e: PactEventBroadcast) => void) | undefined;
    const client = makeClient({
      subscribe: vi.fn((_id, onEvent) => {
        clientOnEvent = onEvent;
        return () => {};
      }),
      decryptRaw: vi.fn(() => {
        throw new Error("bad key");
      }),
      getProjectEvents: vi.fn().mockResolvedValue([]),
    });
    const src = new RelaySource(client as RelayClient);
    const events: SseEvent[] = [];
    const states: State[] = [];
    src.subscribe("p1", (s) => states.push(s), (e) => events.push(e));

    await expect(
      clientOnEvent!({ projectId: "p1", bodyEnc: "enc" } as PactEventBroadcast),
    ).resolves.toBeUndefined();
    expect(events).toEqual([]); // undecryptable → skipped
    expect(states.length).toBe(1); // state still re-folds
  });
});

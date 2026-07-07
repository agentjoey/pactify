// pactify claude cockpit host — a thin (~1 file) Node bridge over the Claude
// Agent SDK. It speaks newline-delimited JSON-RPC 2.0 on stdin/stdout, in the
// SAME envelope shape as the codex app-server backend, so the Go side reuses one
// transport. Launched by pactify's cockpit manager as a serve subprocess
// (`node <abs>/host.mjs`) — NEVER via npx (E0-⑤: npx hangs the handshake).
//
// Protocol (Go ⇄ host):
//   Go→host requests (host replies with `result`):
//     initialize {}                                   -> { ok: true }
//     thread/start { cwd, model?, systemPrompt? }     -> { thread: { id } }
//     thread/resume { threadId }                      -> { thread: { id } }
//     turn/start { threadId, input:[{type:"text",text}] } -> {}
//     turn/interrupt { threadId }                     -> {}
//     session/delete { threadId }                     -> {}
//   host→Go requests (Go replies with `result`):
//     approval/request { toolName, input, title, requestId, danger } -> { decision: "allow"|"deny" }
//   host→Go notifications (events, no reply):
//     cockpit/session { threadId }   // the SDK session id, once known (after 1st turn)
//     cockpit/message { text, final }
//     cockpit/tool    { phase, name, text }
//     cockpit/usage   { inputTokens, outputTokens, totalTokens, costUSD }
//     cockpit/state   { state }
//     cockpit/error   { error }
//
// NB: the Agent SDK only reveals session_id in the message stream, which does not
// begin until the first turn runs. So thread/start replies IMMEDIATELY (the query
// is created and ready for a turn) and the real id arrives later via
// cockpit/session — blocking thread/start on the id would deadlock a Go client
// that must first see the reply before it can send turn/start.

import { query, deleteSession } from "@anthropic-ai/claude-agent-sdk";
import { createInterface } from "node:readline";

// ---- JSON-RPC plumbing -----------------------------------------------------

let nextId = 1;
const pendingToGo = new Map(); // id -> {resolve}

function write(obj) {
  process.stdout.write(JSON.stringify(obj) + "\n");
}
function reply(id, result) {
  write({ jsonrpc: "2.0", id, result });
}
function replyError(id, message) {
  write({ jsonrpc: "2.0", id, error: { code: -32000, message } });
}
function notify(method, params) {
  write({ jsonrpc: "2.0", method, params });
}
// Send a request TO Go and await its result.
function callGo(method, params) {
  const id = `h${nextId++}`;
  return new Promise((resolve) => {
    pendingToGo.set(id, resolve);
    write({ jsonrpc: "2.0", id, method, params });
  });
}

// ---- Session state ---------------------------------------------------------
// One host process hosts exactly one cockpit session (one thread). The Go side
// spawns a fresh host per session, so no multi-session multiplexing here.

let sessionId = null;          // resolved from the first SDK message
let sessionIdReady;            // promise that resolves once sessionId is known
const sessionIdPromise = new Promise((r) => (sessionIdReady = r));
let pushUserMessage = null;    // feeds the streaming-input async iterable
let abort = null;              // AbortController for the running query / turn

// The streaming input: an async generator the SDK pulls user turns from. We
// push into it via `pushUserMessage`; it never ends until Close.
function makeInput() {
  const queue = [];
  let wake = null;
  pushUserMessage = (m) => {
    queue.push(m);
    if (wake) { wake(); wake = null; }
  };
  return (async function* () {
    while (true) {
      if (queue.length) { yield queue.shift(); continue; }
      await new Promise((r) => (wake = r));
    }
  })();
}

// canUseTool → ask Go for an approval decision. Fires only when the SDK's
// permission system would prompt (default mode: dangerous ops), which matches
// the cockpit's danger-grading. RawInput = the full tool input (trust root).
async function canUseTool(toolName, input, opts) {
  const decision = await callGo("approval/request", {
    toolName,
    input,
    title: opts?.title ?? "",
    requestId: opts?.requestId ?? "",
    danger: opts?.decisionReason ?? "",
  });
  if (decision && decision.decision === "allow") {
    return { behavior: "allow", updatedInput: input };
  }
  return { behavior: "deny", message: "denied by pactify cockpit" };
}

// Consume the SDK message stream and translate to cockpit/* notifications.
async function pump(q) {
  try {
    for await (const msg of q) {
      if (msg.session_id && !sessionId) {
        sessionId = msg.session_id;
        sessionIdReady(sessionId);
        notify("cockpit/session", { threadId: sessionId });
      }
      translate(msg);
    }
  } catch (e) {
    if (e?.name !== "AbortError") notify("cockpit/error", { error: String(e?.message ?? e) });
  }
}

function translate(msg) {
  switch (msg.type) {
    case "stream_event": {
      // partial assistant text deltas
      const ev = msg.event;
      if (ev?.type === "content_block_delta" && ev.delta?.type === "text_delta") {
        notify("cockpit/message", { text: ev.delta.text, final: false });
      }
      break;
    }
    case "assistant": {
      // a completed assistant message: surface tool_use starts; final text is
      // covered by the deltas + result, so only emit tool events here.
      for (const block of msg.message?.content ?? []) {
        if (block.type === "tool_use") {
          notify("cockpit/tool", { phase: "start", name: block.name, text: "" });
        }
      }
      break;
    }
    case "user": {
      // tool_result blocks arrive as a synthetic user message
      for (const block of msg.message?.content ?? []) {
        if (block.type === "tool_result") {
          const text = typeof block.content === "string" ? block.content
            : Array.isArray(block.content) ? block.content.map((c) => c.text ?? "").join("") : "";
          notify("cockpit/tool", { phase: "end", name: "", text });
        }
      }
      break;
    }
    case "result": {
      const u = msg.usage ?? {};
      notify("cockpit/usage", {
        inputTokens: u.input_tokens ?? 0,
        outputTokens: u.output_tokens ?? 0,
        totalTokens: (u.input_tokens ?? 0) + (u.output_tokens ?? 0),
        costUSD: msg.total_cost_usd ?? 0,
      });
      notify("cockpit/state", { state: msg.is_error ? "turn_failed" : "turn_completed" });
      break;
    }
  }
}

function startQuery(cwd, model, systemPrompt, resume) {
  abort = new AbortController();
  const options = {
    cwd: cwd || process.cwd(),
    permissionMode: "default",
    includePartialMessages: true,
    canUseTool,
    abortController: abort,
  };
  if (model) options.model = model;
  if (systemPrompt) options.systemPrompt = systemPrompt;
  if (resume) options.resume = resume;
  const q = query({ prompt: makeInput(), options });
  pump(q);
}

// ---- request dispatch ------------------------------------------------------

async function handle(m) {
  const { id, method, params, result, error } = m;

  // A reply to one of OUR requests to Go (approval/request).
  if (id !== undefined && method === undefined) {
    const r = pendingToGo.get(id);
    if (r) { pendingToGo.delete(id); r(error ? null : result); }
    return;
  }

  try {
    switch (method) {
      case "initialize":
        reply(id, { ok: true });
        break;
      case "thread/start": {
        // Reply immediately: the query is created and ready for turn/start. The
        // session id is not known until the first turn produces a message, and it
        // arrives out-of-band via the cockpit/session notification. Blocking here
        // would deadlock a Go client that gates turn/start on this reply.
        startQuery(params?.cwd, params?.model, params?.systemPrompt, null);
        reply(id, {});
        break;
      }
      case "thread/resume": {
        startQuery(params?.cwd, params?.model, params?.systemPrompt, params?.threadId);
        // resumed session keeps its id
        sessionId = params?.threadId ?? null;
        reply(id, { thread: { id: sessionId } });
        break;
      }
      case "turn/start": {
        const text = (params?.input ?? []).map((b) => b.text ?? "").join("");
        if (pushUserMessage) pushUserMessage({ type: "user", message: { role: "user", content: text } });
        reply(id, {});
        break;
      }
      case "turn/interrupt":
        if (abort) abort.abort();
        reply(id, {});
        break;
      case "session/delete":
        try { if (params?.threadId) await deleteSession(params.threadId); } catch { /* best-effort */ }
        reply(id, {});
        break;
      default:
        if (id !== undefined) replyError(id, `unknown method: ${method}`);
    }
  } catch (e) {
    if (id !== undefined) replyError(id, String(e?.message ?? e));
  }
}

const rl = createInterface({ input: process.stdin });
rl.on("line", (line) => {
  const s = line.trim();
  if (!s) return;
  let m;
  try { m = JSON.parse(s); } catch { return; }
  handle(m);
});
rl.on("close", () => process.exit(0));

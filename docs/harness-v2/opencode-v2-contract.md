# opencode 2.0.32 headless contract (for the pactify Go adapter)

> AJ-720 reference. Produced 2026-10-17 by reading the opencode `dev` branch at commit `8c173a8` (all packages 2.0.32) and the OpenAPI document served by the 2.0.32 binary (`GET /openapi.json`; the consumed projection is `internal/cockpit/opencodeapi/openapi-v2.json`). Source paths are relative to the opencode repository root. Nothing here was observed at runtime unless stated; verify on the real binary before relying on an "inferred" point.

Scope: read-only source research of the opencode `dev` clone (all packages at 2.0.32) plus the served
OpenAPI document the served `/openapi.json`. Nothing was executed.

Citation convention: paths are relative to the clone root
(anomalyco/opencode, `dev` @ `8c173a8`), `file:line` or `file:start-end`. Pactify paths are prefixed `pactify:`.
"Inferred" means derived by reading code paths, not observed at runtime. "Not found" means I searched and found
no code for it. Example payloads are **constructed from the schemas**, not captured from a live server.

Headline conclusions are in the last section ("Mapping for the pactify adapter" + "Blockers").

---

## 1. Server lifecycle

### 1.1 `serve` modes and startup output
- Flags: `serve --hostname --port --cors (repeatable) --service --stdio` (`packages/cli/src/commands/commands.ts:550-563`).
  `--service` and `--stdio` together fail: `"--service and --stdio cannot be combined"` (`packages/cli/src/commands/handlers/serve.ts:9`).
- Mode mapping: `service` if `--service`, `stdio` if `--stdio`, else `default` (`handlers/serve.ts:10-15`).
- Startup line, printed with `console.log` (stdout) **after** the server bound and booted:
  - default/service: `server listening on <url>` (`packages/cli/src/server-process.ts:201-202`). Note the v1 prefix
    `opencode server listening on` is gone; pactify's `strings.HasPrefix(..., "opencode server listening on ")`
    (`pactify:internal/cockpit/opencode_server.go:111`) will never match.
  - stdio: a single JSON line `{"url":"http://127.0.0.1:PORT"}` (`server-process.ts:202`).
  - default mode only, when no password came from env: a second line `server password <random>` (`server-process.ts:203`).
- `<url>` is `HttpServer.formatAddress(server.address)` (`server-process.ts:201`), e.g. `http://127.0.0.1:53211`.
- The line is printed only after `start()` returns, and for non-service mode `start()` returns only after the route
  graph is built and `status.ready` is set (`packages/server/src/process.ts:92-133`). So the line means "ready".
- Port: `--port N` binds exactly N (0 = OS-random). Without `--port`, the server tries 4096 and increments on
  `EADDRINUSE` (`packages/server/src/process.ts:142-149`). Hostname default `127.0.0.1` (`server-process.ts:67`,
  `process.ts:55`).
- Lifetime: default mode runs until killed (`Effect.never`); stdio mode exits when **stdin** reaches EOF/close
  (`server-process.ts:204-208`, `waitForStdinClose` `227-240`); service mode waits for its own shutdown.

### 1.2 Does `serve` attach to / spawn the shared background service?
- Only `--service` looks for an incumbent service (`Service.incumbent`) and registers itself
  (`server-process.ts:65-73`, `141-155`); it also `chdir`s to `$HOME` (`server-process.ts:61`) and applies the saved
  service env (`server-process.ts:75`).
- Default and `--stdio` modes never look up, attach to, or spawn the background service (the incumbent lookup is
  guarded by `serviceOptions !== undefined`, `server-process.ts:69-73`).
- Startup recovery of crashed/claimed sessions (`resumeSuspendedSessions`) is installed **only when a lifecycle is
  passed**, which only service mode does (`packages/server/src/process.ts:107-111`, `236-242`). A plain `serve` does
  not auto-resume a session whose previous server was killed mid-turn.
- `opencode run` (not `serve`) does connect to the background service by default; `--standalone` makes it spawn a
  private `serve --stdio --port 0` child (`packages/cli/src/services/server-connection.ts:41-83`,
  `packages/cli/src/services/standalone.ts:17-33`).
- Auto-update: the updater is only driven by the TUI default handler, `upgrade` and `uninstall`
  (`packages/cli/src/commands/handlers/default.ts:62`, `upgrade.ts:15`, `uninstall.ts:20`); `serve` never calls it.
  `OPENCODE_DISABLE_AUTOUPDATE` is read only inside the updater (`packages/cli/src/services/updater.ts:534`).

### 1.3 Official private-server pattern (recommended model)
`packages/cli/src/services/standalone.ts:17-56`: spawn `<self> serve --stdio --port 0` with env
`OPENCODE_PASSWORD=<random>`, `stdin: "pipe"` as an ownership lease, read the first stdout line and decode
`{url}`, authenticate as Basic `opencode:<password>`, keep draining stdout. In stdio mode the server deletes
`OPENCODE_PASSWORD` and `OPENCODE_SERVER_PASSWORD` from its own `process.env` after reading them, "to keep the lease
credential out of the environment inherited by tools" (`server-process.ts:80-85`). In **default** mode they are NOT
deleted, so shell tools inherit the password (same leak pactify's `secretWriter` guards against in v1).

### 1.4 Authentication
- Password env: `OPENCODE_PASSWORD`, falling back to `OPENCODE_SERVER_PASSWORD` (`packages/cli/src/env.ts:10-13`).
  Without either, a random password is generated (`server-process.ts:86-91`).
- Username is fixed to `"opencode"` (`packages/server/src/auth.ts:20`). `OPENCODE_SERVER_USERNAME` is **not read**
  anywhere (not found in source).
- Credentials accepted: `Authorization: Basic base64(opencode:pw)`, or query `?auth_token=base64(opencode:pw)`, or a
  signed session cookie (`packages/server/src/middleware/authorization.ts:12`, `32-39`, `58-71`).
- 401 body: `{"_tag":"UnauthorizedError","message":"Authentication required"}` (+ `www-authenticate` for non-browser
  clients) (`middleware/authorization.ts:41-56`).

### 1.5 Environment variables read (server side)
From `packages/cli/src/server-process.ts:36-140`, `packages/cli/src/database-path.ts`, `packages/util/src/global*.ts`:

| Variable | Effect | Source |
|---|---|---|
| `OPENCODE_PASSWORD` / `OPENCODE_SERVER_PASSWORD` | server password | `cli/src/env.ts:10-13` |
| `OPENCODE_CONFIG` | explicit config file | `server-process.ts:127` |
| `OPENCODE_CONFIG_CONTENT` | inline config document (highest non-managed priority, see 6.6) | `server-process.ts:128` |
| `OPENCODE_CONFIG_DIR` | **replaces** the global config dir (`$XDG_CONFIG_HOME/opencode`) | `server-process.ts:38`, `118`; `server/src/routes.ts:124`; `util/src/global.ts:79` |
| `OPENCODE_DISABLE_PROJECT_CONFIG` / `OPENCODE_CONFIG_PROJECT_DISABLE` | disables project config walk **and** project AGENTS.md discovery | `server-process.ts:119-121`; `server/src/routes.ts:126-133` |
| `OPENCODE_DISABLE_CLAUDE_CODE` / `OPENCODE_DISABLE_CLAUDE_CODE_SKILLS` | skip `~/.claude` and project `.claude` (only used for skills) | `server-process.ts:122-126`; `core/src/config.ts:53` |
| `OPENCODE_DISABLE_MODELS_FETCH`, `OPENCODE_MODELS_URL`, `OPENCODE_MODELS_PATH` | models.dev catalog fetch/override | `server-process.ts:112-116` |
| `OPENCODE_DISABLE_FILEWATCHER` / `OPENCODE_FILEWATCHER_DISABLE`, `OPENCODE_DISABLE_FFF` | fs watcher / search backend | `server-process.ts:133-139` |
| `OPENCODE_DB`, `OPENCODE_DISABLE_CHANNEL_DB` | DB filename (absolute path allowed via `path.resolve`) | `cli/src/database-path.ts:4-13` |
| `OPENCODE_SIMULATE`, `OPENCODE_CLIENT`, `OPENCODE_PTY_HANDOFF`, `OPENCODE_GIT_BASH_PATH` | misc | `server-process.ts:48-49`, `99`, `108`, `131` |
| `XDG_DATA_HOME`, `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME` | roots of all global paths | `util/src/global-roots.ts:5-18` |
| `OPENCODE_TEST_HOME` | overrides `os.homedir()` for `global.home` | `util/src/global.ts:16-18` |
| `OPENCODE_DISABLE_AUTOUPDATE` | only the CLI updater (not used by serve) | `cli/src/services/updater.ts:534` |
| `OPENCODE_PURE` | **not found** in v2 source | — |
| `OPENCODE_DISABLE_EXTERNAL_SKILLS` | **not found** in v2 source | — |
| Provider keys from models.dev `env` lists, e.g. `MINIMAX_API_KEY`, `DEEPSEEK_API_KEY`, `OPENCODE_API_KEY` | honoured as connections (see 10) | `core/src/plugin/models-dev.ts:43-57`; `core/src/integration.ts:405-408`, `723-727` |

### 1.6 Files written / read outside XDG
Global paths (`packages/util/src/global.ts:15-27`, `66-72`): `data=$XDG_DATA_HOME/opencode`,
`config=$XDG_CONFIG_HOME/opencode` (or `OPENCODE_CONFIG_DIR`), `state=$XDG_STATE_HOME/opencode`,
`cache=$XDG_CACHE_HOME/opencode`, `bin=cache/bin`, `log=data/log`, `repos=data/repos`, `tmp=os.tmpdir()/opencode`.
All of data/config/state/log/bin/repos/tmp are `mkdir -p`'d at startup (`global.ts:66-72`).
- DB: `data/opencode.db` for channels latest/dev/beta/next/prod, else `opencode-<channel>.db` (`cli/src/database-path.ts:4-13`).
- Snapshots (git object store per project): `data/snapshot/<projectID>/<hash>` (`core/src/snapshot.ts:95-97`).
- Tool output spill: `data/tool-output` (`core/src/tool-output.ts:16`, `55`).
- **Outside XDG**: `os.tmpdir()/opencode` (created; also advertised to the model as approved scratch space,
  `core/src/instructions/builtins.ts:43`); `~/.opencode/plan/*` is the only path the `plan` agent may edit
  (`core/src/plugin/plan.ts:31`, `39-40`) — written only if the model writes a plan file (no mkdir found);
  reads of `~/.claude`, `~/.agents` (config/skills, `core/src/config/discovery.ts:69-70`, `129-143`), the system dir
  `/Library/Application Support/opencode` and macOS MDM plists (`core/src/config/discovery.ts:27-41`; loaded last per `core/src/config.ts:246-252`),
  and `<repo>/.git/opencode` (cached project id, read only — `core/src/project.ts:274-279`; no write found).
  `global.home` = `OPENCODE_TEST_HOME ?? os.homedir()` (`global.ts:16-18`), i.e. `$HOME`, not XDG.

### 1.7 `location.directory` vs server cwd
- Every session has `location: {directory}` stored at creation. `POST /api/session` uses `payload.location` or, when
  absent and no `parentID`, **`process.cwd()` of the server** (`packages/server/src/handlers/session.ts:103-105`).
  Children inherit the parent's location (`core/src/session.ts:286-288`).
- Session-scoped routes resolve services from the session's stored location
  (`server/src/middleware/session-location.ts:22-27`). Non-session location-scoped routes (models, providers,
  permission request list, ...) use query `location[directory]=<abs>` or header `x-opencode-directory`
  (URI-encoded), defaulting to server cwd (`server/src/location.ts:72-77`). The v1 `?directory=` query param is
  not read (pactify sends it on every call: `pactify:internal/cockpit/opencode_server.go:202`).
- Config files, AGENTS.md, permissions and tools are per-location instances (`specs/v2/session.md:55`).

---

## 2. Event stream `GET /api/event`

### 2.1 Transport
- Raw SSE, one global feed across all locations; no query parameters, no location/session filtering
  (`packages/protocol/src/groups/event.ts:44-52`; rationale `specs/v2/event-stream-architecture.md:45`).
- Framing: each event is exactly `data: <JSON>\n\n` — **no `event:` and no `id:` lines**
  (`packages/server/src/event-feed.ts:29-31`). The served OpenAPI advertises a generic `{id?, event, data}` SSE item
  (`v2-openapi.json` `/api/event` 200) but the raw handler bypasses it (`server/src/handlers/event.ts:11-33`).
- First frame on every connection: `{"id":"evt_…","type":"server.connected","data":{}}`
  (`handlers/event.ts:13-19`). Events published before the subscription is registered are missed
  (`event-stream-architecture.md:145-153`), so subscribe, wait for `server.connected`, then prompt (this is what
  `opencode run` does, `packages/cli/src/run/noninteractive.ts:78-80`).
- Heartbeat: SSE comment `: heartbeat\n\n` every 15 s (`handlers/event.ts:21`).
- Overflow: each connection has a 4,096-frame dropping queue; overflow fails only that connection
  (`server/src/event-feed.ts:8`, `64-68`; `event-stream-architecture.md:49-62`). An encoding failure fails all
  current connections (`event-feed.ts:41-62`).
- Reconnect/replay: **none** ("Volatile by contract … events during disconnection are missed",
  `protocol/src/groups/event.ts:51`). `Last-Event-ID` is not read (not found).
- Durable per-session replay exists separately: `GET /api/experimental/session/{id}/log?after=<seq>&follow=true`
  (SSE of durable session events plus one `{"type":"log.synced","aggregateID","seq?"}` marker)
  (`protocol/src/groups/session.ts:647-665`, `schema/src/event-log.ts:12-20`, `specs/v2/session.md:106-112`).
  Live-only deltas (text/reasoning/tool-input/compaction deltas, usage updates) are not in this log
  (`specs/v2/session.md:110`). Its exact SSE framing is produced by effect's `StreamSse` encoder (not read; unverified).

### 2.2 Envelope
Durable events: `{id:"evt_…", created:<ms>, metadata?, type, durable:{aggregateID, seq, version}, location?, data}`;
ephemeral: same without `durable` (`packages/schema/src/event.ts:108-116`, `133-140`). Filter by
`data.sessionID`; there is no top-level sessionID.

### 2.3 Event catalogue (public; `schema/src/event-manifest.ts:41-84`, `protocol/src/groups/event.ts:67-68`)
All session events are in `packages/schema/src/session-event.ts`. `SessionError.Error` =
`{type: string, message: string, status?: 100..599, response?: {body: string}}` (`schema/src/session-error.ts:7-12`).
`TokenUsage.Info` = `{input, output, reasoning, cache:{read, write}}` (`schema/src/token-usage.ts:6-14`).
`Model.Ref` = `{id, providerID, variant?}` (`schema/src/model.ts:19-23`).

| Area | type | data fields | source |
|---|---|---|---|
| session | `session.created` | `sessionID, projectID, location{directory,workspaceID?}, subpath?, parentID?, slug, title?, agent?, model?, metadata?, permissions?, version` | `session-event.ts:48-66` |
| | `session.agent.selected` | `sessionID, agent, previous?` | `:69-77` |
| | `session.model.selected` | `sessionID, model, previous?` | `:80-88` |
| | `session.renamed` / `session.metadata.updated` / `session.permissions` | `title` / `metadata` / `permissions` | `:101-129` |
| | `session.deleted` | `sessionID` | `:177-184` |
| | `session.forked`, `session.moved`, `session.viewed` | see source | `:91-99`, `:131-140`, `:187-200` |
| input | `session.inbox.enqueued` | `sessionID, inboxID, item{type:user/synthetic/compaction/move, payload, delivery}` | `:215-222` |
| | `session.inbox.delivered` | `sessionID, inboxID` (the prompt's `id`) | `:208-212` |
| | `session.inbox.cancelled`, `session.inbox.delivery.changed` | | `:225-237` |
| | `session.synthetic` | `sessionID, text, description?, metadata?` (subagent/shell notifications etc.) | `:279-288` |
| execution (turn) | `session.execution.started` | `sessionID` | `:240` |
| | `session.execution.succeeded` | `sessionID` | `:243` |
| | `session.execution.failed` | `sessionID, error` | `:246-250` |
| | `session.execution.interrupted` | `sessionID, reason: "user"\|"shutdown"\|"superseded"\|"inactivity"` | `:253-257` |
| step (one model call) | `session.step.started` | `sessionID, assistantMessageID, agent, model (Model.Ref = model actually used), snapshot?, started` | `:329-341` |
| | `session.step.streamed` | `sessionID, assistantMessageID` | `:345-352` |
| | `session.step.ended` | `sessionID, assistantMessageID, finish ("stop"\|"length"\|"tool-calls"\|"content-filter"\|"error"\|"unknown"), rawFinish?, providerState?, cost, tokens, snapshot?, files?` | `:355-369`, `schema/src/llm.ts:5` |
| | `session.step.failed` | `sessionID, assistantMessageID, error, finish?:"content-filter", rawFinish?, providerState?, cost?, tokens?, snapshot?, files?` | `:372-387` |
| | `session.retry.scheduled` | `sessionID, assistantMessageID, attempt, at, error` | `:569-579` |
| text | `session.text.started` | `sessionID, assistantMessageID, ordinal` | `:392-400` |
| | `session.text.delta` (ephemeral) | `…, ordinal, delta` | `:404-412` |
| | `session.text.ended` | `…, ordinal, text (full), state?` | `:415-426` |
| reasoning | `session.reasoning.started/.delta/.ended` | same shape as text | `:429-466` |
| tools | `session.tool.input.started` | `sessionID, assistantMessageID, id (call id, unique per step), name` | `:476-484` |
| | `session.tool.input.delta` (ephemeral) / `session.tool.input.ended` | `delta` / `text` (raw input JSON text) | `:487-504` |
| | `session.tool.called` | `…, id, input (decoded object), executed, state?` | `:507-516` |
| | `session.tool.progress` (ephemeral) | `…, id, metadata` (replacement metadata; subagent puts `{sessionID: child, status:"running"}`) | `:520-527`; `core/src/tool/plugin/subagent.ts:194` |
| | `session.tool.success` | `…, id, content: [{type:"text",text}\|{type:"file",uri,mime,name?}], metadata?, executed, resultState?` | `:530-544`; `schema/src/tool.ts:67-83` |
| | `session.tool.failed` | `…, id, error, content?, metadata?, executed, resultState?` | `:551-566` |
| usage | `session.usage.updated` (ephemeral) | `sessionID, cost, tokens` — **cumulative session totals** re-read from DB after every step end/fail/usage record | `:167-174`; `core/src/session/projector.ts:82-111`, `794-805` |
| compaction | `session.compaction.started/.delta/.ended/.failed` | see source (ended/failed carry cost/tokens) | `:582-636` |
| instructions | `session.instructions.updated` | `delta, text?` | `:261-276` |
| skill/shell | `session.skill.activated`, `session.shell.started/.ended` | | `:291-326` |
| revert | `session.revert.staged/.cleared/.committed` | | `:638-650` |
| permission | `permission.asked` (ephemeral) | `id ("per_…"), sessionID, action, resources[], save?[], metadata?, source?{type:"tool", messageID, id}, message?` | `schema/src/permission.ts:26-45` |
| | `permission.replied` (ephemeral) | `sessionID, requestID, reply: "once"\|"always"\|"reject"` | `permission.ts:46-53` |
| forms | `form.created` / `form.replied` / `form.cancelled` | `{form: Form.Info}` / `{id, sessionID, answer}` / `{id, sessionID}` (question tool uses forms) | `schema/src/form.ts:173-175` |
| legacy | `session.status`, `session.idle` | in the public schema but **never published by v2 core** (no publisher found) | `schema/src/session-status-event.ts:35-51` |

Additional events **observed in real 2.0.32 recordings by the host** (2026-10-18):

| Event | Adapter handling / known shape |
| --- | --- |
| `permission.rejected` | Tolerated as an informational owned-session event; not a terminal execution event. |
| `shell.created` | `{info:{id, command, pid?, metadata:{sessionID?}, ...}}`; correlate via `info.metadata.sessionID`. Actual 2.0.32 recordings omit pid despite its optional schema field. Recording cancellation waits for this spawn event, snapshots live private-server descendants and verifies exit after interrupting. `schema/src/shell.ts:38-58`. |
| `shell.exited` | `{id, exit?, status}`; correlate with the preceding owned `shell.created`. `schema/src/shell.ts:59`. |
| `integration.updated`, `provider.updated`, `model.updated` | Tolerated global inventory updates; ignored by job-local event filtering. They do not prove prompt completion. |

Readonly safety correction (AJ-720 B1): shell resource evaluation alone does not prove shell execution safety. Redirect-only statements can produce an empty command list (`core/src/shell/parse.ts:173-198`), bypassing `permission.assert` (`core/src/tool/plugin/shell.ts:140-148`). The adapter therefore removes readonly shell entirely using a final wildcard deny (`core/src/tool.ts:231,292-295`); it retains read/glob/grep only. No inference-free session-tool inventory API was identified in the inspected protocol groups. In the subsequent 2.0.32 live check, the host confirmed the model saw only glob/grep/read and redirect-only `> marker.tmp` created no file.

Removed v1 events used by pactify: `message.updated`, `message.part.updated`, `message.part.delta`,
`session.error` — not in the v2 manifest (not found in `schema/src`).

Example (constructed):
```
data: {"id":"evt_0199…","created":1760750000000,"type":"session.tool.success","durable":{"aggregateID":"ses_…","seq":42,"version":2},"data":{"sessionID":"ses_…","assistantMessageID":"msg_…","id":"call_1","content":[{"type":"text","text":"…"}],"metadata":{"truncated":true,"outputPath":"/…/tool-output/tool_…"},"executed":true}}
```

### 2.4 Errors, retries, overflow on the stream
- Provider failures become `SessionError` with `type` (`core/src/session/to-session-error.ts:28-60`, `97-104`):
  `provider.rate-limit`, `provider.auth`, `provider.quota`, `provider.content-filter`, `provider.transport`,
  `provider.internal`, `provider.invalid-output`, `provider.invalid-request` (also used for **context overflow**),
  `provider.unsupported-operation`, `provider.no-route` (also model not selected / unavailable / bad variant,
  `to-session-error.ts:74-83`), `provider.unknown`, `provider.timeout`; plus `permission.rejected`,
  `tool.execution`, `unknown`; step/tool aborts use `aborted`. `status` = provider HTTP status when known,
  `response.body` = raw body.
- Classification (`packages/ai/src/provider-error.ts:235-300`): context overflow = 4xx with
  `context_length_exceeded`-type codes/text; quota = 402, quota codes, or 429 + quota text; auth = 401/403/auth
  codes; rate-limit = 429/rate codes; internal = 408/409/5xx.
- There is **no `isRetryable` flag** in `SessionError`. Retry is done inside the server: rate-limit, provider-internal,
  transport and unknown failures get up to 4 retries with backoff (`specs/v2/session.md:82-86`,
  `ai/src/provider-error.ts:77-95`); each produces `session.retry.scheduled`. What reaches
  `session.execution.failed` is already final.
- Context overflow: one automatic compaction + rebuild of the step, then terminal (`specs/v2/session.md:104`).

### 2.5 "My turn is finished"
The reliable terminal signal is the first **`session.execution.succeeded | failed | interrupted`** for the session
after the prompt was admitted (`core/src/session/execution.ts:119-147`: exactly one terminal per busy period).
Caveats:
- A busy period may already be running (a prompt with `delivery:"steer"` joins it, `specs/v2/session.md:18`); pactify
  prompts idle sessions so this is the normal case.
- Errors before the input is promoted (e.g. no model selected, blocked instructions) end with
  `session.execution.failed` **without** `session.inbox.delivered` for the prompt id (`run` handles this as
  `prePromotionError`, `cli/src/run/noninteractive.ts:239-264`, `777-782`).
- A user decline (reply `reject` without `message`) interrupts the step via `Effect.interrupt`
  (`core/src/session/runner/step.ts:274-276`); the coordinator then has no reason and reports
  `reason:"shutdown"` (`execution.ts:50-53`, `run-coordinator.ts:104`) — inferred; `opencode run` relies on this
  (`noninteractive.ts:537-538`). With `reason:"shutdown"` no idle marker is written and the execution claim is kept
  (`core/src/session/message-updater.ts:148`, `projector.ts:421`, `execution.ts:129-135`).
- Background subagents/shells finish later and post a `session.synthetic` that **wakes the parent again**
  (new `execution.started … succeeded`) (`core/src/session/subagent-completion.ts:37-44`, `tool/plugin/shell.ts:157-176`).
- Server-side barrier: `POST /api/experimental/session/{id}/wait` returns 204 once the process owns no active
  execution (`protocol/src/groups/session.ts:441-454`, `core/src/session/session.ts:265-268`); `opencode run` races
  it against the stream (`noninteractive.ts:763-765`).
- History marker: every non-shutdown terminal appends `{type:"idle", outcome, error?}` to the message list
  (`schema/src/session-message.ts:289-295`, `core/src/session/message-updater.ts:63-77`, `144-148`).

---

## 3. Prompting

- `POST /api/session/{id}/prompt` body: `{id?: "msg_…", text, files?: [{uri, name?, description?, mention?}],
  agents?: [{name, mention?}], skills?: [{id, mention?}], metadata?, delivery?: "steer"|"queue", resume?: boolean}`;
  response `{data: Session.Inbox.User}` = `{id, sessionID, time:{created}, type:"user", payload, delivery}`
  (`protocol/src/groups/session.ts:317-336`, `schema/src/prompt-input.ts:9-34`, `schema/src/session-inbox.ts:52-59`).
  Errors: 409 `ConflictError` (id reused with different content type), 400 `InvalidRequestError` (bad attachment,
  unknown skill), 404 `SessionNotFoundError`.
- Asynchronous: the handler durably admits the input (inbox row) and, unless `resume:false`, calls
  `execution.wake` and returns immediately (`core/src/session/session.ts:145-178`; `specs/v2/session.md:5-21`).
  Execution starts one tick later (`run-coordinator.ts:96`). Default delivery is `steer`
  (`session.ts:155`). Reusing the same `id` is idempotent (`specs/v2/session.md:9`) — useful for retrying a prompt POST.
- Client-generated id: must start with `msg_` (`schema/src/session-message.ts:23-30`); server ids are
  `msg_` + 12 hex time + 14 base62 random (`schema/src/identifier.ts:16-31`). Session ids start with `ses`
  (`schema/src/session-id.ts:5`).
- **No `agent`, `model`, `variant` or `system` in the prompt body.** Set them on the session:
  - at creation: `POST /api/session {agent?, model?:{id, providerID, variant?}, …}` (`protocol/src/groups/session.ts:142-163`);
  - later: `POST /api/session/{id}/agent {agent}` and `POST /api/session/{id}/model {model:{id, providerID, variant?}}`,
    both 204, effective for subsequent provider turns (`session.ts:251-281`). `opencode run` calls both before every
    prompt (`cli/src/run/noninteractive.ts:690-709`).
  - Agents: `build` (default), `plan`, plus subagents `general`, `explore` and hidden `compaction`, `title`,
    `summary` (`core/src/agent.ts:13`, `core/src/plugin/agent.ts:80-140`, `core/src/plugin/plan.ts:34-41`).
- System prompt / extra instructions per session:
  - `PUT /api/experimental/session/{id}/instructions/entries/{key}` body `{value: <JSON>}` (key
    `^[a-z0-9][a-z0-9._-]*$`, value ≤ 256 KiB) (`protocol/src/groups/session.ts:588-607`,
    `schema/src/instruction-entry.ts:10-40`). Rendered as `<context key="KEY">VALUE</context>` (strings verbatim)
    (`core/src/session/instruction-entry.ts:83-86`). If set before the first step it is part of the initial
    instruction baseline, which is appended to the **system** parts after the agent/default system prompt
    (`core/src/session/model-request.ts:100-119`; `specs/v2/session.md:92-94`). Later changes are announced as a
    chronological System message. Experimental endpoint.
  - Alternative: a custom agent via config (`agents.<id>.system`) **replaces** the default system prompt
    (`model-request.ts:111-113`; `core/src/config/plugin/agent.ts:115`).
- Which assistant messages belong to my prompt: the user message projected on delivery has id = prompt `id`
  (`opencode run` locates it by `message.id === messageID`, `noninteractive.ts:550-566`); all assistant messages
  after it up to the next `idle` marker belong to that turn (`schema/src/session-message.ts:284-295`). On the stream,
  `session.inbox.delivered{inboxID:<prompt id>}` marks promotion; subsequent `session.step.*` carry
  `assistantMessageID`.

---

## 4. Messages / history

- `GET /api/session/{id}/message?limit=1..200&order=asc|desc&cursor=…&type=…` → `{data: Message.Info[],
  cursor:{previous?, next?}}` (`protocol/src/groups/message.ts:8-87`). Default order `desc`, default limit 50
  (`server/src/handlers/message.ts:9`, `41-46`). `cursor` and `order` cannot be combined (400). `cursor.next` is
  returned whenever the page is non-empty (`handlers/message.ts:53-61`), so stop on an empty page.
  `type` filter enum excludes `idle` (`message.ts:23-35`) while unfiltered results include idle markers (`:52-64`).
- `GET /api/session/{id}/message/{messageID}` → one message (`protocol/src/groups/session.ts:704-715`).
- Message union `type`: `agent-switched | model-switched | location-switched | user | synthetic | system | skill |
  shell | assistant | compaction | idle` (`schema/src/session-message.ts:297-309`).
- `Session.Message.Assistant` (`schema/src/session-message.ts:212-237`):
  `{id, metadata?, type:"assistant", agent, model:{id, providerID, variant?}, content:[…], snapshot?,
  finish?, rawFinish?, providerState?, cost?, tokens?, error?: SessionError, retry?:{attempt, at, error},
  time:{created, streamed?, completed?}}`. Content items:
  - `{type:"text", text, state?}` (`:177-182`)
  - `{type:"reasoning", text, state?, time?}` (`:184-193`)
  - `{type:"tool", id, name, executed?, providerState?, providerResultState?, state, time:{created, ran?, completed?}}`
    with `state` one of `{status:"streaming", input:string}`, `{status:"running", input, metadata}`,
    `{status:"completed", input, content[], metadata?}`, `{status:"error", input, error, content?, metadata?}`
    (`:126-175`).
- The v1 `{info, parts}` wrapper, `providerID/modelID` top-level fields, and `name/data` errors are gone.
- Final text of a turn: concatenate `content[type=="text"].text` of the turn's last completed assistant message (what
  v2 itself does for subagent results: `core/src/session/subagent-completion.ts:10-19`; and `run`'s reconcile,
  `noninteractive.ts:568-595`). Timestamps are epoch-ms numbers on the wire (`DateTimeUtcFromMillis`).
- Durable history replay: `GET /api/experimental/session/{id}/log` (section 2.1); export:
  `GET /api/experimental/session/{id}/export`.

---

## 5. Usage

- `Session.Info.tokens` / `.cost` are cumulative for **that session only**: incremented on `session.step.ended`,
  `session.step.failed` (when it carries usage) and internal `session.usage.recorded` (title/compaction)
  (`core/src/session/projector.ts:58-74`, `616`, `700-717`; `schema/src/session-event.ts:155-165`). Only
  `event.data.sessionID` is updated — **children are not rolled up**.
- Title generation usage counts toward the root session; it runs only for untitled root sessions
  (`core/src/session/runner/llm.ts:177`), so
  creating with `title` avoids it.
- Child usage: enumerate children (`GET /api/session?parentID=<id>`, recurse) and read each `GET /api/session/{id}`,
  or sum `session.usage.updated` (latest totals per sessionID) / `session.step.ended` deltas from the stream.
- `GET /api/experimental/session/stats` aggregates usage over time ranges (`protocol/src/groups/session.ts:123-140`).
- Per-step usage: `session.step.ended.data.{cost,tokens}`; cost computed from model price table
  (`core/src/session/usage.ts:22-36`).

---

## 6. Permissions

### 6.1 Format
`Permission.Rule = {action: string, resource: string, effect: "allow"|"deny"|"ask"}`, ruleset = array
(`schema/src/permission.ts:56-67`). Glob matching: `*` → `.*`, `?` → `.`, anchored, dotall; a trailing `" *"`
also matches the bare prefix (`"git status *"` matches `git status`) (`core/src/util/wildcard.ts:3-14`).

### 6.2 Evaluation (last match wins)
- `evaluate(action, resource, ...rulesets)` = `rulesets.flat().findLast(match action && match resource)`, default
  `ask` (`core/src/permission.ts:87-97`). Session docs agree: "Evaluated after the agent's rules; the last matching
  rule wins" (`schema/src/session.ts:57`).
- Effective rules = `agent.permissions ++ session.permissions` (`permission.ts:158-163`); unknown agent ⇒
  `[{*,*,deny}]` (`:19`).
- `deny` is decided first on agent+session rules (any resource denied ⇒ deny); saved "always" approvals are appended
  only afterwards and can turn `ask` into `allow` but cannot override `deny` (`permission.ts:165-189`). Multiple
  resources: any `ask` ⇒ ask (`:177-178`). Plugins may hook the final effect (`:179-188`).
- Agent rules are built as: default agent `[{*,*,allow}, {read,*.env,ask}, {read,*.env.*,ask},
  {read,*.env.example,allow}]` (`schema/src/agent.ts:39-52`) ++ built-in additions (build: `question allow`;
  plan: `question allow, edit * deny, edit ~/.opencode/plan/* allow`; explore: `* deny` then shell/grep/glob/
  webfetch/websearch/read allow …; general: `question deny, subagent deny`) (`core/src/plugin/agent.ts:80-118`,
  `core/src/plugin/plan.ts:34-41`) ++ **config `permissions` of all config documents**, appended to every agent
  (`core/src/config/plugin/agent.ts:83-92`) ++ per-agent config permissions (`:121-123`). Built-in agent plugins are
  "pre" plugins and the config agent plugin is a "post" plugin (`core/src/plugin/internal.ts:223-224`, `252-255`),
  so config rules land after built-in rules (inferred from plugin order).
- Precedence therefore (later wins): built-in agent defaults < config (in config load order, 6.6) < session rules.
  **v2 default for `build` is allow-everything** (except `.env` reads → ask).
- A tool whose action's last matching rule is `{action, "*", deny}` is removed from the model's tool list entirely
  (`core/src/tool.ts:228-232`, `292-295`).

### 6.3 Action names and resources
| action | resources | source |
|---|---|---|
| `read` | path relative to the location dir (`"."` for root), or absolute path if outside | `core/src/file-access.ts:93-104`, `144-161`; tool `core/src/tool/plugin/read.ts:15` |
| `glob` / `grep` | the pattern(s) | `tool/plugin/glob.ts:15`, `:69`; `tool/plugin/grep.ts:15`, `:88`; `specs/v2/tools.md:103-115` |
| `edit` | used by edit, write, patch tools (v1 `write`/`patch` keys map to `edit`) | `tool/plugin/edit.ts:276`, `write.ts:79`, `patch.ts:197`; `core/src/v1/config/migrate.ts:28-33` |
| `shell` (v1 `bash`) | **one resource per simple command** parsed with tree-sitter; resource is the command node text, or the whole redirected statement if redirected (`ls > f`); `save` = `"<prefix> *"` using an arity table | `tool/plugin/shell.ts:23`, `124-147`; `core/src/shell/parse.ts:173-203` |
| `webfetch`, `websearch` | url / query | `tool/plugin/webfetch.ts:12`, `websearch.ts:12` |
| `subagent` (v1 `task`) | the target agent id | `tool/plugin/subagent.ts:17`, `138-151` |
| `external_directory` | `"<dir>/*"` of any path outside location+project | `core/src/file-access.ts:106-142` |
| `question`, `skill`, `execute` (code mode), MCP actions | | `tool/plugin/question.ts:65`, `skill.ts:52`, `tool.ts:236` |
| `list` | **no such tool/action in v2** (no `list` tool registered; tool names `core/src/plugin/internal.ts:231-248`) | — |

Shell notes: `cd`/`pushd`… are not permission-checked as commands, their target dirs go through
`external_directory` (`shell/parse.ts:186-189`, `tool/plugin/shell.ts:130-136`). Pipelines/`;`/`&&` split into
separate resources, so v1 deny patterns like `*|*`, `*;*`, `*&*` no longer match anything; each piece must pass on
its own. Command substitution: the outer command text still contains `$(…)`, inner commands are separate
`command` nodes (inferred from `descendantsOfType("command")`, `parse.ts:178`). A "portable" scanner is used when
the wasm parser is absent or on local/dev channels (`parse.ts:144-152`, `tool/plugin/shell.ts:126-128`) — behaviour of
that scanner not read (unverified).

### 6.4 What happens on deny / reject
- Rule `deny` ⇒ `Permission.BlockedError` ⇒ built-in tools map it to a model-visible `ToolFailure` (e.g. shell:
  "Unable to execute command: …", `tool/plugin/shell.ts:274-277`); the tool call fails with
  `session.tool.failed`, the **model continues** (`specs/v2/tools.md:158-163`; `to-session-error.ts:61`). No
  `continue_loop_on_deny` is needed; the v1 option is listed as unsupported and dropped
  (`core/src/config/normalize.ts:46-52`).
- Interactive `ask` ⇒ `permission.asked` event; the tool fiber waits (`permission.ts:246-260`).
  - reply `once` ⇒ proceeds; `always` ⇒ proceeds and saves `save[]` patterns as project-level allow rules in the DB,
    auto-approving other pending matching asks (`permission.ts:299-322`, saved list `GET /api/permission/saved`);
  - `reject` **with `message`** ⇒ `CorrectedError` ⇒ tool failure with that text, model continues; all other
    pending asks of the same session are rejected with the same feedback (`permission.ts:276-296`);
  - `reject` **without message** ⇒ `DeclinedError` (a defect on purpose) ⇒ the whole step is interrupted
    (`model-request.ts:396-416`, `runner/step.ts:207-216`, `274-276`) ⇒ execution ends interrupted (see 2.5).
  - `opencode run` without `--auto` replies `reject` + message *"This non-interactive run cannot ask the user for
    permission, so the request was rejected. Continue without this action."*; with `--auto`/`--yolo` replies `once`
    (`cli/src/run/noninteractive.ts:73-74`, `158-186`).

### 6.5 Request / reply API
- Pending for a session: `GET /api/session/{sid}/permission` → `{data: Permission.Request[]}`
  (`protocol/src/groups/permission.ts:89-100`); location-wide: `GET /api/permission/request?location[directory]=…`
  → `{location, data}` (`:23-35`). Pending state is in memory only (`core/src/permission.ts:130`, `327-337`).
- Reply: `POST /api/session/{sid}/permission/{rid}/reply` body `{decision: "once"|"always"|"reject",
  message?: string|null}` → 204; 404 `PermissionNotFoundError` if the request is gone or belongs to another session
  (`protocol/src/groups/permission.ts:117-…`, `server/src/handlers/permission.ts:76-85`). The v1 field was `reply`.
- Non-blocking evaluation for external callers: `POST /api/session/{sid}/permission` → `{data:{id, effect}}`.

### 6.6 Setting rules (creation, resume, config)
- At creation: `POST /api/session {permissions:[…]}` (`protocol/src/groups/session.ts:142-152`). Children created by
  the subagent tool get no explicit permissions and therefore **inherit the parent's session rules**
  (`core/src/session.ts:294-297`, `tool/plugin/subagent.ts:182-190`).
- On resume: `PATCH /api/session/{id} {permissions:[…]}` → 204, publishes `session.permissions`; the projector
  **replaces** the stored ruleset (`core/src/session/session.ts:82-88`, `projector.ts:593-598`). (Pactify's v1
  comment "PATCH appends" no longer holds.) Existing children keep their own snapshot.
- Config: native `permissions: [{action,resource,effect}]`; v1 `permission` (string or `{key: effect|{pattern:
  effect}}`) and v1 `tools` booleans are migrated with key renames (`normalize.ts:179-183`, `522-560`;
  `v1/config/migrate.ts:28-33`). Config document order (later wins): managed policies, wellknown, global dir,
  `OPENCODE_CONFIG`, project `opencode.json[c]` files, project `.opencode/` dirs, **`OPENCODE_CONFIG_CONTENT`**,
  managed system dir + MDM (`core/src/config.ts:226-290`).

---

## 7. Interrupt / cancel

- `POST /api/session/{id}/interrupt[?resume=true|false]` → `{interrupted: boolean}` (true iff an active execution owned
  by this process was interrupted; idle = no-op false; unknown session = 404) (`protocol/src/groups/session.ts:667-686`,
  `server/src/handlers/session.ts:587-591`, `specs/v2/session.md:64`). Acknowledges immediately; cleanup is
  asynchronous (`core/src/session/run-coordinator.ts:147-167`).
- Emits `session.execution.interrupted {reason:"user"}` and an idle marker with `outcome:"interrupted"`
  (`execution.ts:128-136`, `message-updater.ts:148`). Unsettled tools/assistant are failed with `aborted`
  (`runner/step.ts:207-224`). Pending inbox input is not deleted (`specs/v2/session.md:62`).
- Running tools: the drain fiber is interrupted; a foreground shell's `onInterrupt` cancels its job, closing the job
  scope which kills the child (spawned `detached`, `forceKillAfter: 3s`) (`tool/plugin/shell.ts:260-262`,
  `core/src/shell.ts:299-306`, `401`, `425-428`). A foreground subagent's `onInterrupt` interrupts the child session
  (`tool/plugin/subagent.ts:235-239`). `jobs.cancel(sessionID)` is called only for the job whose id equals this
  session id (i.e. when the interrupted session is itself a subagent child) (`execution.ts:130`, `core/src/job.ts:411-439`).
  **Background** shells/subagents are not cancelled by the parent's interrupt (inferred: no code path found).
- `?resume=true` re-wakes pending steering input after interruption (`execution.ts:153-170`).
- Deleting a session (`DELETE /api/session/{id}`) interrupts it, awaits idle, then deletes it and all children
  (`core/src/session.ts:376-386`).

---

## 8. Subagents / child sessions

- The `subagent` tool (input `{agent, description, prompt, model?, sessionID?, background?}`) creates a child via
  `sessions.create({parentID, title: description, agent, model})` and prompts it; depth limit
  `experimental.subagent_depth` (default 1) (`tool/plugin/subagent.ts:29-49`, `109-207`).
- Children are ordinary sessions with `parentID`; they appear on the **same global `/api/event` stream**
  (`session.created` carries `parentID`; all their step/tool/permission events carry the child `sessionID`).
  `opencode run` treats any session whose parent chain reaches the root as "owned" for permissions/forms
  (`noninteractive.ts:141-156`).
- Listing: `GET /api/session?parentID=<id>` (`parentID=null` = roots) (`protocol/src/groups/session.ts:89-121`,
  `schema/src/session.ts:76-86`). There is no `/children` route in v2 (served path list).
- Foreground subagent: the parent tool blocks until the child completes, returns `{sessionID, status:"completed",
  output}` (`subagent.ts:208-245`). Background (`background:true`): returns immediately with status `running`; on
  completion a `session.synthetic` `<subagent sessionID=… state=…>…</subagent>` is posted to the parent and wakes it
  (`subagent-completion.ts:21-46`, `subagent-job.ts:30-67`). `POST /api/session/{id}/background` moves blocking
  foreground tools to background (`protocol/src/groups/session.ts:688-702`).
- Active check: `GET /api/session/active` → `{data: {[sessionID]: {type:"running"}}}`; absent = idle
  (`protocol/src/groups/session.ts:196-206`).

---

## 9. Models / providers

- `GET /api/model?location[directory]=…` → `{location:{directory}, data: Model.Info[]}`; returns
  `models.available()` = only `enabled` models of providers that have a connection (credential or env key) or are
  `activation:"enabled"`; ordered by release date (`server/src/handlers/model.ts:10-17`, `core/src/model.ts:222-226`,
  `core/src/provider.ts:445-466`). No inference is done. Signed out, only free `opencode` models are enabled
  (`core/src/model.ts:290-306`).
- `Model.Info` (`schema/src/model.ts:102-126`): `{id, modelID, providerID, canonical?, family?, name, compatibility?,
  package?, settings?, headers?, body?, capabilities:{tools, input[], output[], temperature?…}, variants:
  [{id, settings?, headers?, body?}], time:{released}, cost:[…], status, enabled, limit:{context, input?, output}}`.
- Selection format: `Model.Ref {id, providerID, variant?}`; string form `provider/model#variant`
  (`schema/src/model.ts:19-43`). The `id` field of `Model.Info` is what goes into `Ref.id` (`opencode models`
  prints `providerID/id`, `cli/src/commands/handlers/models.ts:20-23`).
- Effort ⇒ `variant` (must be one of `variants[].id`; unknown variant fails the run with `provider.no-route`,
  `to-session-error.ts:74-83`).
- `GET /api/model/default` → model used when the session has none (`protocol/src/groups/model.ts:25-38`).
- `GET /api/provider` → provider list (`v2-ops.txt`).

---

## 10. Credentials and storage

- Storage: SQLite table `credential(id, integration_id, label, value JSON, connector_id, method_id, active, time_*)`
  in the main DB (`core/src/credential/sql.ts:5-14`), i.e. `$XDG_DATA_HOME/opencode/opencode.db`. Values:
  `{type:"key", key, metadata?, configuration?}`, `{type:"oauth", methodID, refresh, access, expires, metadata?}`,
  `{type:"external", methodID, metadata?}` (`schema/src/credential.ts:33-58`). OAuth refreshes are written back to
  this table (`core/src/integration.ts:728-740`).
- v1 `auth.json` import: only the DB migration `20260805200742_import_legacy_credentials`, which reads
  `data/auth.json` and inserts rows (skipping integrations that already have one)
  (`core/src/database/migration/20260805200742_import_legacy_credentials.ts:34-103`). **A brand-new (empty) DB is
  bootstrapped from the schema and every migration is only marked complete, never executed**
  (`core/src/database/migration.ts:22-49`). So the import happens only when an existing v1 database (with a
  `session` table) is upgraded; a fresh managed home never imports `auth.json`, and later changes to `auth.json` are
  never read. This explains the measured `{"data":[]}`.
- `GET /api/credential` → `{data: [{id, integrationID, label, active, value}]}` **including secret values**
  (`protocol/src/groups/credential.ts:8-17`, `schema/src/credential.ts:64-70`). `POST /api/credential`
  `{id?, integrationID, label?, value, activate?}` stores one (409 on id conflict) (`credential.ts:19-31`,
  `schema/src/credential.ts:73-80`). `opencode auth export|import` uses these (`cli/src/commands/handlers/auth/import.ts:11-50`).
- Env-var keys: every models.dev provider with an `env` list gets an `env` connection method; any listed variable
  that is set becomes an active connection (`core/src/plugin/models-dev.ts:41-57`, `core/src/integration.ts:396-409`,
  `723-727`). The bundled snapshot lists `MINIMAX_API_KEY`, `DEEPSEEK_API_KEY`, `OPENCODE_API_KEY`
  (`core/src/models-dev/snapshot.txt`). Stored credentials take precedence over env connections
  (`integration.ts:397-409`, credentials first).
- Sessions/messages/events: same DB (`opencode.db`) (`core/src/session/sql.ts`).
- Hiding/removing a session: `DELETE /api/session/{id}` → 204, deletes it and children (`protocol/src/groups/session.ts:221-232`,
  `core/src/session.ts:376-386`). `time.archived` exists in `Session.Info` but there is **no archive endpoint**
  (not found in protocol). `session list` in the CLI lists roots of the current project, newest first
  (`cli/src/commands/commands.ts:420-437`).

---

## 11. Errors

- HTTP envelope: JSON body `{_tag, message, …}` with tag-specific fields and status from the error class, e.g.
  `{"_tag":"SessionNotFoundError","sessionID":"…","message":"…"}` 404, `InvalidRequestError` 400 (`kind?`,
  `field?`), `ConflictError` 409, `UnauthorizedError` 401, `ServiceUnavailableError` 503 (`service?`),
  `UnknownError` 500 (`ref?`), `LocationNotFoundError` 404, `PermissionNotFoundError` 404
  (`packages/protocol/src/errors.ts:5-266`; served shapes in `v2-ops.txt`).
- Run errors (events and messages): `SessionError {type, message, status?, response?:{body}}` on
  `session.execution.failed.error`, `session.step.failed.error`, `session.tool.failed.error`,
  `session.retry.scheduled.error`, `Assistant.error`, `Idle.error` (section 2.4 for the `type` taxonomy).
- Quota vs rate limit vs auth are separated by `type` (`provider.quota`, `provider.rate-limit`, `provider.auth`),
  classified in `ai/src/provider-error.ts:256-280`. ChatGPT token-sharing codes get friendly messages
  (`to-session-error.ts:10-26`, `101`).

---

## 12. Instructions (AGENTS.md)

- Global: `$XDG_CONFIG_HOME/opencode/AGENTS.md` (or `OPENCODE_CONFIG_DIR/AGENTS.md`)
  (`core/src/config/plugin/instruction.ts:41`, `70-74`).
- Project: `AGENTS.md` files found walking **up** from the session's `location.directory`; the walk stops at `$HOME`
  if the location is under `$HOME`, else at the project root (`instruction.ts:36-40`, `76-86`) — so for repos under
  `$HOME` ancestor AGENTS.md files between the repo and `$HOME` are included (inferred from `fs.up(start, stop)`).
  Project discovery is skipped if the location is not inside the project root or `OPENCODE_DISABLE_PROJECT_CONFIG`
  is set (`instruction.ts:39`, `server/src/routes.ts:133`).
- Configured `instructions: [...]` entries (paths, globs, URLs) are added (`instruction.ts:88-160`).
- Nested: after a successful `read` inside the location, AGENTS.md files between the read file and the location
  root are injected as durable synthetic instructions (`core/src/tool/plugin/read.ts:82-100`).
- Rendered as `Instructions from: <path>\n<content>` in the initial system baseline (`core/src/instruction-discovery.ts:123-125`).
- `CLAUDE.md` is **not** read as instructions (no reference found in core).
- Built-ins always added: date and an `<env>` block with working dir, project root, git yes/no, platform and the
  tmp dir (`core/src/instructions/builtins.ts:21-55`).

---

## 13. Headless reference: `opencode run --format json`

Files: `packages/cli/src/commands/handlers/run.ts`, `packages/cli/src/run/run.ts`,
`packages/cli/src/run/noninteractive.ts`, `packages/cli/src/session-target.ts`.

1. Connect: background service by default, private `serve --stdio --port 0` with `--standalone`, or `--server URL`
   (`handlers/run.ts:10-13`; section 1.2). `--auto` (also hidden `--yolo`, `--dangerously-skip-permissions`)
   = auto-approve (`handlers/run.ts:27`, `commands.ts:26-35`).
2. Resolve the target session (`run.ts:91-127`, `session-target.ts:32-89`): `--session` = `GET /api/session/{id}`
   (create with that id if missing); `--continue` = newest root in `location.directory` via
   `GET /api/session?directory=…&parentID=null&order=desc`; `--fork` = `POST /api/session/{id}/fork`; else
   `POST /api/session {id?, agent, model, location:{directory}}` (no permissions). When attached to the shared service
   it `PUT /api/session/{id}/environment` with the CLI's env minus password vars (`session-target.ts:79-85`,
   `cli/src/env.ts:15-22`). Optional `PATCH /api/session/{id} {title}` (`run.ts:130-135`).
3. Subscribe `GET /api/event` and wait for the first frame (`noninteractive.ts:78-80`). Generate prompt id
   `msg_…` (`:82`).
4. `POST /api/session/{id}/agent {agent}` if `--agent`; `POST /api/session/{id}/model {model:{providerID,id,variant}}`
   if a model/variant was requested (`:690-709`).
5. Start consuming, then `POST /api/session/{id}/prompt {id, text, files, delivery:"steer"}` (`:713-726`).
6. Consumption (`:209-547`):
   - `permission.asked` for owned sessions (root or descendants, resolved via `parentID` chain): reply `once` with
     `--auto`, else `reject` + feedback message (model continues) (`:158-186`, `221-224`).
   - `form.created` (question tool) for owned sessions: `DELETE /api/session/{sid}/form/{fid}?message=…` so the
     question tool fails with feedback (`:188-207`, `225-235`).
   - Ignore events whose `data.sessionID` is not the root session (`:236`) — only root output is printed.
   - `session.inbox.delivered{inboxID==prompt id}` ⇒ promoted (`:239-245`). Before promotion only
     `session.execution.failed` matters (stored as pre-promotion error) (`:253-264`).
   - Print: `session.step.started` → `step_start`; `session.text.ended` → `text` (full text, not deltas);
     `session.reasoning.ended` → `reasoning` (only with `--thinking`); `session.tool.input.*`/`tool.called`/
     `tool.progress` accumulate; `session.tool.success|failed` → `tool_use`; `session.step.ended` → `step_finish`
     with `{reason: finish, cost, tokens}`; `session.step.failed` → `error` (`:267-523`).
   - Terminal: `session.execution.succeeded` ⇒ done; `.failed` ⇒ `error` and exit 1; `.interrupted` ⇒ exit 130 for
     user interrupt, else error `"Session interrupted: <reason>"` unless caused by its own rejection
     (`:524-546`).
7. After the prompt returns, it also drains already-pending permissions/forms
   (`GET /api/session/{id}/permission`, `GET /api/session/{id}/form`) (`:740-757`).
8. Completion: `Promise.race([POST /api/experimental/session/{id}/wait, consumer-finished→wait])` (`:763-764`), then
   `reconcile()`: page `GET /api/session/{id}/message?limit=200&order=desc` back to the prompt's user message and
   print any text/reasoning/tool/error not already printed from events (`:550-677`, `766`). Missing user message ⇒
   error "Prompt was not promoted" (`:777-782`).
9. JSON lines: `{"type": "step_start"|"text"|"reasoning"|"tool_use"|"step_finish"|"error", "timestamp", "sessionID",
   …}` (`:100-104`); CLI-level failures print `{"type":"error",…,"error":{"type":"unknown","message"}}`
   (`run.ts:248-262`). SIGINT ⇒ `POST /api/session/{id}/interrupt` (`:679-686`).

---

## Mapping for the pactify adapter

| v1 call / behaviour used by pactify | v2 equivalent | Caveats |
|---|---|---|
| `opencode serve --hostname 127.0.0.1 --port 0`, wait for stdout `opencode server listening on http://…` (`pactify:opencode_server.go:63`, `107-117`) | same argv prints `server listening on http://127.0.0.1:PORT`; or `serve --stdio --port 0` prints `{"url":"…"}` | Prefix changed (v1 matcher never fires → 30 s timeout). `--stdio` is what opencode itself uses for private servers, removes the password from the tool env, and exits on stdin EOF (needs a stdin pipe kept open). |
| `OPENCODE_SERVER_USERNAME=opencode`, `OPENCODE_SERVER_PASSWORD=<pw>` | `OPENCODE_PASSWORD` (or legacy `OPENCODE_SERVER_PASSWORD`); username is always `opencode` | Default mode leaves the password in `process.env` → inherited by shell tools; keep `secretWriter`/redaction or use `--stdio` / `PUT /api/session/{id}/environment`. |
| `?directory=<repo>` on every request | session routes: none (location from session). Location routes: `location[directory]=<abs>` query or `x-opencode-directory` header | `?directory=` is ignored; without location, server cwd is used (pactify sets `cmd.Dir = RepoDir`, so cwd works). |
| `GET /config/providers` → `providers[].id` + `models` keys | `GET /api/model` → `data[]` with `providerID`, `id`, `enabled`, `variants[].id` | Only available+enabled models are returned; ID string `providerID + "/" + id`. |
| `POST /session {title, permission:[{permission,pattern,action}]}` | `POST /api/session {title, agent, model:{id,providerID,variant?}, location:{directory}, permissions:[{action,resource,effect}]}` → `{data: Session.Info}` | Response is wrapped in `data`. Field names changed. Set agent/model here (prompt can't). |
| Read-only rules: `*,read,glob,grep,list,bash(+exact commands),unsafe patterns` (`pactify:opencode_permissions.go`) | `[{*,*,deny}, {read,*,allow}, {glob,*,allow}, {grep,*,allow}, {shell,*,deny}, {shell,"git status",allow}, …, {shell,"*>*",deny}…]` | Rename `bash→shell`; `list` has no v2 tool (drop); last-match semantics unchanged (findLast); `\|`, `;`, `&` patterns no longer meaningful because commands are split per simple command (each part must be allowed); `*` deny also removes `question`, `subagent`, `webfetch`, `external_directory`. |
| Default-access session rules `edit ask, bash ask` | `[{edit,*,ask}, {shell,*,ask}]` (+ consider `{question,*,deny}`) | v2 `build` agent default is `*: allow`; build also allows `question` → forms would block a headless run unless denied or cancelled. |
| `PATCH /session/{id} {permission}` on resume ("appends") | `PATCH /api/session/{id} {permissions}` → 204 | **Replaces** the ruleset (`projector.ts:593-598`); send the complete list. Existing children keep their old copy. |
| `OPENCODE_CONFIG_CONTENT` with v1 `permission{…}` + `experimental.continue_loop_on_deny` (`pactify:opencode.go:13-43`) | Still accepted (migrated to `permissions`, appended to every agent); prefer native `"permissions":[…]` | `continue_loop_on_deny` is dropped and unnecessary (deny ⇒ tool failure, model continues). Config rules lose to session rules. Ordering of JSON object keys must be preserved for v1 form (same as today). |
| `OPENCODE_DISABLE_AUTOUPDATE/…CLAUDE_CODE/…EXTERNAL_SKILLS/OPENCODE_PURE` (MinimalConfig) | `OPENCODE_DISABLE_CLAUDE_CODE` honoured; consider `OPENCODE_DISABLE_PROJECT_CONFIG`, `OPENCODE_DISABLE_MODELS_FETCH`, `OPENCODE_CONFIG_DIR` | `OPENCODE_PURE` and `OPENCODE_DISABLE_EXTERNAL_SKILLS`: not found; `~/.agents` and system/MDM config are still read (`$HOME`, not XDG). Autoupdate never runs under `serve`. |
| `GET /session/{id}` → `tokens`, `cost` | `GET /api/session/{id}` → `data.tokens{input,output,reasoning,cache{read,write}}`, `data.cost` | Wrapped in `data`; own session only; includes title-generation usage unless `title` set at create. |
| `GET /session/{id}/children` (recursive) | `GET /api/session?parentID=<id>` (paginate with `cursor.next`, default 50) or track `session.created.data.parentID` on the stream | No `/children` route. |
| `GET /session/{id}/message` → `[{info, parts}]`; seen-set diff | `GET /api/session/{id}/message?order=desc&limit=200` → `{data, cursor}`; assistant `content[]` | Paginate; locate the turn by the prompt's own `id` (user message id) and the next `idle` marker instead of a before/after diff. |
| `POST /session/{id}/prompt_async {agent, parts, model, system, variant}` | `POST /session/{id}/agent`, `POST /session/{id}/model {model:{id,providerID,variant}}`, optional `PUT /api/experimental/session/{id}/instructions/entries/<key> {value}`, then `POST /api/session/{id}/prompt {id:"msg_…", text, delivery:"steer"}` | No per-prompt `system`; instruction entries are experimental and rendered as `<context key=…>`; a custom config agent with `system` replaces the default prompt instead. Generate the `msg_` id client-side for correlation/idempotency. |
| `GET /event` (data-only SSE, first event read as ready) | `GET /api/event`; first frame `server.connected` | Global feed for all locations/sessions; filter by `data.sessionID` (root + children). Heartbeat comments `: heartbeat`. |
| `message.part.delta {partID, field:"text", delta}` | `session.text.delta {assistantMessageID, ordinal, delta}` | Key text by `(assistantMessageID, ordinal)`; reasoning has separate `session.reasoning.*`. |
| `message.part.updated` text parts (full text) | `session.text.ended {text}` | Durable; use for final text if the history read fails. |
| `message.part.updated` tool part, status `running/completed/error`, `state.output` | `session.tool.input.started {id,name}` → `session.tool.called {input}` → `session.tool.success {content[]}` / `session.tool.failed {error, content?}` | Tool id is unique only per assistant message: key by `(assistantMessageID, id)`. Output is `content[]` (text/file items), not a string. |
| `state.metadata.truncated` warning | `session.tool.success.data.metadata.truncated` (+ `outputPath`) | Set by the generic bounding layer (`tool-output.ts:68-116`) and by glob/grep/read. |
| `message.updated` assistant `providerID/modelID` → `SessionInfo.Model` | `session.step.started.data.model {providerID, id, variant}` (and `Assistant.model`) | |
| `session.idle` / `session.status{type:"idle"}` = finished | `session.execution.succeeded|failed|interrupted` for the root session | v1 events are never emitted in v2. Background subagent/shell completions re-wake the parent later. Decline without message ⇒ `interrupted` with `reason:"shutdown"` (inferred). |
| `session.error {error:{name,data}}`, `Assistant.error` | `session.execution.failed.error`, `session.step.failed.error` (`SessionError{type,message,status?,response?}`) | Rewrite `opencodeFailure`: `provider.auth` → auth, `provider.quota` → quota, `provider.rate-limit` → transient (already retried 4×), `provider.internal/transport/timeout/unknown` → transient, `provider.no-route` → model. No `isRetryable`. |
| `GET /session/status` after SSE reconnect | `GET /api/session/active` (absent ⇒ idle) or `POST /api/experimental/session/{id}/wait` | No replay on `/api/event`; for missed durable events use `GET /api/experimental/session/{id}/log?after=<seq>&follow=true`, else re-read messages. |
| `GET /permission` after reconnect | `GET /api/session/{sid}/permission` per owned session (or `GET /api/permission/request`) | In-memory; lost if the server restarts. |
| `permission.asked {id, permission, …}` | `permission.asked` data `{id, sessionID, action, resources[], save?, metadata?, source?}` | Field `permission` → `action`; patterns → `resources`. |
| `POST /permission/{id}/reply {reply:"once"|"reject"}` | `POST /api/session/{sid}/permission/{rid}/reply {decision:"once"|"always"|"reject", message?}` | Needs the request's `sessionID` in the path. For non-interactive rejects send a `message` so the model continues; `always` persists project rules in the DB — avoid. |
| `POST /session/{id}/abort` + kill descendant processes | `POST /api/session/{id}/interrupt` → `{interrupted}`; event `session.execution.interrupted{reason:"user"}` | Foreground shells/subagents are cancelled; background jobs are not (inferred) — keep the process-tree snapshot/kill. |
| Managed home: `XDG_DATA_HOME`/`XDG_CONFIG_HOME` mirror, link `opencode/auth.json` (`pactify:internal/sub/opencode_home.go:34-39`) | Credentials live in `opencode.db` `credential` table | Fresh DB never imports `auth.json` → linked file is useless. Use env keys (`MINIMAX_API_KEY`, `DEEPSEEK_API_KEY`, …) or `POST /api/credential`. Also isolate `XDG_STATE_HOME` and `XDG_CACHE_HOME` (locks, bin cache) if full isolation matters. |
| Doctor checks `opencode/opencode.db` is a regular file (`pactify:opencode_home.go:88-91`) | Same file name for channels latest/dev/beta/next/prod; `opencode-<channel>.db` otherwise; `OPENCODE_DB` overrides | |
| Snapshot of user files: `opencode.db{,-wal,-shm}`, `auth.json` (`opencode_home.go:96-124`) | Same files; `auth.json` no longer used by v2 | |

### Blockers / things the adapter cannot do the v1 way on v2

1. **Credentials via linked `auth.json` do not work** on a fresh managed DB (migration only marked done,
   `core/src/database/migration.ts:22-49`). v2 credentials (incl. OAuth refresh) live in the user's
   `opencode.db`, the same file as the user's sessions. Options: provider env keys; copy credentials with
   `GET /api/credential` from a server on the user's real data dir and `POST /api/credential` into the managed one
   (contradicts the "never open the user DB" rule in `pactify:opencode_home.go:94`; OAuth refreshes then diverge); or
   point `OPENCODE_DB` at the user's DB and `DELETE` sessions afterwards (pollutes the user's history until deleted).
   Needs an Owner decision.
2. **No per-prompt system prompt.** Closest: experimental instruction entries (rendered as
   `<context key="…">…</context>` inside the system baseline) or a config-defined agent whose `system` replaces the
   default prompt.
3. **No event replay on `/api/event`** (volatile). Reconnect must rebuild state from `GET /api/session/active`,
   messages, pending permissions, or the experimental durable log.
4. **Child usage is not aggregated**; the adapter must enumerate children (`?parentID=`) and sum.
5. **`PATCH permissions` replaces** instead of appending; the resume path must send the full ruleset.
6. **Shell permission semantics changed** (per-command resources, `shell` action, no `list`); the read-only
   allowlist/denylist must be rewritten and re-verified on the real binary (the portable scanner path is unverified).
7. **Completion is busy-period based**: background subagents/shells can re-wake the parent after
   `execution.succeeded`; a declined permission without a message surfaces as `interrupted/shutdown` (inferred).
8. Headless hazards with the default `build` agent: everything is `allow` by default, and the `question` tool opens
   forms that block until answered or cancelled (`DELETE /api/session/{sid}/form/{fid}?message=…`) — deny
   `question` in session rules or handle `form.created`.

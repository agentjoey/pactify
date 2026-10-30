# Jobs view

The dashboard (`pactify serve`) and GET-only panel (`pactify sub panel`) share **Dashboard / Jobs / Settings** navigation. Jobs is the default page. The header shows attention counts, a search trigger (⌘K or `/`), live polling age, Settings and a theme control. On phones, navigation stays in a segmented row and content has a 16px gutter. Live remains on Dashboard and in job details.

Both builds default to **dark**, independently of the OS. Choose **light** or **dark** from the header or **Settings → Appearance**. The choice is saved in this browser's localStorage when available. Space Grotesk and JetBrains Mono are served locally; no Google Fonts requests or CSP changes are needed. Motion respects `prefers-reduced-motion`.

## Pipeline and list

**Lines** is the default Jobs view. Its four stations use the existing latest-run section rule:

| Station | Jobs |
| --- | --- |
| 01 Queued | `queued`, even with attention |
| 02 Running | `running` or `retrying`, even with attention |
| 03 Needs you | Other statuses with attention |
| 04 Shipped | Other statuses without attention |

Group production lines by **Project** (repository basename), **Harness**, or **One line**. Each line shows observed harnesses or project counts, its running share and jobs finished today. Belts move only on lines with running jobs. Statistics use actual jobs, timestamps and available actions; there is no inferred capacity or execution stage. Shipped shows the three most recent jobs per line, with **+N more** expanding that line in place. Completion-day totals use the browser's local calendar.

Lines with queued, running/retrying or Needs-you jobs stay active regardless of age. Lines are also active when any creation, start or finish timestamp is less than three days old. Current work sorts before recent completions, with the latest activity first within each group; quiet lines follow in latest-activity order.

The **Active** / **All lines** toolbar choice defaults to Active and is saved in localStorage when available. Active collapses older idle lines under **Quiet lines · N**. Each chip shows the line name and relative age; click it to expand that line in place, or use **Show quiet lines** / **Hide quiet lines** for all of them. All lines shows every line without the quiet section. Any search or filter reveals matching lines regardless of age.

On desktop, the line label rail stays fixed while stations scroll horizontally. Scroll padding reserves its 220px width so Queued remains visible. Below 640px, stations become a full-width horizontal pager, initially on **Running**. Tap a station tile or swipe; highlighted tiles and page dots track the visible station. The board handles empty/filter-empty states, while onboarding, loading, server errors and expired sessions retain explicit guidance.

Switch to **List** for the existing sectioned cards, completion-day stacks and optional **Compact list** with eight aligned metadata columns. The Lines/List choice is saved in localStorage when available. Project, Harness, Status and search filters remain in the URL and work in both views. In Lines, the view switch, grouping, search and shortcuts share one toolbar; **Filters** reveals the other controls. Phone filters also use this disclosure in List. Search stays visible and `/` focuses it in both views. Search matches title, ID, repository and requested or actual model. Explicit resume/fallback links form chains; matching any run retains the whole chain. Chains use their root title and latest run's status, model, duration and usage. Manual resume links count as rework; automatic retries do not.

Use `/` or **⌘K** to search, **j/k** for next/previous job, **h/l** for previous/next station in Lines, **Enter** for details and **Escape** to close. **?** opens shortcut help. Typing inside inputs does not trigger job navigation.

Open `#/jobs` or `#/jobs/<id>` directly. The drawer preserves filters and shows brief, result, Live, bounded readable logs, file diffs and run timelines. Dashboard actions confirm cancel, resume and dismiss. Worktree apply and discard stay with the delegating session or the CLI, so the dashboard shows a finished job with a pending worktree as Done with a worktree pending marker rather than in Needs you. **Dismiss** is offered for terminal jobs whose only attention is an unacknowledged failure or actionable warnings, with no pending worktree. It keeps the result and job history, acknowledges the job and moves the card out of Needs you. It does not apply, discard or otherwise change a worktree. Needs-you cards in the panel instead offer copyable terminal commands. The panel sends GET requests only and keeps its strict CSP and session handling.

## Dashboard and Settings

Dashboard starts with five KPIs: in progress, queued, attention, succeeded in seven local calendar days and failed in seven local calendar days. Phones use a 2+3 tile layout. The main column contains running and attention jobs, the Live tree, a 14-day token chart and harness usage. The side column shows installed harness snapshots, quota and recent job activity. Cost, subscription, unknown and partial usage remain distinct; unavailable data is not fabricated.

Settings uses a left navigation and content area, with a section select on phones. **Harnesses / Models & routing** retains installed versions, models, default model/effort, aliases, fallback ordering, quota groups and exhaustion records. Dashboard edits still handle 422 field errors, 409 conflicts and explicit reloads; panel machine settings remain read-only. **Storage** shows per-location sizes and reclaimable estimates, cache usage against its actual cap, policy chips, protected-worktree notices and the reviewed dashboard cleanup flow; the panel provides CLI commands. **Doctor** shows only the existing stored snapshot, never running checks. **Appearance** stores a manual theme and **Shortcuts** documents navigation.

Live displays observed harnesses, models, effort, calls, diff, quota, resume/fallback and retry links. Redacted events are batched per animation frame, with at most four live streams; hidden or unmounted views close their streams. No unobserved agents, stages or quota reset times are inferred.

## Access through an authenticated reverse proxy

Access path:

```text
browser → authenticated reverse proxy → http://127.0.0.1:7777 → pactify serve
```

For example, open `https://dash.example.com/#/jobs` and complete proxy authentication first. `PACTIFY_ALLOWED_ORIGINS` is for a reverse proxy that performs user authentication and should be configured with full origins, such as `https://dash.example.com`; comma-separated values and trailing-slash removal are supported.

By default `pactify serve --addr 127.0.0.1:7777` requires no token. All requests (including static assets and SSE) validate Host: a loopback listener accepts only `127.0.0.1`, `localhost`, `[::1]` plus the listening port, and allowlist-origin Hosts (with or without their explicit port). Browser write requests must exactly match the `http` loopback origin on that port or be allowlisted; other local ports and `null` return 403. CLI / script requests without Origin remain allowed. These checks cannot replace user authentication.

Non-loopback listeners (including `0.0.0.0`, `::` and an omitted host) require `PACTIFY_SERVE_TOKEN` with at least 32 characters, and Host accepts only the bound IP / hostname plus port or an allowlisted proxy Host. Whenever a token is set on any address, every `/api/` request (including reads and SSE) must carry `Authorization: Bearer <token>`, otherwise it returns 401. Static assets remain public but are subject to the Host check. The Dashboard UI itself does not send this token; token mode is for reverse proxies that inject the header after authenticating users, and the proxy must also cover SSE requests.

The API uses the serve process's `PACTIFY_HOME` and, like the CLI, reads `$PACTIFY_HOME/jobs`, or `~/.pactify/jobs` when unset. A background service has a different environment from an interactive shell, which affects job directories and the configuration needed for resume; variables exported in the shell do not automatically reach the service. Workers do not inherit `PACTIFY_ALLOWED_ORIGINS` or `PACTIFY_SERVE_TOKEN`. The service reclaims finished workers in the background.

## Local sub panel

```sh
pactify sub panel
pactify sub panel --port 17100 --no-open
```

By default it selects a random loopback port and opens the browser; `--no-open` only prints the URL, it can listen only on `127.0.0.1`, and Ctrl-C exits. The first visit to `http://127.0.0.1:<port>/?token=…` consumes a one-time launch token in exchange for an independent session token; reusing the original URL returns 401.

The frontend removes the token from the URL and keeps the session in the current tab's `sessionStorage`. The API and fetch log streams use `Authorization: Bearer <session>`, not an authentication cookie; after restarting the panel a new URL is required. Panel authentication is independent of serve's host security model.

A new tab opened at the cleaned panel URL can request the existing session
through a BroadcastChannel with a request nonce. Tabs of the same panel origin
share the session through this channel; BroadcastChannel is same-origin only,
and the panel CSP forbids third-party scripts. A tab that already has a session
restores it immediately without waiting for a handoff. It keeps the received
session in its own sessionStorage and uses bearer headers; the session is never
added to a URL or copied command. Without an answering tab, the panel shows
“Session ended” after two seconds. The original launch URL remains single-use.

Polling pauses while the document is hidden, doubles its delay after errors from
2 seconds up to 30 seconds, resets on success, and stops on 401. The header shows
relative update age, documentation and the CLI version; the tab title counts
chains needing attention. Search includes title, job ID, repository and both
requested and actual models. In List view, compact completed history uses eight aligned desktop
columns: status, title, harness/model, project, duration, tokens, cost and time.
Titles truncate with their full text in a tooltip. Narrow rows put title/status
above one metadata line. Job details show one command list: in the pinned footer
on desktop, or revealed from the compact footer button on narrow screens. Cards and
Usage show reported costUSD, subscription for codex/kimi/agy without a reported
cost, and unknown for other unpriced harnesses.

## API and boundaries

Both hosts mount the same `subpanel.ReadOnlyHandler`:

| Feature | serve | Local panel |
| --- | --- | --- |
| List / details | `/api/sub/jobs?all=1`, `/api/sub/jobs/<id>` | `/api/jobs?all=1`, `/api/jobs/<id>` |
| SSE logs / events | `/api/sub/jobs/<id>/{logs,events}` | `/api/jobs/<id>/{logs,events}` |
| diff | `/api/sub/jobs/<id>/diff` | `/api/jobs/<id>/diff` |
| Doctor snapshot | `/api/sub/doctor` | `/api/doctor` |
| CLI version | `/api/sub/info` | `/api/info` |
| Harness status / models | `/api/sub/harnesses` (`?refresh=1` to refresh) | `/api/harnesses` |
| Router configuration | `GET /api/sub/router` | `GET /api/router` |
| Usage summary | `/api/sub/cost?since=7d&by=harness` | `/api/cost?since=7d&by=harness` |
| Save router / defaults | `PUT /api/sub/router` | Not registered |
| Clear exhaustion records | `DELETE /api/sub/quota/<encoded-key>` | Not registered |
| Job actions | `POST /api/sub/jobs/<id>/{cancel,resume,apply,discard,dismiss}` | Not registered |

The shared read-only API accepts only GET and rejects HEAD as well. All serve requests undergo the Host check, and token mode also requires the API Bearer header; write operations undergo the exact Origin check above, accept only JSON, cap the request body at 64 KiB, and return 409 on state conflicts. Dashboard resume allows at most four active jobs per store (including queued/retrying) and returns 429 when exceeded; the CLI does not consume this quota. Finishing, canceling or a failed start releases the slot. The CLI and dashboard share the session lock.

Job reads use an atomic state snapshot and a single index, without recovering orphan workers, creating lock files or modifying job state. Directories missing `state.json` are skipped, and invalid job ids are rejected. The cost API reuses [`sub cost`](sub-cost.md) for its arguments and errors, additionally returning `exhaustedRoutes` after filtering expired records, and does not infer exhaustion or reset times from historical `quotaSnapshots`.

Logs start from at most 100 readable records within the last 1 MiB window, with a half-line buffer of at most 64 KiB, and the browser retains at most 1,000 lines; a terminal job closes the stream, leaving details cancels the request, and a broken stream / error displays a status. Use `pactify sub logs <id> --follow` for the complete log.

The worktree diff is relative to the recorded base commit, includes untracked files, does not stage or modify the Git index, is limited to 512 KiB / 10 seconds, and shows a truncation notice. When there is no worktree, it has been discarded or it cannot be read, the existing change list / notice is shown; the result does not store the full patch. Result file reads are limited to 4 MiB.

The diff response includes `maxBytes` (524288), `truncated`, `patch`, `files` and
`note`. The viewer colours text nodes, shows per-file line counts, and initially
collapses files larger than 400 lines. File headers show a single path for
unchanged names and old → new for renames, with added/removed line counts. A truncated patch shows its size limit and
a copyable `git -C <worktree> diff <base> --` command for the full tracked diff.

`GET /doctor` returns `{lastRun: null, harnesses: [...]}` because doctor persists
verified versions, not complete reports. Each harness has `name`, `installed`,
`version` and `verifiedVersion`. Installed versions come only from stored catalog
entries matching current executable metadata; an absent or changed snapshot
shows “version unknown”. The endpoint never runs a harness, discovers models,
executes doctor, recovers jobs, or writes storage. Settings offers a copyable
`pactify sub doctor` command. `GET /info` returns `{version: "..."}` using the
running CLI's release version. Both routes reject non-GET methods, including HEAD,
and inherit their host's authentication and Host checks.

## Build and verification

```sh
cd web
npx tsc --noEmit
npx vitest run
npm run build
npm run build:panel
npx playwright test --retries 0
node scripts/shots.mjs
node scripts/shots-live.mjs
node scripts/shots-panel.mjs
```

`build` and `build:panel` produce the Git-tracked `internal/serve/dist` and `internal/subpanel/dist` respectively; `npm run e2e` builds both in order and then runs Playwright. Tests and screenshot scripts use an independent mock service and the final embedded dist, without depending on a resident daemon.

After rebuilding dist, rebuild the Go binary and restart the local server. For macOS background services, see [operations](operations.md). Check screenshots and the actual authenticated proxy path when applicable.

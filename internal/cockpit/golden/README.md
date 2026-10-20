# Sanitized protocol recordings

`Recorder` captures HTTP response bodies (including SSE) and newline-framed ACP /
stream-json stdin/stdout. Recording is opt-in through `StartOpts.Transcript`.
Normal jobs leave it nil. Headers are omitted; structured credentials, provider
`key` fields, common token formats, passwords, cookies, device IDs, email addresses,
and absolute user-home paths are replaced with `[REDACTED]` before persistence.
`Scan` independently walks structured and embedded JSON/NDJSON/SSE values. It never
prints a sensitive value in its error. There is no credential-file reader here.

The real transcripts live in `../testdata/golden/`: five opencode and four kimi.
`../golden_replay_test.go` replaces only the external process/HTTP boundary and
runs the production adapter, ACP client, usage/wire parser and failure classifier.
It compares normalized turn-event order, final text, usage, failure category and
session ID. Raw housekeeping frames remain recorded but are excluded from event
ordering because their callbacks can precede session registration or follow turn
completion. Cancellation waits for the recorded cancel/abort request. Permission
responses are causally acknowledged before the replay emits later SSE frames.

`TestGoldenDetectsRenamedTokens` changes `tokens` (opencode) and `usage` (kimi wire)
to `tokenz`, retaining valid JSON and the original expectation. Both must produce
an outcome mismatch. `TestGoldenReadOnlyContainsDeniedWrite` requires an actual
rejected write tool call, rather than a verbal refusal. `TestGoldenFilesSanitized`
scans every recorded transcript. `testdata/agy-contract.json` is explicitly a
synthetic stream-event contract, not a live recording; replay does not launch agy.

## Record explicitly

Run only from a disposable worktree, with a new absolute recording root. Keep the
root across write and resume so the managed session database survives. Do not use
the user's HOME as the recording root. The recorder validates that HOME and all
vendor data directories are real directories below that root. It accepts only
opencode or kimi; ordinary builds do not include this test.

Prepare a private `HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, and
`KIMI_CODE_HOME` below the root. Use the managed-link mechanism for authentication:
link opencode's `auth.json`; link kimi's `config.toml` and `device_id`, and link
its **entire `credentials/` and `oauth/` directories**. Create the source `oauth/`
directory as 0700 if it does not yet exist; kimi normally creates it lazily. The latter shares refresh
locks with interactive kimi sessions. Never link individual kimi credential files:
atomic rename would replace the link and strand a rotated refresh token inside
the disposable home, invalidating the user's retained login when it is deleted.
Do not read, print or copy credential contents.

Production managed homes record source inode/size/mtime and canonical path at
prepare for explicitly marked credential file links. Configuration links, such
as kimi config.toml and device_id, never write back private rewrites. If a CLI
atomically replaces a credential file link, cleanup renames the
private file back to an unchanged source, first restoring the source's original
permissions on the private file. A chmod failure quarantines it instead. If the source also changed (or its
baseline is unavailable), cleanup instead renames the private file into an owned
0700 persistent quarantine at `$PACTIFY_HOME/credential-quarantine/refresh-*/credential`
(default `~/.pactify/credential-quarantine`)
and emits a warning. Neither case reads credential contents or deletes a refreshed token.
Do not use shared /tmp storage for quarantine: cleanup or reboot can erase it.
Rename failure retains the private file and reports its location; do not remove
the recording root until preservation succeeds. The recorder applies the same source-metadata write-back/conflict-quarantine
policy to opencode's file link and reports any preservation failure. E2E fixtures
use a stricter policy: every replaced file link fails loudly and is quarantined
outside the fixture root for operator review. Never remove a private
replacement as credential cleanup, and never put refreshed tokens in the repo.

Take metadata-only snapshots of `~/.config/opencode`, `~/.local/share/opencode`,
and `~/.kimi-code` before and after. Compare inode, mode, size, modification time,
and symlink target without following links. List any credential refresh separately.

With those directory environment variables explicitly set:

```sh
PACTIFY_HOME="$record_root" PACTIFY_RECORD_HOME="$record_root" \
  PACTIFY_RECORD_HARNESS=opencode PACTIFY_RECORD_SCENARIO=write \
  go test -tags record ./internal/cockpit -run '^TestRecordGolden$' -count=1 -v
```

Repeat with `readonly`, `resume`, `cancel`, `failure`; kimi uses `write`, `resume`,
`cancel`, `failure` only, because it cannot enforce read-only access. Limit live kimi
recording invocations to ten per task. No record command is run in CI.

Opencode queries the live provider list first. It chooses `opencode/*-free`;
`PACTIFY_RECORD_MODEL` may explicitly choose a live-listed free model or
`minimax-coding-plan/*`. The failure case starts a valid session and changes only
the submitted model to a nonexistent ID, so the real SSE `session.error` path is
recorded without billing. The read-only diagnostic overrides only the plan agent's
prompt to elicit one actual write attempt; its native plan identity and all deny
rules remain in force. This setup is noted in the transcript.

Kimi uses the linked subscription configuration. Its failure case generates its
own configuration pointing to a loopback listener that immediately disconnects;
it never reads or edits the user's provider configuration, and uses a dummy key.
New recordings cap the offline case at one attempt per step. Wire fragments retain
LLM model identity, turn usage, and terminal/error records; full LLM prompt and tool
payloads are omitted from wire because ACP already records that traffic. Resume
fragments start at each agent's own wire offset.

Run all replay and redaction checks under a fresh isolated home:

```sh
PACTIFY_HOME="$(mktemp -d)" go test ./internal/cockpit/... -run 'TestGolden|TestRedact|TestScanner|TestRecorder|TestManagedDirectory' -count=1
```

If preservation fails, stop recording and interactive sessions before recovery.
Keep the reported private file and recording root. Fix quarantine ownership,
0700 directory permissions or same-volume rename availability, then retry. The
operator can alternatively rename the retained file into a private persistent
quarantine on the same volume, or restore it to the correct source by rename
with the source's original mode after resolving concurrent refreshes. Never
blindly replace a source that also changed. No credential contents need be read,
printed or copied. The remaining metadata-check/rename race with interactive agy
refresh remains possible; exchange syscalls alone do not provide a
simple portable compare-and-replace contract.

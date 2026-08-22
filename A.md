<!-- pact:begin (managed by pactify — edit outside this block) -->
# pact protocol

> Bind this working copy's seat once, then read the board.

```bash
pactify seat use <your-seat-id>   # from the roster in .pact/PROJECT.md
pactify join --roles <your-roles>
```

Your seat resolves from `PACT_AGENT_ID` (env) else the untracked `.pact/seat` file.
For concurrent seats in one repo, use a separate git worktree per seat.
Then read `.pact/PROJECT.md` and `.pact/STATE.yml`. Run `pactify help` for the verbs.
<!-- pact:end -->

<!-- Generated from plugins/src; do not edit. -->

# Pactify portable plugin

Generated at `plugins/agent-plugin/`, this directory is a marketplace repository
root. Plain skill copies live at `plugins/agents-skills/`. Its `pactify/` child is an
Agent Plugins 1.0 plugin (version 0.6.0, license FSL-1.1-MIT).
It requires pactify >= 0.14.0, git, macOS or Linux, and an installed and
logged-in harness CLI. It has no lifecycle hooks or forwarding agents.
Each skill includes a local prerequisite checker. Never run installers on the user's behalf.

## Other hosts

The [portable Agent Plugins 1.0 bundle](https://github.com/agentjoey/pactify/tree/main/plugins/agent-plugin)
provides the same delegation and review workflow with bounded polling.
The install locations below are **pending the final repository location** and may change;
these examples target `agentjoey/pactify` and `plugins/agent-plugin`.

For Codex:

```sh
codex plugin marketplace add agentjoey/pactify --sparse .agents/plugins --sparse plugins/agent-plugin
```

Then select pactify in the host's plugin interface. For Copilot:

```sh
copilot plugin install agentjoey/pactify:plugins/agent-plugin/pactify
```

For Cursor, open Settings → Plugins and paste
`https://github.com/agentjoey/pactify/tree/main/plugins/agent-plugin` into plugin search.
The bundle includes marketplace files for all three hosts.
See the [Codex packaging guide](https://developers.openai.com/plugins/build/plugins),
[Copilot plugin reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-plugin-reference)
and [Cursor plugins guide](https://cursor.com/docs/plugins) for host installation details.

Alternatively, copy the folders from
[plugins/agents-skills](https://github.com/agentjoey/pactify/tree/main/plugins/agents-skills)
into `.agents/skills/`. The release's `pactify-delegate.zip` contains one Agensi skill.
Checks only detect pactify and show install commands; agents must never run installers.

The host must allow `pactify sub` outside its sandbox because pactify runs harness CLIs
outside the workspace. For example, in Codex's `~/.codex/rules/default.rules`:

```python
prefix_rule(pattern=["pactify", "sub"], decision="allow")
```

Codex matches rules against simple commands only, so the portable skills pass the brief
with `--brief-file` instead of a heredoc.


Use delegate for default codex delegation or an explicit @alias, delegate-codex,
delegate-kimi, delegate-opencode, or explicitly invoke experimental delegate-agy.
Use jobs for read-only status. Delegate only on explicit user request.
Start with `--wait 0s --json`, parse the job id on exit 3, and repeat
`pactify sub wait <jobId> --timeout 540s` while pending. Review the complete diff
and new files, rerun all acceptance commands, then apply/discard or resume for rework.
See each skill for the complete shared rules.

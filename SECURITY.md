# Security policy

## Reporting a vulnerability

Report privately through GitHub, at [Security, Report a vulnerability](https://github.com/svyatov/equip/security/advisories/new). The report is visible only to the maintainer until an advisory is published.

Do not open a public issue for a vulnerability.

You will get a first response within 14 days. That response says whether the report is accepted and, if it is, the timeline for the fix and the disclosure. If 14 days pass with no reply, open a public issue saying only that a private report is waiting for a response. Leave out every detail of the vulnerability.

## What is in scope

equip runs on the user's machine. It reads and writes the configuration files of Claude Code and Codex. It keeps its own Record of each Project under `$XDG_STATE_HOME/equip`. To measure an MCP server, it starts the server or connects to it the way the agent does. Reports of the following are in scope:

- A write that changes a key equip does not own in an agent's configuration file, or that corrupts the file.
- A write to `.claude/settings.local.json` in a Project where git tracks that file.
- A write to any file other than these: the agent configuration files, the Record, the presets under `$XDG_CONFIG_HOME/equip/presets`, the measurements under `$XDG_CACHE_HOME/equip`, and the Project's `.git/info/exclude`.
- An MCP server that equip starts or connects to although no agent has it on and trusted.
- A crafted configuration file, plugin manifest, preset, or Record that makes equip run a command or read a file it otherwise would not.

Out of scope: an agent that ignores the state equip wrote, and an extension that misbehaves once an agent loads it. Both are ordinary issues.

## Supported versions

equip is pre-1.0. Only the latest release gets fixes; there are no backports to earlier tags.

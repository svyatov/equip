# What Claude Code loads per project, and which settings set its state

Research for ticket #2. Question: for a given **Project**, which **Skills**, **Plugins** and **MCP servers** does a Claude Code session load, from where, with what precedence, and which per-project, per-user (uncommitted) setting puts each **Extension** in the **State** on, **Manual-only**, or off?

Sources: the official docs at `code.claude.com/docs/en/*.md` (fetched 2026-09-25 via `llms.txt`), the Claude Code 2.1.282 binary on this machine (`strings`), and this machine's config files. Secrets were not read or printed; only key names and state values were inspected.

## Answer in one table

| Extension kind | Discovered from | On | Manual-only | Off | Key lives in (per project, per user, uncommitted) |
| --- | --- | --- | --- | --- | --- |
| Skill (personal, project, nested, `--add-dir`, bundled, claude.ai-synced) | See [Skills](#skills) | absent, or `"on"` | `"user-invocable-only"` | `"off"` | `skillOverrides` in `<repo-root>/.claude/settings.local.json` |
| Skill inside a plugin | Plugin's `skills/` dir | plugin on | none | none (only whole plugin) | none; `skillOverrides` ignores plugin skills |
| Plugin | Marketplace installs, `.claude/skills/<dir>` with a manifest (`@skills-dir`), claude.ai sync (`@synced`), `--plugin-dir` (`@inline`) | `true` | none | `false` | `enabledPlugins` in `<repo-root>/.claude/settings.local.json`, key `name@origin` |
| MCP server: user, local, plugin, managed-provided, claude.ai connector, default-on built-in | See [MCP servers](#mcp-servers) | absent from list | none | listed | `projects["<repo-root>"].disabledMcpServers` in `~/.claude.json` |
| MCP server: default-off built-in (e.g. `computer-use`) | built in | listed | none | absent | `projects["<repo-root>"].enabledMcpServers` in `~/.claude.json` |
| MCP server from project `.mcp.json` | `.mcp.json` at project root | name in `enabledMcpjsonServers` (or `enableAllProjectMcpServers: true`) | none | name in `disabledMcpjsonServers` | `<repo-root>/.claude/settings.local.json` |

`<repo-root>` is the git repository root, and for a git worktree the main checkout's root ([settings](https://code.claude.com/docs/en/settings.md), "Where Claude Code keeps the local file in a git repository"; [permissions](https://code.claude.com/docs/en/permissions.md), trust keyed on repo root).

## Settings precedence (applies to every settings key below)

Highest first: managed settings, command line (`--settings`), `.claude/settings.local.json`, `.claude/settings.json`, `~/.claude/settings.json` ([settings](https://code.claude.com/docs/en/settings.md), "Settings precedence"). Lists merge across files instead of overriding ("Lists merge instead of overriding").

`.claude/settings.local.json` is the per-user, per-project, never-committed file. Claude Code adds `**/.claude/settings.local.json` to the global git excludes the first time it writes it ([settings](https://code.claude.com/docs/en/settings.md), "Keep personal settings out of a repository"). In a git repo it reads and writes the file at the repository root, even when started in a subdirectory, and in a worktree at the main checkout's root (since v2.1.211). A file an older version left in the starting directory is still read; the root's value wins per key.

`~/.claude.json` is a separate file Claude Code writes for itself: sign-in, MCP server definitions, and per-project state under `projects["<path>"]` ([settings](https://code.claude.com/docs/en/settings.md), "Find or create your settings files"). It is not part of the settings precedence stack.

## Skills

### Where skills are discovered

From [skills](https://code.claude.com/docs/en/skills.md), "Choose where skills load":

| Location | Path | Command name |
| --- | --- | --- |
| Enterprise | `.claude/skills/<name>/SKILL.md` in the managed settings dir | `/<dir>` |
| Personal | `~/.claude/skills/<name>/SKILL.md` | `/<dir>` |
| Project | `.claude/skills/<name>/SKILL.md`, in the start dir and every parent up to the repo root (worktree: up to the worktree root, falling back to the main checkout's skills when the worktree has none, v2.1.277+) | `/<dir>` |
| Nested | `<subdir>/.claude/skills/...`, loaded once Claude touches files there | `/<dir>`, or `/<subdir-path>:<dir>` on a clash |
| Additional dir | `.claude/skills/` in a `--add-dir` dir | `/<dir>` |
| Legacy commands | `.claude/commands/*.md` (and `~/.claude/commands/`) | file name, subdirs joined with `:` |
| Plugin | `<plugin>/skills/<name>/SKILL.md` | `/<plugin>:<name>` |
| claude.ai account | downloaded to `~/.claude/skills/synced/` (v2.1.273+), switched off by `syncClaudeAiSkills: false` | `/anthropic-skills:<name>`, or bare `/<name>` when free |
| Bundled | shipped in the binary, switched off by `disableBundledSkills: true` | `/<name>` |

Same-name precedence: enterprise over personal over project; any of those replaces a bundled skill of that name; a skill beats a `.claude/commands/` file; plugin skills never clash because they are namespaced; a synced skill loses to any other skill of that name ([skills](https://code.claude.com/docs/en/skills.md), "Resolve skills that share a name"). A personal or project skill's command comes from its directory name; frontmatter `name` is only a display label there ("How a skill gets its command name").

Symlinked skill folders are followed and deduplicated by target ("Choose where skills load").

### `~/.agents/skills`

Claude Code does not load `~/.agents/skills` or a project's `.agents/skills`. The docs never mention either path. In the 2.1.282 binary the only references are inside an undocumented Cursor-config importer that reads `~/.cursor/skills`, `~/.agents/skills`, and `<project>/.agents/skills` and writes copies into `.claude/skills` (`sourceLabel:"~/.agents/skills"`, `skillsOut: .../.claude/skills`).

On this machine, `~/.agents/skills` (managed by a skills installer, `~/.agents/.skill-lock.json`) reaches Claude Code only through links: `~/.claude/skills/agent-browser -> ../../.agents/skills/agent-browser` and `ast-grep` likewise. Every other skill in `~/.claude/skills` is a symlink straight to its source repo or a real directory. So for Claude Code, a skill in `~/.agents/skills` is a personal skill only if something links or copies it into `~/.claude/skills`.

### Setting a skill's state

`skillOverrides`, an object mapping skill name to a state ([settings-reference](https://code.claude.com/docs/en/settings-reference.md#skilloverrides), [skills](https://code.claude.com/docs/en/skills.md), "Override skill visibility from settings"):

| Value | Listed to Claude | In `/` menu | equip State |
| --- | --- | --- | --- |
| `"on"` (or absent) | name and description | yes | on |
| `"name-only"` | name only | yes | (no equip equivalent; counts as on) |
| `"user-invocable-only"` | hidden | yes | Manual-only |
| `"off"` | hidden | hidden, and invoking by full name returns an error | off |

- Scope: any settings file. The `/skills` menu writes it to `.claude/settings.local.json`, so that file is the native per-project, per-user place.
- Keys are skill names. In user, project, and local settings Claude Code matches only the skill's own name, not a bundled alias (`review` does not reach `/code-review`).
- Plugin skills are not affected: "Plugin skills are not affected by `skillOverrides`. Manage those through `/plugin` instead." The binary agrees: the override resolver returns early for `e.source==="plugin"`.
- The bundled `/doctor` skill's own instructions (in the binary) give the same recipe: ``"skillOverrides": {"<name>": "off"}`` in `.claude/settings.local.json` for a project skill, or `~/.claude/settings.json` for a skill from `~/.claude/skills`.
- The frontmatter alternative `disable-model-invocation: true` gives the same effect as `"user-invocable-only"` but edits the skill file, which equip must not do.

Verified locally: `~/.claude/settings.json` has `skillOverrides` with `"off"` and `"user-invocable-only"` values; `supermatt/.claude/settings.local.json` has `{"ask-supermatt":"off","setup-supermatt-skills":"off"}`; `.dotfiles/.claude/settings.local.json` has `{"orca-cli":"off"}`.

## Plugins

### Where plugins are discovered

Every plugin has an id `<name>@<origin>` ([plugins/loading](https://code.claude.com/docs/en/plugins/loading.md), "Find where a plugin came from"):

| Origin suffix | Source | Default when no `enabledPlugins` entry |
| --- | --- | --- |
| `@<marketplace>` | installed from a marketplace; records in `~/.claude/plugins/installed_plugins.json`, files in `~/.claude/plugins/cache/` | manifest `defaultEnabled` (default `true`) |
| `@skills-dir` | a dir with `.claude-plugin/plugin.json` under `~/.claude/skills/` or the project's `.claude/skills/` (project one needs workspace trust, primary working dir only) | manifest `defaultEnabled` |
| `@synced` | enabled on the claude.ai account, downloaded to `~/.claude/plugins/synced/` | on |
| `@inline` | `--plugin-dir`, `--plugin-url`, `CLAUDE_CODE_PLUGIN_DIRS`, SDK | on, for that session |

For a marketplace plugin the key uses the marketplace entry name; components are namespaced under the manifest `name` ("Entry name and manifest name"). Claude Code does not scan a project's `.claude/plugins/`.

`installed_plugins.json` (version 2 here) maps each id to a list of installs with `scope` (`user`, `project`, `local`, `managed`), `installPath`, `version`, and for `project`/`local` a `projectPath` ([plugins/cli-reference](https://code.claude.com/docs/en/plugins/cli-reference.md), `plugin list --json`). Locally, `compound-engineering@compound-engineering-plugin` has a `user` install plus `local` and `project` installs with different `projectPath`s. The install scope decides which settings file `/plugin install` writes `enabledPlugins` into ([plugins/install](https://code.claude.com/docs/en/plugins/install.md), "Choose an install scope"); whether the plugin loads is decided by the merged `enabledPlugins`.

### Setting a plugin's state

`enabledPlugins`, an object mapping `<name>@<origin>` to `true` or `false` ([settings-reference](https://code.claude.com/docs/en/settings-reference.md#enabledplugins)). Sources merge key by key, lowest to highest: `--add-dir` settings (only `true` counts), user, project, local, `--settings`, managed ([plugins/loading](https://code.claude.com/docs/en/plugins/loading.md), "Find where a plugin is enabled"). So a `false` in `~/.claude/settings.json` does not beat a project `true`; the docs name `.claude/settings.local.json` as the place to opt out of a project-enabled plugin. Managed `true`/`false` cannot be overridden.

- On: `true`. Off: `false`. Manual-only: none. A plugin is all or nothing.
- `claude plugin enable|disable <id> --scope local` writes the same key ([plugins/cli-reference](https://code.claude.com/docs/en/plugins/cli-reference.md)).
- A plugin set `true` only in a local file but not yet on disk: Claude Code fetches external-source plugins when an untracked `.claude/settings.local.json` sets them `true` ([plugins/loading](https://code.claude.com/docs/en/plugins/loading.md), "Enabled in project settings but not installed").
- Enabling a plugin also enables its dependencies; disabling fails while an enabled plugin depends on it (`plugin enable`/`disable` docs).

Verified locally: `blog-test/.claude/settings.local.json` sets many `name@marketplace` ids to `false` and one to `true`.

### Do overrides reach extensions inside a plugin?

- Skills inside a plugin: no. `skillOverrides` ignores them (docs and binary). The only control is the whole plugin.
- MCP servers inside a plugin: yes. A plugin server registers as `plugin:<plugin-name>:<server-name>` ([mcp](https://code.claude.com/docs/en/mcp.md), "Plugin-provided MCP servers"), and the `/mcp` toggle writes that name to `disabledMcpServers`. Locally: `QPM-Production` has `["plugin:github:github","plugin:context7-plugin:context7"]`, `remont-lzk` has `["plugin:sentry:sentry"]`. Its tools are named `mcp__plugin_<plugin>_<server>__<tool>`.

## MCP servers

### Where MCP servers are discovered

[mcp](https://code.claude.com/docs/en/mcp.md), "MCP installation scopes" and "Scope hierarchy and precedence":

| Source | Stored in |
| --- | --- |
| Local scope | `~/.claude.json` → `projects["<path>"].mcpServers` |
| Project scope | `.mcp.json` at project root (needs per-server approval) |
| User scope | `~/.claude.json` → top-level `mcpServers` |
| Plugin | plugin's `.mcp.json` or `plugin.json` `mcpServers` |
| claude.ai connectors | fetched from claude.ai when signed in with a subscription; named `claude.ai <Display Name>` |
| Managed | `managedMcpServers` in managed settings, or `managed-mcp.json` |
| Built-in | e.g. `claude-in-chrome`, `computer-use` (reserved names) |

Same server in several places: one connection, whole entry from the highest source. Order: managed-provided, then local, project, user, plugin, claude.ai connectors. The three scopes match duplicates by name; plugins and connectors match by endpoint (URL or command).

### Setting an MCP server's state

Two unrelated mechanisms ([mcp](https://code.claude.com/docs/en/mcp.md), "Disable a server without removing it" and "Project server approvals and workspace trust"):

1. `/mcp` toggle, stored per project in `~/.claude.json` under `projects["<path>"]`:
   - `disabledMcpServers`: opt-out list for user and local servers, plugin servers (`plugin:<plugin>:<server>`), managed-provided servers, claude.ai connectors (`claude.ai Gmail`), and default-on built-ins.
   - `enabledMcpServers`: opt-in list for default-off built-ins such as `computer-use`.
   - Claude Code consults exactly one list per server. The binary's check is `is(name)`: default-off built-in → on if in `enabledMcpServers`; any other → off if in `disabledMcpServers`.
2. Project `.mcp.json` approval, in settings files (`/mcp` approval dialog writes `.claude/settings.local.json`):
   - `enabledMcpjsonServers`: names approved. `enableAllProjectMcpServers: true` approves all.
   - `disabledMcpjsonServers`: names rejected; wins over both approvals, in any settings file.
   - Unapproved and unrejected servers stay "Pending approval" in interactive sessions, but `claude -p`, SDK, and cloud sessions load them without asking ([mcp](https://code.claude.com/docs/en/mcp.md), "Project scope"). So for equip "off" must be an explicit `disabledMcpjsonServers` entry, not merely absence from the approved list.

The bundled `/doctor` prompt in the binary states the same split: user/local servers via `/mcp disable <server>` (persists to `disabledMcpServers` in the project entry of `~/.claude.json`, per project even for a user-scope server); `.mcp.json` servers via `disabledMcpjsonServers` in `.claude/settings.local.json`.

- Manual-only: none. MCP servers have no manual-only state (tool search defers schemas, but that is not a per-server setting).
- Blunter tools, not per-server state: `disableClaudeAiConnectors: true` (any file, any-source-true wins) turns off all fetched connectors; `deniedMcpServers` (any file, lists merge) blocks by name, command, or URL and cannot be re-enabled from `/mcp` ([settings-reference](https://code.claude.com/docs/en/settings-reference.md#deniedmcpservers)).

Verified locally: 8 `projects` entries carry non-empty `disabledMcpServers` with `claude.ai ...` and `plugin:...:...` names. Every one of the 870 `projects` entries also has empty `enabledMcpjsonServers`/`disabledMcpjsonServers` arrays; in the binary these are defaults of the per-project config object (`Uae`), while the approval check reads settings sources. Current approvals on this machine live in settings files (`svyatov.com` and `podbor_bot` `.claude/settings.local.json` have `enabledMcpjsonServers`).

## Open questions

1. `skillOverrides` merging across files. `enabledPlugins` is documented to merge key by key. For `skillOverrides` the docs do not say. The binary resolves a skill's value from the merged settings object and lists `skillOverrides` alongside `enabledPlugins` in the same object-valued key set, and local data (user file with 9 entries, project local files with others) only makes sense with a per-key merge. Likely per-key; confirm with a live session before equip relies on it.
2. The exact `projects[...]` key for `disabledMcpServers` when the session starts in a subdirectory or worktree. Trust and `settings.local.json` both key on the repo root (main checkout for worktrees); all local entries observed are repo roots. Assumed the same; unverified.
3. Whether the per-project `enabledMcpjsonServers`/`disabledMcpjsonServers` arrays in `~/.claude.json` are still honored (legacy) or only carried as defaults. equip should write the settings-file keys only.
4. `plugin:<plugin-name>:<server-name>` uses the plugin's manifest name or its marketplace entry name when they differ. Docs say "plugin name"; components are namespaced by manifest name, so probably the manifest name.
5. claude.ai-synced skills: the binary looks them up under extra names (an `anthropic-skills:`-style prefix first). Which key form equip should write for them is unverified.

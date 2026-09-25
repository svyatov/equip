# How to estimate an extension's context cost

Research for ticket #4. Question: what does each **Extension** kind put into a session's context in Claude Code and in Codex, and how can equip estimate that in tokens without starting an **Agent** session? Can MCP tool lists be read without starting the server, is a rough chars/4 estimate close enough, and are skill descriptions capped or truncated?

Sources, fetched 2026-09-25: the Claude Code docs at `code.claude.com/docs/en/*.md` (found via `llms.txt`), the Codex docs at `learn.chatgpt.com/docs/*.md` (where `developers.openai.com/codex/llms.txt` now points), the `openai/codex` source at `main` commit `aa38089` (2026-09-25), the MCP specification, and the Anthropic token counting docs. Local checks ran Claude Code 2.1.282 and codex-cli 0.155.1 on this machine.

## Answer in one table

"Always-on" is what the extension adds to every turn whether or not it is used. "On use" is what loads only when the model or the user invokes it; equip should show it separately or not at all, since the session total is about the always-on part.

| Extension | Agent | Always-on text | Cap | Manual-only | Off |
| --- | --- | --- | --- | --- | --- |
| Skill | Claude Code | One listing line: name, `description` + `when_to_use` | 1,536 chars per skill; whole listing 1% of the context window | 0 | 0 |
| Skill | Codex | One listing line: name, `description`, short path | 1,024 chars per description; whole listing 2% of the context window | 0 | 0 |
| Plugin | Claude Code | Sum of its skills, commands and agents (name + description each), plus its MCP servers | as for each part | n/a | 0 |
| Plugin | Codex | Sum of its skills (listed as `plugin:skill`), plus its MCP servers | as for each part | n/a | 0 |
| MCP server, tool search on (default) | Claude Code | Every tool's name (`mcp__server__tool`), plus the server's `instructions` | instructions 2,048 chars | n/a | 0 |
| MCP server, tool search on (default) | Codex | The server's name and `instructions`, inside the `tool_search` tool description; tool names are not listed | none in practice (512 KiB) | n/a | 0 |
| MCP server, tool search off or exempt | both | Full tool definitions: name, description, JSON input schema, plus instructions | Claude Code: 2,048 chars per tool description | n/a | 0 |

Recommended estimator: tokens ≈ UTF-8 bytes of the exact rendered text ÷ 4 for Codex and ÷ 3 for Claude Code. Skills and plugins can be estimated from files alone. MCP servers need one short `tools/list` handshake, which equip should run only on demand, only for servers the agent already trusts, and then cache.

## Skills

### Claude Code

What enters context: a skill listing with every skill's name and description. The full `SKILL.md` body loads only when the skill runs, then stays in context for the rest of the session ([skills](https://code.claude.com/docs/en/skills.md), "Skill content lifecycle"; [context window](https://code.claude.com/docs/en/context-window.md), "Skill descriptions").

Per-skill cap: "the combined `description` and `when_to_use` text is truncated at 1,536 characters in the skill listing" ([skills](https://code.claude.com/docs/en/skills.md), frontmatter table). The cap is the `skillListingMaxDescChars` setting ([settings reference](https://code.claude.com/docs/en/settings-reference.md)). If `description` is missing, Claude Code uses the first non-empty line of the body.

Session budget: the whole listing gets 1% of the model's context window, in characters. Over budget, every name stays but Claude Code drops descriptions, least-invoked skills first. The budget is `skillListingBudgetFraction` or the `SLASH_COMMAND_TOOL_CHAR_BUDGET` env var ([skills](https://code.claude.com/docs/en/skills.md), "Skill descriptions are cut short"). equip cannot see invocation counts, so it should add up the uncapped estimates, show the sum, and flag when the sum passes the budget.

State and cost ([skills](https://code.claude.com/docs/en/skills.md), "Control who invokes a skill" and "Override skill visibility from settings"):

| How set | Listed to Claude | Cost |
| --- | --- | --- |
| default, `skillOverrides: "on"` | name and description | full line |
| `skillOverrides: "name-only"` | name only | name only |
| `disable-model-invocation: true`, or `skillOverrides: "user-invocable-only"` | hidden | 0 |
| `skillOverrides: "off"` | hidden | 0 |
| `user-invocable: false` | name and description | full line |

`skillOverrides` does not apply to plugin skills. After `/compact` the listing is not re-injected; only invoked skills come back, at up to 5,000 tokens each and 25,000 in total ([skills](https://code.claude.com/docs/en/skills.md), "Skill content lifecycle"). equip can ignore this, since it describes the start of a session.

Subagents in `.claude/agents/` and plugin `agents/` work the same way: name and description always on, system prompt on use. Claude Code warns once custom agent descriptions pass 15,000 tokens ([subagents](https://code.claude.com/docs/en/sub-agents.md)).

### Codex

What enters context: a developer message `<skills_instructions>` with a fixed intro and how-to block, a `### Skill roots` alias table, and one line per skill in the form `- {name}: {description} (file: {alias}/{dir}/SKILL.md)` ([build skills](https://learn.chatgpt.com/docs/build-skills.md), "progressive disclosure"; source `codex-rs/ext/skills/src/render.rs`, `render_with_description`, and `catalog_prompt.rs`). The fixed block is about 2,800 bytes, roughly 700 tokens, paid once if any skill is listed.

Per-skill cap: descriptions longer than 1,024 characters are cut to 1,021 plus `...` (`MAX_CATALOG_SKILL_DESCRIPTION_CHARS` in `render.rs`). The optional `agents/openai.yaml` `interface.short_description` is also capped at 1,024 (`codex-rs/skills/src/interface.rs`).

Session budget: 2% of the model's context window in tokens, or 8,000 characters when the window is unknown. Over budget, Codex shortens descriptions first and then drops skills with a warning ([build skills](https://learn.chatgpt.com/docs/build-skills.md); `skill_metadata_budget` in `render.rs`). `skills.max_context_tokens` overrides it, up to 10,000 ([config reference](https://learn.chatgpt.com/docs/config-file/config-reference.md)). The bundled models all have a 272,000-token window (`codex-rs/models-manager/models.json`), so the default budget is 5,440 tokens. Codex counts this budget with the same heuristic this doc recommends: `approx_token_count` is UTF-8 bytes ÷ 4, rounded up (`codex-rs/utils/string/src/truncate.rs`).

State and cost:

| How set | Cost |
| --- | --- |
| default | full line |
| `policy.allow_implicit_invocation: false` in `agents/openai.yaml` | 0: the entry is `hidden_from_prompt()` (`codex-rs/ext/skills/src/provider/host.rs`), `$skill` still works |
| `[[skills.config]] enabled = false` in `config.toml` | 0 |

Checked on this machine: `codex debug prompt-input` printed the exact model-visible skills block without calling a model or starting MCP servers. It was 23,815 characters, and some descriptions were already cut mid-word, as the budget describes.

## Plugins

Claude Code: "Every session where a plugin is enabled includes the names and descriptions of its skills, agents, and commands" ([measure plugin cost](https://code.claude.com/docs/en/plugins/measure.md)). Commands count as skills. Hooks are listed as "harness-only, no model context cost", and MCP servers as "tool schemas resolved at runtime; not counted". A plugin root `CLAUDE.md` is not loaded ([plugin components](https://code.claude.com/docs/en/plugins/components.md)). So a plugin's always-on cost is the sum of its skill, command, and agent listing lines, plus the cost of each MCP server it ships.

Claude Code has its own estimator: `claude plugin details <name>` prints an always-on total and per-component always-on and on-invoke figures without starting a session ([CLI reference](https://code.claude.com/docs/en/plugins/cli-reference.md), "plugin details"). It prints text only, has no JSON flag, and needs one process per plugin. equip can use it to check its own numbers but should not parse it.

Codex: a plugin bundles skills, MCP servers, and apps ([build plugins](https://learn.chatgpt.com/docs/build-plugins.md)). Its skills appear in the skills list with a `plugin_name:` prefix, and its MCP tools look like any other server's. Codex adds a fixed `## Plugins` usage block, about 1,000 bytes, once per session (`codex-rs/core/src/context/available_plugins_instructions.rs`). The plugin's cost is the sum of its skills and servers.

Hooks are the gap in both agents. A hook's output can reach the model: Claude Code puts `additionalContext` from `SessionStart`, `UserPromptSubmit` and other hooks into context ([hooks](https://code.claude.com/docs/en/hooks.md), "Add context for Claude"), and Codex does the same, spilling anything over `additionalContextLimit` (default 2,500 tokens) to disk ([config reference](https://learn.chatgpt.com/docs/config-file/config-reference.md)). This session is an example: a plugin's `SessionStart` hook injected about 1,000 tokens of instructions, while `claude plugin details` reported that plugin's hooks as costing nothing. equip cannot know hook output without running the hook, so it should mark such plugins as "+ hook output, unknown" and not guess.

## MCP servers

### Claude Code

With tool search, the default: "Only tool names and server instructions load at session start". Definitions are fetched through the `ToolSearch` tool when needed ([MCP](https://code.claude.com/docs/en/mcp.md), "Scale with MCP tool search"). Tool descriptions and server instructions are each truncated at 2,048 characters, set by `CLAUDE_CODE_MAX_MCP_DESCRIPTION_LENGTH` (same section).

Full definitions load up front when:

- `ENABLE_TOOL_SEARCH=false`;
- `ENABLE_TOOL_SEARCH=auto` and all definitions together are under 10% of the context window (`auto:N` sets another percent);
- `ANTHROPIC_BASE_URL` points to a host that is not first-party, unless `ENABLE_TOOL_SEARCH` is set;
- `CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS` is set, the model is older than the 4.5 generation, or the deployment rejects tool search;
- the server has `alwaysLoad: true`, or a tool sets `_meta["anthropic/alwaysLoad"]`

([MCP](https://code.claude.com/docs/en/mcp.md), "Configure tool search" and "Exempt a server from deferral"). equip should read these and switch between the two formulas.

Checked on this machine: `claude -p "/context"` reported "MCP tools (deferred)" at 16.9k tokens, which is the size of the definitions if loaded, and left that figure out of the 19.9k session total. The deferred names showed up in context as a plain list, and the server instructions as a `# MCP Server Instructions` section.

MCP prompts become `/server:prompt` commands and are injected only when run ([MCP](https://code.claude.com/docs/en/mcp.md), "Use MCP prompts as commands").

### Codex

Codex defers MCP tools when the model has `supports_search_tool` and the provider supports namespaced tools (`search_tool_enabled` in `codex-rs/core/src/tools/spec_plan.rs`). Every model in the bundled catalog sets `supports_search_tool: true`, so deferral is the default with OpenAI. Deferred tools are reachable only through `tool_search`, whose description lists each source as `- {server name}: {instructions}` (`create_tool_search_tool` in `codex-rs/core/src/tools/handlers/tool_search_spec.rs`; the source is built in `handlers/mcp.rs` `search_info`). Tool names are not listed. The only cap on these descriptions is 512 KiB for the whole list, so in practice instructions are not cut. The docs ask server authors to "keep the first 512 characters self-contained" ([MCP](https://learn.chatgpt.com/docs/extend/mcp.md)), but I found no code that cuts at 512.

Without deferral, each tool goes out as a namespaced function whose namespace description is the server's instructions. Input schemas over `tool_input_schema_max_bytes` (default 5,000 bytes) are compacted (`codex-rs/config/src/mcp_types.rs`). `enabled_tools` and `disabled_tools` filter the list, and `omit_tools_from` can keep tools off given surfaces ([MCP](https://learn.chatgpt.com/docs/extend/mcp.md); `apply_mcp_tool_exposure_policy` in `spec_plan.rs`).

`codex debug prompt-input` does not show tools, so it cannot check MCP estimates.

### Reading tool lists without a session

A server's config gives only its name, command or URL. Tool names, descriptions, schemas and instructions exist only in the server's answers, so there is no way to read them from files:

- Claude Code has a discovery cache for remote HTTP and SSE servers used before, but it is off unless a rollout or `MCP_DISCOVERY_CACHE=1` turns it on. The docs don't give its location, so it is not a stable interface ([MCP](https://code.claude.com/docs/en/mcp.md), "Server status detail").
- Codex caches the tools of its own ChatGPT apps server in `~/.codex/cache/codex_apps_tools`, but not those of user MCP servers.

So equip needs a short handshake per server. Under MCP 2025-11-25 and earlier that is `initialize`, `notifications/initialized`, `tools/list` (following `nextCursor` pages), with `instructions` in the `initialize` result ([lifecycle](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle.md), [tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools.md)). MCP 2026-07-28 drops `initialize`: `server/discover` returns `instructions` with a `ttlMs` cache hint, and `tools/list` results carry `ttlMs` too. On stdio, a client should try `server/discover` first and fall back to `initialize` on error ([changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog.md), [versioning](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning.md), [caching](https://modelcontextprotocol.io/specification/2026-07-28/server/utilities/caching.md)).

Speed, measured here with a throwaway Ruby client: a local stdio server (`codegraph serve --mcp`) answered in 0.10 s, and a remote HTTP server (`developers.openai.com/mcp`) in 0.86 s. Servers started through `npx` or `uvx` can take seconds on a cold package cache. The agents' own limits show what to expect: Claude Code gives a server 5 seconds to connect ([MCP](https://code.claude.com/docs/en/mcp.md), "Exempt a server from deferral"), and Codex's docs say `startup_timeout_sec` defaults to 10, though the source default is 30 s (`DEFAULT_STARTUP_TIMEOUT` in `codex-rs/codex-mcp/src/rmcp_client.rs`).

Safety: a handshake runs the server's command with its env, the same as the agent would. That is arbitrary code, may download packages, and may need secrets. A project `.mcp.json` server in Claude Code is not trusted until the user approves it (see `docs/research/claude-code-loading.md` on branch `research/claude-code-loading`). Remote servers behind OAuth answer 401 without a token, and equip should not read agent token stores. So:

- never probe on startup or in the background;
- probe only servers the agent already has enabled and trusted, on an explicit user action, with a timeout of about 5 s, then end the process;
- cache the result keyed on a hash of the server config, and honor `ttlMs` when the server sends it;
- until a server is probed, show its cost as unknown, not zero.

## Is chars/4 close enough?

For Codex, yes. Codex budgets its own prompt with bytes ÷ 4 (`approx_token_count`, `APPROX_BYTES_PER_TOKEN = 4`), so equip would match the agent's own arithmetic.

For Claude Code, chars/4 runs about 30% low on current models. Claude Opus 4.7 and later use a tokenizer that makes "approximately 30 percent more tokens" for the same text, and later models share it ([token counting](https://platform.claude.com/docs/en/build-with-claude/token-counting.md)). Claude Code's own estimates agree. On this machine:

| Text | Chars | chars/4 | Claude Code estimate | chars per token |
| --- | --- | --- | --- | --- |
| `ponytail:ponytail` listing line | 847 | 211 | ~310 (`plugin details`), ~280 (`/context`) | 2.7 to 3.0 |
| same skill's body | 5,680 | 1,425 | ~2.2k (`plugin details`) | 2.6 |
| `ce-pov` listing line | 362 | 90 | ~120 | 3.0 |
| `ce-debug` body | 16,519 | 4,148 | ~5.6k | 2.9 |
| `codegraph_explore` full definition (JSON) | 1,701 | 427 | 625 (`/context`) | 2.7 |

So bytes ÷ 3 for Claude Code and bytes ÷ 4 for Codex. Both are within about ±20% for English prose and JSON, which is enough to rank extensions and total a session. equip should show them with `~` and round them the way `claude plugin details` does. Exact counts need the Anthropic `count_tokens` endpoint, which is free but needs an API key ([token counting](https://platform.claude.com/docs/en/build-with-claude/token-counting.md)). That is not worth adding to equip.

## Open questions

- Claude Code's per-line listing format and fixed listing header are not documented. The ÷ 3 rule was fitted to its estimates, not to its source.
- The MCP deferred-name format in Claude Code is taken from what this session saw. The per-tool overhead beyond the name is not documented.
- Hook output is unknowable without running the hook.
- Codex also injects a `<recommended_plugins>` block of plugins that are not installed, about 8,000 characters here. It is not tied to any installed extension, so equip should leave it out of the totals.
- The Codex docs and source disagree on the default `startup_timeout_sec` (10 or 30 s).

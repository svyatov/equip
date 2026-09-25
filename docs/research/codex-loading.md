# What Codex loads per project, and which settings toggle it

Research for ticket #3. Question: for a given **Project**, which **Skills**, **Plugins** and **MCP servers** does a Codex CLI session load, from where, with what precedence, and is there a per-project way to set each one's **State** (on, manual-only, off)?

Checked against Codex CLI 0.155.1 (the version installed here), its source at tag [`rust-v0.155.1`](https://github.com/openai/codex/tree/rust-v0.155.1) (commit `be2951ea`), and the official docs. `https://developers.openai.com/codex/llms.txt` now indexes the Codex docs at `learn.chatgpt.com/docs/*.md`; links below point there. Source links are abbreviated as `S/…` = `https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/…`.

## Answer in one table

| Extension | Discovered in | On / off per project | Manual-only per project |
| --- | --- | --- | --- |
| Skill | see [Skills](#skills) | **No config file does it.** `[[skills.config]]` in a project `.codex/config.toml` is ignored. Only the user layer, a `--profile` file, or `-c skills.config=[...]` on the command line count. | **None.** Only `policy.allow_implicit_invocation: false` in the skill's own `agents/openai.yaml`, which is global to that skill folder. |
| Plugin | `[plugins."name@marketplace"]` keys in the merged config, files in `$CODEX_HOME/plugins/cache/<marketplace>/<plugin>/<version>/` | **Yes:** `[plugins."name@mkt"] enabled = true/false` in the project's `.codex/config.toml`, trusted projects only. Verified. | n/a (the glossary defines manual-only for skills only; a plugin's skills follow the skill rules) |
| MCP server | `[mcp_servers.<id>]` in the merged config; plugin-bundled servers from the plugin's `.mcp.json` | **Yes:** `[mcp_servers.<id>] enabled = false` in the project's `.codex/config.toml`, trusted projects only. Verified. Plugin-bundled: `[plugins."p@m".mcp_servers.<s>] enabled`, documented, not verified from a project layer. | n/a |

Every per-project path needs the project to be **trusted**, and trust itself is only stored per user: `[projects."<abs path>"] trust_level = "trusted"` in `~/.codex/config.toml`.

## Config layers and precedence

Codex merges these layers, highest precedence first ([Config basics](https://learn.chatgpt.com/docs/config-file/config-basic.md), "Configuration precedence"; numeric order in [`S/config/src/config_layer_source.rs`](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/config/src/config_layer_source.rs#L33-L51)):

1. CLI flags and `-c` / `--config` overrides (`SessionFlags`, 30)
2. Project `.codex/config.toml` files from the project root down to the cwd, closest wins, trusted projects only (`Project`, 25)
3. Profile file `$CODEX_HOME/<name>.config.toml`, only when launched with `--profile <name>` (`User` with a profile, 21)
4. User config `$CODEX_HOME/config.toml` (`User`, 20)
5. Cloud-managed defaults, then 6. system `/etc/codex/config.toml`, then 7. built-in defaults

(Legacy `managed_config.toml` layers sit above all of these in the code, at 40 and 50.)

Facts that matter for equip:

- **Project root** is the nearest ancestor containing a `project_root_markers` entry, default `[".git"]` ([Advanced config](https://learn.chatgpt.com/docs/config-file/config-advanced.md), "Project root detection"). Every directory from that root down to the cwd can have its own `.codex/`.
- **Trust gate.** An untrusted or unknown project still gets its project layers built, but marked disabled, so their `config.toml` values are not merged ([`S/config/src/loader/mod.rs` L1111-L1130](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/config/src/loader/mod.rs#L1111-L1130), [L1686](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/config/src/loader/mod.rs#L1686)). Trust is read from the merged `projects` table *before* project layers load, so it can come from the user config or from `-c` ([L366-L410](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/config/src/loader/mod.rs#L366-L410)), never from the project itself.
- **Project denylist.** A project layer can't set `openai_base_url`, `chatgpt_base_url`, `apps_mcp_product_sku`, `responses_api_metadata`, `model_provider(s)`, `notify`, `profile`, `profiles`, the realtime base URLs, or `otel` ([L78-L91](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/config/src/loader/mod.rs#L78-L91)). `skills`, `plugins`, `mcp_servers` and `marketplaces` are **not** on it, so they merge. So a project can't pick a profile.
- **Profiles** are no longer `[profiles.x]` tables; since 0.134.0 `--profile x` reads `$CODEX_HOME/x.config.toml` ([Advanced config](https://learn.chatgpt.com/docs/config-file/config-advanced.md), "Profiles"). A profile is chosen per launch, not per directory.
- **`-c` keys are split on every `.` with no quote handling** ([`S/config/src/overrides.rs` L22](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/config/src/overrides.rs#L22)). `-c 'plugins."astro@mkt".enabled=false'` and `-c 'projects."/path".trust_level=...'` silently do nothing. Use an unquoted key when the name has no dots (`-c plugins.astro@mkt.enabled=false`) or an inline table (`-c 'projects={"/abs/path"={trust_level="trusted"}}'`). Both verified.
- **`CODEX_HOME`** moves the whole user layer: `config.toml`, profiles, `$CODEX_HOME/skills`, the plugin cache, auth and history ([Advanced config](https://learn.chatgpt.com/docs/config-file/config-advanced.md), "Config and state locations"). A per-project `CODEX_HOME` works as a switch, but equip would have to link auth, cache and history into each one.

## Skills

### Where they come from

Roots, from [`S/ext/skills/src/host_roots.rs`](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/ext/skills/src/host_roots.rs#L73-L135) and [Build skills](https://learn.chatgpt.com/docs/build-skills.md), "Where Codex loads local skills":

| Scope | Path | Notes |
| --- | --- | --- |
| Repo | `<dir>/.agents/skills` for every `<dir>` from project root down to cwd | Loaded whether or not the project is trusted |
| Repo | `<dir>/.codex/skills` for every project `.codex/` layer | Not in the docs. The code walks *all* layers, including disabled ones, so this also loads in untrusted projects (verified) |
| User | `~/.agents/skills` | Always `$HOME`, not `CODEX_HOME` |
| User | `$CODEX_HOME/skills` | Commented in code as "Deprecated … kept for backward compatibility" |
| System | `$CODEX_HOME/skills/.system` | Bundled skills; `skills.bundled.enabled = false` turns them all off |
| Admin | `/etc/codex/skills` | From the system config layer |
| Plugin | the `skills/` folder of each **enabled** plugin in the cache | Named `<plugin>:<skill>` in the catalog (seen here as `astro:astro`) |

Roots are deduplicated by path. Skills with the same `name` are not merged; both show up. Symlinked skill folders are followed.

### Off: `[[skills.config]]`

```toml
[[skills.config]]
path = "/abs/path/to/skill/SKILL.md"   # or: name = "demo-a" / name = "plugin:skill"
enabled = false
```

- The rules are read **only from `User` layers (base and profile) and `SessionFlags`**. Project layers are skipped ([`S/config/src/skills_config.rs` L150-L158](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/config/src/skills_config.rs#L150-L158)). Verified: a trusted project's `.codex/config.toml` with `name = "demo-d"` left `demo-d` in the catalog. The docs don't state this limit.
- `path` must point at the `SKILL.md` file. Pointing at the folder did nothing (verified), although the [config reference](https://learn.chatgpt.com/docs/config-file/config-reference.md) says "Path to a skill folder containing `SKILL.md`". The path is canonicalized, so symlinks resolve.
- `name` matches every loaded skill with that name. For plugin skills it's the namespaced name: `name = "astro:astro"` hid it, `name = "astro"` did not (verified). The `name` selector isn't in the public docs, but it's in the schema (`SkillConfig { path, name, enabled }`, [L20](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/config/src/skills_config.rs#L20)).
- Later rules win, and the session layer comes last, so `-c 'skills.config=[{name="demo-a",enabled=false}]'` adds to the user's list rather than replacing it (verified).
- Plugin cache paths contain a version (`…/sites/0.1.31/skills/…`, as in this machine's config), so a `path` rule stops matching after a plugin update. `name` survives updates.

### Manual-only: `allow_implicit_invocation`

`agents/openai.yaml` next to `SKILL.md`:

```yaml
policy:
  allow_implicit_invocation: false
```

"When `false`, Codex won't implicitly invoke the skill based on user prompt; explicit `$skill` invocation still works" ([Build skills](https://learn.chatgpt.com/docs/build-skills.md), "Optional metadata"). The bundled `skill-creator` reference says it plainly: "the skill is not injected into the model context by default" (`~/.codex/skills/.system/skill-creator/references/openai_yaml.md`). Verified: `demo-b` with this flag never appears in the model-visible prompt. This is exactly the glossary's **Manual-only**.

No config key sets this. `SkillConfig` has only `path`, `name`, `enabled` with `deny_unknown_fields`. So it can't be set per project: the file lives in the skill folder, is shared by every project that loads that folder, gets committed for repo skills, and gets overwritten when a plugin cache refreshes. 43 of the plugin skills cached here already ship with it set to `false` (for example every `shortcuts@svyatov-agent-toolkit` skill).

## Plugins

- **Discovery:** marketplaces come from `[marketplaces.<name>]` in any layer, including trusted project config ([config reference](https://learn.chatgpt.com/docs/config-file/config-reference.md), `marketplaces.<name>.source_type`), plus `$REPO_ROOT/.agents/plugins/marketplace.json` and `~/.agents/plugins/marketplace.json` ([Build plugins](https://developers.openai.com/plugins/build/plugins.md), "Enable or disable a plugin for a repo"). Installed copies live in `~/.codex/plugins/cache/$MARKETPLACE/$PLUGIN/$VERSION/` (same page).
- **State:** "installed" means a `plugins."name@mkt"` key exists in the merged config *and* its files are in the cache; "enabled" means `enabled = true` ([`S/core-plugins/src/manager.rs` L3505-L3521](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/core-plugins/src/manager.rs#L3505-L3521)). The table comes from `config_layer_stack.effective_config()`, the merge of all enabled layers ([`S/core-plugins/src/marketplace_policy.rs` L210-L306](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/core-plugins/src/marketplace_policy.rs#L210-L306)).
- **Per project:** documented and verified. Build plugins: "Use the repo's `.codex/config.toml` to control whether a local-marketplace plugin is enabled for that project … Set `enabled = false` to disable the plugin for the project without uninstalling it." A trusted project with `[plugins."astro@svyatov-agent-toolkit"] enabled = false` removed `astro:astro` from the catalog. The same file in an untrusted project changed nothing.
- **Limits:** workspace-managed plugins (Admin > Plugins, e.g. the `openai-curated-remote` cache here) ignore this key. Marketplace refresh can still download disabled plugins.
- Disabling a plugin removes its skills and its bundled MCP servers together.

## MCP servers

- **Discovery:** `[mcp_servers.<id>]` from the merged config (user, profile, trusted project, `-c`, managed), plus servers bundled in enabled plugins' `.mcp.json` ([MCP](https://learn.chatgpt.com/docs/extend/mcp.md), "Plugin-provided MCP servers"). Skills can declare MCP dependencies in `agents/openai.yaml`, but that only prompts for installation.
- **Per project:** the MCP docs say you "can also scope MCP servers to a project with `.codex/config.toml` (trusted projects only)". Tables deep-merge, so a project file with only `[mcp_servers.codegraph] enabled = false` turns off a server defined in the user config. Verified with `codex mcp list --json` against a trusted copy of this machine's config. A partial entry for a server not defined anywhere else would fail to parse, since it has no `command`/`url`.
- Plugin-bundled servers: `[plugins."p@m".mcp_servers.<server>] enabled = false` ([config reference](https://learn.chatgpt.com/docs/config-file/config-reference.md)). Documented for `config.toml`. **Not verified** from a project layer.
- One-off: `-c mcp_servers.<id>.enabled=false` (documented example, verified).

## What this means for equip

| State | Skill | Plugin | MCP server |
| --- | --- | --- | --- |
| on | default | `enabled = true` in project `.codex/config.toml` | default, or `enabled = true` in project `.codex/config.toml` |
| manual-only | none per project | n/a | n/a |
| off | none per project in a file; per launch: `codex -c 'skills.config=[{name="…",enabled=false}]'` | `enabled = false` in project `.codex/config.toml` | `enabled = false` in project `.codex/config.toml` |

Constraints and workarounds:

1. **Project `.codex/config.toml` is inside the repo.** To stay "never committed", equip would have to write it and keep it out of git (for example `.git/info/exclude`). If the repo already commits a `.codex/config.toml`, equip would be editing a tracked file. That needs a decision.
2. **Trust is required** and lives in the user config (`[projects."<path>"] trust_level`). Trusting a project also turns on its project hooks and exec-policy rules, not just equip's toggles.
3. **Skills can't be switched off or set to manual-only per project by any file Codex reads.** The closest workarounds:
   - a launcher (shell function or `equip run codex`) that adds `-c 'skills.config=[...]'` for the cwd. This is the only no-file option, and it only works when Codex is started through it (not from the desktop app);
   - a per-project profile file `~/.codex/equip-<project>.config.toml` with `[[skills.config]]` entries, which still needs `--profile` on every launch;
   - a per-project `CODEX_HOME`, which is heavy;
   - for off only, `enabled = false` on the owning plugin, which is all-or-nothing for that plugin.
   Manual-only has no workaround short of rewriting the skill's own `agents/openai.yaml`, which is global and gets overwritten for plugin skills.
4. **Name selectors beat path selectors** for equip's global rules: they survive plugin version bumps. Plugin skills take the `plugin:skill` form.
5. **`-c` needs unquoted or inline-table keys** for plugin ids and paths (see above).

## Verification log

Codex 0.155.1, throwaway project `/private/tmp/codexres/proj` (with `.git/`) holding skills `demo-a`, `demo-b` (`allow_implicit_invocation: false`) under `.agents/skills/`, `demo-c`, `demo-d` under `.codex/skills/`, and a `.codex/config.toml` with `[[skills.config]] name="demo-d" enabled=false`, `[mcp_servers.codegraph] enabled=false`, `[plugins."astro@svyatov-agent-toolkit"] enabled=false`. The skills catalog was read from `codex debug prompt-input`, MCP state from `codex mcp list --json`. Trust came either from a temp `CODEX_HOME` holding a copy of the user config plus a `[projects]` entry, or from `-c 'projects={…}'`. `~/.codex` was not modified.

| Case | Catalog (probe skills) | codegraph enabled |
| --- | --- | --- |
| untrusted | `astro:astro, demo-a, demo-c, demo-d` | true |
| trusted | `demo-a, demo-c, demo-d` (plugin off, `demo-d` rule ignored) | false |
| `-c 'projects."/path".trust_level=…'` (quoted key) | unchanged from untrusted | true |
| `-c 'skills.config=[{name="demo-a",enabled=false}]'` | `demo-a` gone | |
| `-c 'skills.config=[{name="astro:astro",…}]'` / `name="astro"` | gone / still present | |
| `path=".../demo-a/SKILL.md"` / `path=".../demo-a"` | gone / still present | |
| `--profile probe` with `[[skills.config]] name="demo-c"` in `probe.config.toml` | `demo-c` gone | |

`demo-b` never appeared in the prompt in any case.

## Open questions

- Whether `[plugins."p@m".mcp_servers.<s>] enabled` is honored from a project layer (likely, since plugin config reads the merged config, but not tested).
- Whether the ChatGPT desktop app, which also runs Codex, reads project `.codex/config.toml` the same way the CLI does (the docs say the CLI and IDE extension share config layers).
- Loading `.codex/skills` from untrusted projects looks unintentional (the code iterates `all_layers_high_to_low`, which includes disabled layers) and could change in a later release.
- This is all Codex 0.155.1. The skills loader has moved between crates before (`core` to `ext/skills`), so equip should pin and re-check these behaviours per Codex release.

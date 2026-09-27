# Checking equip's counts against the agents

Use this when equip's list or a facet count disagrees with what Claude Code or Codex loads. Get the truth from each agent, get equip's rows, and diff them by path. Done when every difference is either fixed or on the expected list at the end.

## 1. equip's rows

From the repo, for the Project in `<dir>` (the working directory when left out):

```sh
go run ./scripts/rows <dir> > /tmp/equip-rows.tsv 2> /tmp/equip-facets.tsv
```

Each row is tab-separated: kind, name, plugin (set on a plugin's skill row), state, agents, Locations. The facet counts go to stderr. An agent's facet counts every row the agent has, in any state. To compare with a session, count only that agent's rows that are on or manual-only.

A plugin skill row's Locations are the plugin's dir. Its skill dir is `<plugin dir>/skills/<name>`.

## 2. Claude Code's truth

Run this from `<dir>`. It costs one short Haiku call:

```sh
claude -p "reply ok" --model haiku --output-format stream-json --verbose --max-turns 1 \
  | jq -c 'select(.type=="system" and .subtype=="init")' > /tmp/claude-init.json
```

The init event holds `.skills`, `.plugins` (name and install path), `.mcp_servers`, and `.slash_commands`.

- `.skills` includes By-name skills. A plugin's skill reads `plugin:skill`.
- A plugin's `commands/*.md` appear in `.slash_commands`, not in `.skills`. The model still sees them in its skill listing, and equip counts their cost but gives them no row.
- The skill listing in an interactive session's system prompt leaves out By-name skills, so it is the wrong source for counts.

## 3. Codex's truth

Run this from `<dir>`. It makes no model call:

```sh
codex debug prompt-input hi > /tmp/codex-prompt.json
codex plugin list
```

The prompt's skills section starts with a root table (`r0` = `~/.codex/skills`, and so on), then one line per skill, ending in `(file: rN/<path>/SKILL.md)`.

- Codex lists only the skills it may call on its own. A skill whose `agents/openai.yaml` has `allow_implicit_invocation: false` is loaded but absent from the listing.
- Codex names a skill by its frontmatter `name`, not its dir, so `gstack-autoplan/` lists as `autoplan`. Match by resolved path, never by name.
- The prompt does not list enabled plugins. `codex plugin list` gives each plugin's status, including remote ones that have no entry in `config.toml`.

## 4. Diff

Resolve every path with `realpath`, since skill dirs are often symlinks, then compare the sets. Of equip's skills that an agent does not list, check the By-name flag before calling one a bug: `disable-model-invocation: true` in `SKILL.md` hides it from Claude Code's listing, and `allow_implicit_invocation: false` in `agents/openai.yaml` hides it from Codex's.

## 5. Settle a loading rule

When the diff hinges on how an agent loads something (a symlink, a dir layout, a frontmatter field), test it in a scratch git repo. Build it with a Ruby script in a `mktemp -d` dir, put one control skill next to one variant skill under `.claude/skills/` or `.agents/skills/`, and run step 2 or step 3 inside it. This is how equip learned that Codex skips a skill whose `SKILL.md` file is itself a symlink, while Claude Code loads it.

## Expected differences

- Claude Code's built-ins have no row: the skills `design` and `doctor`, and the plugin `agents-md@builtin`.
- `claude-in-chrome` is on in interactive sessions but absent from `claude -p`.
- Codex remote plugins (`openai-curated-remote`) have no row, since equip reads plugins from `config.toml` only.
- A Codex plugin that is enabled in `config.toml` and cached, but gone from its marketplace, still has a row. Whether Codex loads it is unknown.

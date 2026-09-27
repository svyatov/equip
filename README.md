# equip

A terminal UI for Claude Code and Codex users that lists the skills, plugins, and MCP servers a project loads, and sets which are on.

- **One list for both agents.** Each extension is one row across Claude Code and Codex. One save writes its state into `.claude/settings.local.json` and `~/.claude.json` for Claude Code, and into `.codex/config.toml` for Codex. It replaces editing each agent's files by hand.
- **Per project, out of git.** equip keeps the project files it writes out of git through `.git/info/exclude`, and never writes one that git tracks. Your choices stay out of the team's commits.
- **Presets.** A preset is a named set of extensions, such as Ruby or Accounting. A project can have several active at once.
- **Token cost per session.** Each skill shows its estimated context cost, and each agent shows the Total for the project. A Codex Total over its listing budget is flagged.
- **macOS and Linux.** CI runs the tests on both. Windows is not tested.

Install it with Homebrew:

```bash
brew install svyatov/tap/equip
```

Or build it with Go 1.27.1 or later, the version `go.mod` declares:

```bash
go install github.com/svyatov/equip@latest
```

Prebuilt archives for macOS and Linux are also on each [release](https://github.com/svyatov/equip/releases). Check the install, then open equip in a project:

```bash
equip --version   # equip v0.1.1
cd your-project
equip             # opens the TUI on the project's extensions
```

Each row shows a state glyph (`●` on, `◉` manual-only, `○` off), the extension's name, and its estimated tokens, then a bar of their size (`▂▄▆█`). `?` marks an MCP server not measured yet. `by name` marks a skill that no agent that has it calls on its own: its `SKILL.md` sets `disable-model-invocation` for Claude Code, and its `agents/openai.yaml` sets `allow_implicit_invocation: false` for Codex. Only a call by its name loads it, so it costs no tokens, and its row is greyed out. The `By name` facet lists them. A manual-only skill's row is greyed out too, and reads `manual-only` when no agent lists it. Each skill of a plugin has its own row, with the plugin's name greyed after it. It follows its plugin, and `enter` on it moves to the plugin. The footer of the TUI lists the keys:

```text
j/k  move   h/l  pane   space  cycle state   m  measure   /  search   p  presets   s  save   ?  keys   q  quit
```

The keys follow vim: `h` and `l` move between the facets, the list, and the detail pane, `g` and `G` jump to the ends, and `ctrl+d` and `ctrl+u` move half a page. `?` lists every key.

The top line shows each agent's estimated tokens per session. On open, equip measures each MCP server you configured or installed with a plugin, and each project server you approved, by starting it in the background, and caches the result until its config changes. `+ 4 MCP unmeasured` says how many servers that are on the total still leaves out. equip does not start a server it would have to guess about: one built into the agent, or one whose command or `cwd` is a relative path. A server that refuses equip, for want of a login or because it runs only inside its agent, is not tried again until its config changes. The detail pane says why a server is not measured, and `m` tries one again. `skills over budget` means Codex's skill listing passes its budget, so Codex shortens or drops some skills.

Changes stay unsaved until you press `s`. `equip --help` prints the usage.

## Questions and bugs

Both go to [GitHub issues](https://github.com/svyatov/equip/issues). Report a security vulnerability privately, as [SECURITY.md](SECURITY.md) describes.

equip is actively maintained and pre-1.0. [CHANGELOG.md](CHANGELOG.md) states what a version bump may break.

## Documentation

- [`CHANGELOG.md`](CHANGELOG.md) says what changed in each release.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) says how to set up, test, and send a change.
- [`GLOSSARY.md`](GLOSSARY.md) defines the vocabulary.

## License

[MIT](LICENSE).

# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/2.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). equip is pre-1.0, so a MINOR bump may break you and a PATCH bump will not.

The public surface has three parts:

- the command line: `equip`, `--help`, and `--version`
- the files equip keeps: the Record of each Project under `$XDG_STATE_HOME/equip/`, and the presets under `$XDG_CONFIG_HOME/equip/presets/`
- the entries equip writes into the agents' files: `.claude/settings.local.json` in the Project and `~/.claude.json` for Claude Code, and `.codex/config.toml` in the Project for Codex

The MCP cost cache under `$XDG_CACHE_HOME/equip/` and the layout of the TUI are outside it. A flag, file format, or file location is removed only after at least one release lists it under Deprecated. In that release, equip prints a notice naming its replacement and the earliest version that removes it.

## [Unreleased]

## [0.1.3] - 2026-09-27

### Changed

- The agents' facets are `Claude Code` and `Codex`, and each counts every row its agent has, in any state. They were `Claude Code only` and `Codex only`, which left out the rows both agents have.

### Fixed

- A skill whose `SKILL.md` is a symlink is no longer listed for Codex, which skips it. Claude Code still loads it.

## [0.1.2] - 2026-09-27

### Added

- Each skill of a plugin has a row of its own in the list, named by the skill with its plugin greyed after it, and the `Skills` and `By name` facets count it. It follows its plugin: the state keys say so and change nothing, `enter` moves to the plugin, and a search for the plugin's name finds its skills.

### Fixed

- A legacy MCP server that quits on `server/discover` is started again for `initialize`, so it gets measured.
- A skill whose `agents/openai.yaml` sets `allow_implicit_invocation: false` costs nothing in Codex, which leaves it out of its listing, where equip counted its tokens.
- A skill set to `user-invocable-only` or `off` in the `skillOverrides` of `~/.claude/settings.json` or the project's `.claude/settings.json` takes that state by default, where it showed as on.

- The TUI fits the terminal. The key help stays on the bottom line, the list and the detail pane scroll to keep the highlight on screen, and a line says how many more rows are below. Below 100 columns the facet sidebar hides and `[ ]` still switches facets. `pgup`, `pgdn`, `home`, and `end` move by a page or to the ends.
- The top line wraps where it was cut, so a narrow terminal still shows each agent's total and the unsaved count, and a long project path is cut from its start.
- `s` says how many changes it saved.

### Changed

- equip measures MCP servers on open, in the background and four at a time, where it waited for `m` on each. It measures a server that is off too, as long as the user configured it or installed it with a plugin, or approved it in the project, so its cost is known before it is turned on. The top line counts the servers left, and the footer says how many could not be measured. equip skips a server it would have to guess about, one built into the agent or with a relative command or `cwd`, and does not try again a server that refused it, for want of a login or with an error of its own, until its config changes. The detail pane says why a server is not measured.
- The keys follow vim. `h`, `l`, the arrows, `tab`, and `enter` move between the facet sidebar, the list, and the detail pane, and the focused pane has a coloured border. `j` and `k` pick a facet in the sidebar, move in the list, and move among a plugin's MCP servers or scroll the detail pane. `g` and `G` jump to the ends, `ctrl+d` and `ctrl+u` move half a page, and `space` cycles the state. `?` lists every key. In the presets workspace, `h` and `l` move between the library and the members.
- The TUI uses the Catppuccin Mocha colours, with a colour per state, per kind, and per agent.
- The top line counts the MCP servers a total leaves out until they are measured, as in `+ 4 MCP unmeasured`, and says `skills over budget` where it said `over budget`. A cost not measured yet reads `?` in the list and `unmeasured` in the detail pane, where it read `unknown`. The project path writes the home directory as `~`.
- A skill that no agent that has it calls on its own reads `by name` in place of `~0`, greyed out, and the new `By name` facet lists these skills and the plugins made only of them. The detail pane says it costs none. Claude Code reads `disable-model-invocation` in its `SKILL.md`, and Codex reads `allow_implicit_invocation` in its `agents/openai.yaml`.
- A manual-only skill's row is greyed out too, and its cost reads `manual-only` when no agent lists it.
- The manual-only glyph is `◉` where it was `◐`, which the common coding fonts lack, so terminals drew it wider from another font.
- The TUI has a retro look: the logo in ANSI shades, pane titles set into the borders, key caps in the footer, and a bar of each cost's size. The list keeps the search in its border, so it shows one more row, and says how many rows are above as well as below. A plugin's `@marketplace` is dimmed and cut before its name. The sidebar counts the rows the search keeps in each facet, and a legend of the marks sits at its foot when there is room.
- The detail pane wraps long notes, writes paths with `~`, and lines up a plugin's contents in columns.
- The presets workspace says what a preset is when there are none, marks the active presets with `●`, lines up the costs of members and of the add list, and says `nothing matches` for an empty search. Its right pane no longer shows the state keys, which do nothing there.

## [0.1.1] - 2026-09-27

### Added

- Prebuilt binaries for macOS and Linux, on amd64 and arm64, attached to each release with an SPDX SBOM per archive and a build attestation. Verify one with `gh attestation verify <file> --repo svyatov/equip`.
- A Homebrew cask: `brew install svyatov/tap/equip`.

## [0.1.0] - 2026-09-27

### Added

- `equip` opens a TUI in the working directory's Project. It lists every skill, plugin, and MCP server that Claude Code and Codex would load there, one row per extension across both agents.
- Each extension can be set on, manual-only, or off, and `s` saves the states into the agents' files. A save keeps every key equip does not own and keeps `.claude/settings.local.json` out of git. One MCP server inside a plugin can be turned off while the plugin stays on.
- A state set by hand in an agent's file shows as an unsaved Override. A save that finds such a change since the last read writes nothing and imports the change first.
- Presets are created, renamed, edited, and deleted in a workspace opened with `p`, and one or more presets can be active in a Project. A preset changed outside equip shows its changes as unsaved states.
- Each skill shows its estimated context cost, and each agent shows the Total for the Project. A Codex Total over its listing budget is flagged. An MCP server's cost is measured on request by starting it.
- A facet sidebar narrows the list by kind, agent, state, or change, and `/` searches by name.
- equip keeps a Record per Project and machine. On a moved repository's first open, equip offers to adopt the Record left at the old path.
- `--help` prints the usage and `--version` prints the build version.

[unreleased]: https://github.com/svyatov/equip/compare/v0.1.3...HEAD
[0.1.3]: https://github.com/svyatov/equip/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/svyatov/equip/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/svyatov/equip/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/svyatov/equip/releases/tag/v0.1.0

# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/2.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). equip is pre-1.0, so a MINOR bump may break you and a PATCH bump will not.

The public surface has three parts:

- the command line: `equip`, `--help`, and `--version`
- the files equip keeps: the Record of each Project under `$XDG_STATE_HOME/equip/`, and the presets under `$XDG_CONFIG_HOME/equip/presets/`
- the entries equip writes into the agents' files: `.claude/settings.local.json` in the Project and `~/.claude.json` for Claude Code, and `.codex/config.toml` in the Project for Codex

The MCP cost cache under `$XDG_CACHE_HOME/equip/` and the layout of the TUI are outside it. A flag, file format, or file location is removed only after at least one release lists it under Deprecated. In that release, equip prints a notice naming its replacement and the earliest version that removes it.

## [Unreleased]

### Fixed

- The TUI fits the terminal. The key help stays on the bottom line, the list and the detail pane scroll to keep the highlight on screen, and a line says how many more rows are below. Below 100 columns the facet sidebar hides and `[ ]` still switches facets. `pgup`, `pgdn`, `home`, and `end` move by a page or to the ends.

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

[unreleased]: https://github.com/svyatov/equip/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/svyatov/equip/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/svyatov/equip/releases/tag/v0.1.0

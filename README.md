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

Each row shows a state glyph (`●` on, `◐` manual-only, `○` off), the extension's name, and its estimated tokens. The footer of the TUI lists the keys:

```text
↑↓ move  [ ] facet  / search  1-3 set state  x drop override  m measure  tab MCP servers  p presets  s save  q quit
```

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

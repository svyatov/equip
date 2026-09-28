# QA through tmux

Use this to test equip end to end as a user would. Run the real binary in a detached tmux session, drive it with keys, and read the screen. Check each result against what the agents load. Done when every feature you set out to test either works or has an issue filed.

## 1. Set up

Work in a fresh `mktemp -d` dir, `$T`. Keep every helper there as a Ruby script.

1. Build equip: `go build -o $T/equip .` from the repo.
2. Make the Project: `git init $T/proj` with one commit. Add fixtures under `.claude/skills/`, `.agents/skills/`, and a `.mcp.json`. For an MCP server, write a stdio server in stdlib Ruby. An `npx` server pulls in a package nobody vetted.
3. Back up `~/.claude.json` and `~/.codex/config.toml`. The agents and equip write to them, and step 5 undoes those writes.

equip's own files follow the XDG vars, so QA never touches the user's presets, Records, or MCP cache. The agents' config has no such switch. For a test that needs a clean home, such as running in the home directory, set `HOME` to a fake dir in `$T`.

## 2. Drive equip

Start equip in a detached session, with the XDG vars pointing into `$T`:

```sh
tmux new-session -d -s eq -x 200 -y 50 -c $T/proj \
  "env XDG_CONFIG_HOME=$T/xdg/config XDG_STATE_HOME=$T/xdg/state XDG_CACHE_HOME=$T/xdg/cache $T/equip; echo EXITED=\$?; sleep 3600"
```

The trailing `echo EXITED=$?; sleep 3600` keeps the pane alive after equip quits, so a crash or an exit code stays readable.

- Send a key with `tmux send-keys -t eq j`. Type text with `tmux send-keys -t eq -l 'text'`.
- Read the screen with `tmux capture-pane -t eq -p`. The first line holds the totals and the unsaved count. The last line holds the flash or a prompt. `▸` marks the highlighted row.
- Test a layout with `-x` and `-y` on a new session, for example 90x20 and 60x20. 60 is the narrowest width equip draws (`minWidth` in `tui.go`); below it, equip shows "terminal too small".

Keys get lost or merged when sent too fast:

- Pause about 0.35 s between keys, and 0.5 s after `Escape`. A key right after `Escape` reads as an alt combo.
- Capture the screen after each action, and confirm the change before the next one. A lost key can shift a later burst so that it types `s` and saves stray states.
- Wait 1 to 1.5 s after `s` before reading the flash and the files.

A Ruby helper that sends each key with a pause and then prints the screen saves most of the typing.

## 3. Check against the agents

After each save, compare equip with what each agent loads, as `docs/agents/checking-counts.md` describes. Also read the files equip wrote:

- Claude Code: `.claude/settings.local.json`, and the Project's entry in `~/.claude.json`.
- Codex: `.codex/config.toml`.
- equip: `$T/xdg/state/equip/*.toml` for the Records, and `$T/xdg/config/equip/presets/*.toml` for the Presets.

Two agent gates change what a check can show:

- Codex ignores a Project's `.codex/config.toml` until the user trusts the Project. The user must run `codex` in the Project and accept, so ask them. Note that macOS resolves `/tmp` to `/private/tmp` in the trust key.
- Claude Code starts a `.mcp.json` server only in a Project whose `hasTrustDialogAccepted` is true in `~/.claude.json`.

A useful sequence: take a baseline, turn every row off, turn some on, activate a Preset, then repeat in a linked worktree, a moved repo, a subdir, and a dir outside git.

## 4. File what you find

File one issue per finding, following `docs/agents/issue-tracker.md`, labelled `needs-triage` plus `bug` or `enhancement`. Give the steps to reproduce, the expected result, and the commit you tested. Check `docs/agents/checking-counts.md`'s expected differences first.

## 5. Clean up

1. `tmux kill-session -t eq`.
2. Remove the entries the run added to `~/.claude.json` (one per QA Project path) and the Codex trust entry in `~/.codex/config.toml`. Leave every other key as it was, and check that both files still parse.
3. Delete `$T`.

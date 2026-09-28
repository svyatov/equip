## Checks

`mise run lint` and `mise run cover` run what CI runs; both pass before a commit. `mise tasks` lists the rest. Install the tools and the pre-commit hook once with `mise install && mise x -- lefthook install`.

## Counts

When equip's list or a facet count disagrees with what Claude Code or Codex loads, follow `docs/agents/checking-counts.md`.

## QA

To test equip end to end in a terminal, driving the TUI through tmux, follow `docs/agents/qa.md`.

## Agent skills

### Issue tracker

Issues live in GitHub Issues on `svyatov/equip`, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The default vocabulary: `bug`, `enhancement`, `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` at the repo root, and ADRs in `docs/adr/` once the first one is written. See `docs/agents/domain.md`.

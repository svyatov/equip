## Checks

`task lint` and `task cover` run what CI runs; both pass before a commit. `task` alone lists the rest. Install the pre-commit hook once with `go tool lefthook install`.

## Agent skills

### Issue tracker

Issues live in GitHub Issues on `svyatov/equip`, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The default vocabulary: `bug`, `enhancement`, `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

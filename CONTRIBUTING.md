# Contributing to equip

Contributions are welcome. This file says what to install, what to run before you push, and what a change has to satisfy to be merged.

## What a change has to satisfy

Two documents decide whether a change is acceptable:

- [`GLOSSARY.md`](GLOSSARY.md) defines the vocabulary. Use its words for types, identifiers, and UI text. Each entry lists the synonyms to avoid.
- [`AGENTS.md`](AGENTS.md) names the checks a change passes before a commit: `mise run lint` and `mise run cover`.

A change that introduces or renames a domain term updates `GLOSSARY.md` in the same pull request.

## Setup

You need [Go](https://go.dev/dl/) at the version in `go.mod`, currently 1.27.1, and [mise](https://mise.jdx.dev/getting-started.html). Then:

```bash
git clone https://github.com/svyatov/equip.git
cd equip
mise trust && mise install
mise run build
```

`mise install` fetches golangci-lint, govulncheck, and lefthook at the versions `mise.toml` pins.

To run the checks on every commit, install the pre-commit hook once:

```bash
mise x -- lefthook install
```

The hook formats staged Go files, then runs `mise run lint` and `mise run test`.

## Before you push

```bash
mise run test    # go test -race -shuffle=on ./...
mise run cover   # the same run with coverage, fails under 88%
mise run lint    # go.mod tidy and verified, go fix -diff, golangci-lint, the formatter, and the dash check
mise run vuln    # govulncheck over the code paths equip calls
```

CI runs `build`, `cover`, `lint`, and `vuln` through these same tasks, so a local pass means what a green check means.

## Tests

A change that adds functionality arrives with a test. Put it in a `_test.go` file in the package it covers. A bug fix arrives with a test that fails without the fix.

## Opening a pull request

Fork the repository, branch from `main`, and open the pull request against `main`. Name the branch after the commit type, as in `fix/codex-plugin-state`.

The pull request title follows [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/): `type(scope): description`. Pull requests are squash-merged, so the title becomes the commit on `main`. The branch is deleted on merge.

The pull request template asks for what CI cannot report. Delete any heading you have nothing to put under.

## Reporting bugs and asking questions

Both go to [GitHub issues](https://github.com/svyatov/equip/issues).

Security vulnerabilities do not go to the issue tracker. See [SECURITY.md](SECURITY.md).

## Who decides

Leonid Svyatov ([@svyatov](https://github.com/svyatov)) is the sole maintainer. He reviews and merges every change and cuts every release. There is no succession arranged: if he stops, nobody else has commit or release access, and the project would need a fork to continue.

#!/bin/sh
# Print one version's CHANGELOG.md section, for `goreleaser --release-notes`,
# so the changelog entry is the release body rather than a commit list.
#
# With no argument it reads the newest released version heading, which is how
# `mise run release-check` fails a release whose section is empty before the tag
# rather than after it.
#
# Run from the repo root, by `mise run release-check` and by
# .github/workflows/release.yml with the tag's version.

set -eu

version=${1:-$(sed -n 's/^## \[\([0-9][^]]*\)\].*/\1/p' CHANGELOG.md | head -1)}
[ -n "$version" ] || {
	echo "CHANGELOG.md has no released version heading" >&2
	exit 1
}

notes=$(awk -v heading="## [$version]" '
	index($0, heading) == 1 { found = 1; next }
	# The link definitions at the end belong to no section, so the oldest
	# release stops there as every other one stops at the next heading.
	found && (/^## / || /^\[[^]]+\]: /) { exit }
	found { print }
' CHANGELOG.md)

printf '%s' "$notes" | grep -q '[^[:space:]]' || {
	echo "CHANGELOG.md has no '## [$version]' section, or the section is empty" >&2
	exit 1
}

printf '%s\n' "$notes"

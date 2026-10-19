#!/bin/sh
# Installs the repository's git hooks into .git/hooks. Run once per clone:
#
#   scripts/install-git-hooks.sh
#
# The hooks encode the two standing release rules:
#
#   - a commit changing more than 1000 lines is pushed to origin
#   - a commit changing more than 3000 lines is released — mygo.json's
#     version is bumped, an annotated vX.Y.Z tag is made, and both are
#     pushed, which triggers .github/workflows/release.yml
#
# They run on `git commit` in this checkout only; GitHub's own protections
# and the release workflow are unaffected. Remove with:
#
#   rm .git/hooks/post-commit

set -eu

top=$(git rev-parse --show-toplevel 2>/dev/null) || {
	printf 'not inside a git repository\n' >&2
	exit 1
}
cd "$top" || exit 1

hooks=$(git rev-parse --git-path hooks)
mkdir -p "$hooks"
cp scripts/git-hooks/post-commit "$hooks/post-commit"
chmod +x "$hooks/post-commit"
printf 'installed %s/post-commit\n' "$hooks"

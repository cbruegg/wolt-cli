#!/usr/bin/env bash
# install-git-hooks.sh -- point this clone's git at the tracked hooks dir.
#
# Run once after cloning:
#   ./scripts/install-git-hooks.sh
#
# Uninstall:
#   git config --unset core.hooksPath
#
# The hooks at scripts/git-hooks/ mirror the remote CI gates so a green
# local "git push" matches a green pipeline. See AGENTS.MD for the
# policy.

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "${repo_root}"

hooks_dir="scripts/git-hooks"
if [ ! -d "${hooks_dir}" ]; then
  echo "install-git-hooks: ${hooks_dir} not found" >&2
  exit 1
fi

# Make sure the hooks themselves are executable. Git ignores non-exec
# scripts silently.
chmod +x "${hooks_dir}"/* 2>/dev/null || true

current="$(git config --get core.hooksPath 2>/dev/null || true)"
if [ "${current}" = "${hooks_dir}" ]; then
  echo "git-hooks: already installed (core.hooksPath = ${hooks_dir})"
  exit 0
fi

git config core.hooksPath "${hooks_dir}"
echo "git-hooks: installed (core.hooksPath = ${hooks_dir})"
echo ""
echo "Hooks now active:"
for h in "${hooks_dir}"/*; do
  [ -x "$h" ] || continue
  echo "  $(basename "$h")"
done
echo ""
echo "Bypass for emergencies:"
echo "  git commit --no-verify"
echo "  git push --no-verify"

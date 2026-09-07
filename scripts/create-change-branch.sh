#!/usr/bin/env bash
set -euo pipefail

usage() {
	echo "usage: scripts/create-change-branch.sh <spec-or-change-path>" >&2
}

fail() {
	echo "create-change-branch: $*" >&2
	exit 1
}

ensure_clean_worktree() {
	local changes
	changes=$(git status --porcelain=v1 --untracked-files=all -- .)
	[[ -z "$changes" ]] || fail "working tree must be clean"
}

if [[ $# -ne 1 ]]; then
	usage
	exit 2
fi

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(git -C "$script_dir" rev-parse --show-toplevel 2>/dev/null) || \
	fail "scripts directory is not inside a git repository"
cd "$repo_root"

entry_path=$1
[[ -f "$entry_path" ]] || fail "specification or change file not found: $entry_path"
if [[ "$entry_path" =~ ^agent/(specs|changes)/([^/]+)\.md$ ]]; then
	change_slug=${BASH_REMATCH[2]}
else
	fail "path must match agent/{specs,changes}/<change-slug>.md"
fi

branch="change/$change_slug"
git check-ref-format --branch "$branch" >/dev/null 2>&1 || \
	fail "invalid change branch derived from path: $branch"

ensure_clean_worktree
git checkout master 1>&2
git fetch 1>&2
ensure_clean_worktree

local_exists=0
if git show-ref --verify --quiet "refs/heads/$branch"; then
	local_exists=1
else
	status=$?
	[[ $status -eq 1 ]] || fail "cannot inspect branch $branch"
fi

remote_exists=0
if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
	remote_exists=1
else
	status=$?
	[[ $status -eq 1 ]] || fail "cannot inspect origin/$branch"
fi

if [[ $local_exists -eq 1 ]]; then
	git checkout "$branch" 1>&2
elif [[ $remote_exists -eq 1 ]]; then
	git checkout --track -b "$branch" "origin/$branch" 1>&2
else
	git checkout -b "$branch" 1>&2
fi
ensure_clean_worktree
printf '%s\n' "$branch"

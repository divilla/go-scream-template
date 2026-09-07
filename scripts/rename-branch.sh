#!/usr/bin/env bash
set -euo pipefail

usage() {
	echo "usage: scripts/rename-branch.sh <new-branch-name>" >&2
}

fail() {
	echo "rename-branch: $*" >&2
	exit 1
}

if [[ $# -ne 1 ]]; then
	usage
	exit 2
fi

new_branch=$1

git rev-parse --git-dir >/dev/null 2>&1 || fail "not inside a git repository"
git check-ref-format --branch "$new_branch" >/dev/null || fail "invalid branch name: $new_branch"

old_branch=$(git branch --show-current)
[[ -n "$old_branch" ]] || fail "cannot rename from detached HEAD"
[[ "$old_branch" != "$new_branch" ]] || fail "current branch is already named $new_branch"

if git show-ref --verify --quiet "refs/heads/$new_branch"; then
	fail "local branch already exists: $new_branch"
fi

remote=$(git config --get "branch.$old_branch.remote" || true)
remote_branch=$(git config --get "branch.$old_branch.merge" || true)

if [[ -n "$remote_branch" ]]; then
	remote_branch=${remote_branch#refs/heads/}
else
	remote_branch=$old_branch
fi

if [[ -z "$remote" ]]; then
	if git remote get-url origin >/dev/null 2>&1; then
		remote=origin
	else
		fail "current branch has no upstream remote and origin does not exist"
	fi
fi

remote_ref="refs/heads/$remote_branch"
if ! remote_head=$(git ls-remote --exit-code --heads "$remote" "$remote_ref"); then
	fail "remote branch does not exist: $remote/$remote_branch"
fi
remote_tip=${remote_head%%$'\t'*}
if [[ -z "$remote_tip" || "$remote_head" != "$remote_tip"$'\t'"$remote_ref" ]]; then
	fail "cannot verify remote branch tip: $remote/$remote_branch"
fi

local_tip=$(git rev-parse HEAD)
if [[ "$local_tip" != "$remote_tip" ]]; then
	fail "local branch tip $local_tip does not match remote branch tip $remote_tip"
fi

if git ls-remote --exit-code --heads "$remote" "refs/heads/$new_branch" >/dev/null; then
	fail "remote branch already exists: $remote/$new_branch"
fi

echo "Renaming $old_branch to $new_branch on $remote."

git push \
	--atomic \
	"--force-with-lease=$remote_ref:$remote_tip" \
	"--force-with-lease=refs/heads/$new_branch:" \
	"$remote" \
	"HEAD:refs/heads/$new_branch" \
	":$remote_ref"
git branch -m "$new_branch"
git branch --set-upstream-to="$remote/$new_branch" "$new_branch"

echo "Renamed local branch $old_branch to $new_branch."
echo "Renamed remote branch $remote/$remote_branch to $remote/$new_branch."

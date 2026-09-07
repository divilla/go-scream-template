#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
test_root=$(mktemp -d)
repo="$test_root/repo with spaces"
remote="$test_root/origin.git"
mkdir -p "$repo/scripts" "$repo/agent/specs" "$repo/agent/changes" "$repo/other"
trap 'rm -rf -- "$test_root"' EXIT

cp "$script_dir/create-change-branch.sh" "$repo/scripts/create-change-branch.sh"
printf '%s\n' '# Domain types' >"$repo/agent/specs/000-domain-types.md"
printf '%s\n' '# Remote branch' >"$repo/agent/specs/001-remote-branch.md"
printf '%s\n' '# Enhanced debug' >"$repo/agent/changes/014-enhanced-debug.md"
printf '%s\n' '# Invalid path' >"$repo/other/plain.md"

git init --bare -q "$remote"
git -C "$repo" init -q -b master
git -C "$repo" config user.name 'Create Change Branch Test'
git -C "$repo" config user.email 'create-change-branch@example.invalid'
git -C "$repo" remote add origin "$remote"
git -C "$repo" add .
git -C "$repo" commit -q -m initial
git -C "$repo" push -q -u origin master
git -C "$remote" symbolic-ref HEAD refs/heads/master
git -C "$repo" remote set-head origin --auto >/dev/null
git -C "$repo" checkout -q -b source-branch
printf '%s\n' 'source branch only' >"$repo/source-branch-only.txt"
git -C "$repo" add source-branch-only.txt
git -C "$repo" commit -q -m 'source branch commit'

usage_error="$test_root/usage-error"
set +e
"$repo/scripts/create-change-branch.sh" > /dev/null 2>"$usage_error"
usage_status=$?
set -e
[[ $usage_status -eq 2 ]]
grep -Fxq 'usage: scripts/create-change-branch.sh <spec-or-change-path>' "$usage_error"

printf '%s\n' uncommitted >"$repo/uncommitted.txt"
dirty_error="$test_root/dirty-error"
set +e
"$repo/scripts/create-change-branch.sh" agent/specs/000-domain-types.md > /dev/null 2>"$dirty_error"
dirty_status=$?
set -e
[[ $dirty_status -eq 1 ]]
[[ $(git -C "$repo" branch --show-current) == source-branch ]]
[[ -z $(git -C "$repo" branch --list change/000-domain-types) ]]
grep -Fxq 'create-change-branch: working tree must be clean' "$dirty_error"
rm "$repo/uncommitted.txt"

branch=$(cd "$test_root" && "$repo/scripts/create-change-branch.sh" agent/specs/000-domain-types.md)
[[ "$branch" == change/000-domain-types ]]
[[ $(git -C "$repo" branch --show-current) == change/000-domain-types ]]
[[ $(git -C "$repo" rev-parse HEAD) == $(git -C "$repo" rev-parse master) ]]
[[ $(git -C "$repo" rev-parse HEAD) != $(git -C "$repo" rev-parse source-branch) ]]
! git -C "$repo" cat-file -e HEAD:source-branch-only.txt 2>/dev/null
printf '%s\n' 'local branch only' >"$repo/local-branch-only.txt"
git -C "$repo" add local-branch-only.txt
git -C "$repo" commit -q -m 'local branch commit'
local_branch_head=$(git -C "$repo" rev-parse HEAD)

git -C "$repo" checkout -q master
change_branch=$(cd "$test_root" && "$repo/scripts/create-change-branch.sh" agent/changes/014-enhanced-debug.md)
[[ "$change_branch" == change/014-enhanced-debug ]]
[[ $(git -C "$repo" branch --show-current) == change/014-enhanced-debug ]]
[[ $(git -C "$repo" rev-parse HEAD) == $(git -C "$repo" rev-parse master) ]]

existing_local_branch=$("$repo/scripts/create-change-branch.sh" agent/specs/000-domain-types.md)
[[ "$existing_local_branch" == change/000-domain-types ]]
[[ $(git -C "$repo" branch --show-current) == change/000-domain-types ]]
[[ $(git -C "$repo" rev-parse HEAD) == "$local_branch_head" ]]
[[ -f "$repo/local-branch-only.txt" ]]

git -C "$repo" checkout -q master
git -C "$repo" checkout -q -b change/001-remote-branch
printf '%s\n' 'remote branch only' >"$repo/remote-branch-only.txt"
git -C "$repo" add remote-branch-only.txt
git -C "$repo" commit -q -m 'remote branch commit'
remote_branch_head=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" push -q -u origin change/001-remote-branch
git -C "$repo" checkout -q master
git -C "$repo" branch -D change/001-remote-branch >/dev/null
git -C "$repo" branch -dr origin/change/001-remote-branch >/dev/null
existing_remote_branch=$("$repo/scripts/create-change-branch.sh" agent/specs/001-remote-branch.md)
[[ "$existing_remote_branch" == change/001-remote-branch ]]
[[ $(git -C "$repo" branch --show-current) == change/001-remote-branch ]]
[[ $(git -C "$repo" rev-parse HEAD) == "$remote_branch_head" ]]
[[ $(git -C "$repo" rev-parse --abbrev-ref --symbolic-full-name '@{upstream}') == \
	origin/change/001-remote-branch ]]
[[ -f "$repo/remote-branch-only.txt" ]]

invalid_error="$test_root/invalid-error"
set +e
"$repo/scripts/create-change-branch.sh" other/plain.md > /dev/null 2>"$invalid_error"
invalid_status=$?
set -e
[[ $invalid_status -eq 1 ]]
grep -Fxq 'create-change-branch: path must match agent/{specs,changes}/<change-slug>.md' "$invalid_error"

printf '%s\n' 'create-change-branch tests passed'

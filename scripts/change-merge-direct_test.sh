#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT

create_repository() {
	local name=$1
	local branch=$2
	local repo="$test_root/$name/repo"
	local remote="$test_root/$name/origin.git"

	mkdir -p "$repo/scripts"
	cp "$script_dir/change-merge-direct.pl" "$repo/scripts/change-merge-direct.pl"
	git init --bare -q "$remote"
	git -C "$repo" init -q -b master
	git -C "$repo" config user.name 'Change Merge Direct Test'
	git -C "$repo" config user.email 'change-merge-direct@example.invalid'
	git -C "$repo" remote add origin "$remote"
	printf '%s\n' initial >"$repo/content.txt"
	git -C "$repo" add .
	git -C "$repo" commit -q -m initial
	git -C "$repo" push -q -u origin master
	git -C "$repo" checkout -q -b "$branch"
	printf '%s\n' "$branch" >"$repo/content.txt"
	git -C "$repo" add content.txt
	git -C "$repo" commit -q -m 'work in progress'
	git -C "$repo" push -q -u origin "$branch"
}

assert_merged() {
	local name=$1
	local branch=$2
	local expected_message=$3
	local repo="$test_root/$name/repo"
	local remote="$test_root/$name/origin.git"

	[[ $(git -C "$repo" branch --show-current) == master ]]
	[[ $(git -C "$repo" log -1 --format=%s) == "$expected_message" ]]
	[[ $(git -C "$repo" rev-parse HEAD) == $(git -C "$repo" rev-parse origin/master) ]]
	[[ $(git -C "$repo" rev-list --count HEAD) -eq 2 ]]
	[[ $(<"$repo/content.txt") == "$branch" ]]
	! git ls-remote --exit-code --heads "$remote" "$branch" >/dev/null
}

create_repository default-change change/013-default-message
(
	cd "$test_root/default-change/repo"
	scripts/change-merge-direct.pl >/dev/null
)
assert_merged default-change change/013-default-message 'Implement change 013-default-message'

for branch_case in custom-change custom-fix custom-test custom-other; do
	case $branch_case in
		custom-change) branch=change/014-custom-message ;;
		custom-fix) branch=fix/request-validation ;;
		custom-test) branch=test/request-validation ;;
		custom-other) branch=release/candidate ;;
	esac
	message="Merge $branch"
	create_repository "$branch_case" "$branch"
	(
		cd "$test_root/$branch_case/repo"
		scripts/change-merge-direct.pl "$message" >/dev/null
	)
	assert_merged "$branch_case" "$branch" "$message"
done

create_repository already-squashed fix/already-squashed
git -C "$test_root/already-squashed/repo" commit -q --amend -m 'Keep existing squash'
git -C "$test_root/already-squashed/repo" push -q --force origin fix/already-squashed
existing_squash=$(git -C "$test_root/already-squashed/repo" rev-parse HEAD)
(
	cd "$test_root/already-squashed/repo"
	scripts/change-merge-direct.pl 'Keep existing squash' >/dev/null
)
assert_merged already-squashed fix/already-squashed 'Keep existing squash'
[[ $(git -C "$test_root/already-squashed/repo" rev-parse HEAD) == "$existing_squash" ]]

create_repository restricted-no-message fix/requires-message
restricted_error="$test_root/restricted-error"
set +e
(
	cd "$test_root/restricted-no-message/repo"
	scripts/change-merge-direct.pl >/dev/null 2>"$restricted_error"
)
restricted_status=$?
set -e
[[ $restricted_status -ne 0 ]]
grep -Fxq 'current branch is not a change/<change-slug> branch: fix/requires-message' "$restricted_error"

argument_error="$test_root/argument-error"
set +e
"$script_dir/change-merge-direct.pl" one two >/dev/null 2>"$argument_error"
argument_status=$?
set -e
[[ $argument_status -ne 0 ]]
grep -Fxq 'usage: scripts/change-merge-direct.pl ["commit message"]' "$argument_error"

empty_error="$test_root/empty-error"
set +e
"$script_dir/change-merge-direct.pl" '' >/dev/null 2>"$empty_error"
empty_status=$?
set -e
[[ $empty_status -ne 0 ]]
grep -Fxq 'commit message cannot be empty' "$empty_error"

master_error="$test_root/master-error"
set +e
(
	cd "$test_root/default-change/repo"
	scripts/change-merge-direct.pl 'Do not merge master' >/dev/null 2>"$master_error"
)
master_status=$?
set -e
[[ $master_status -ne 0 ]]
grep -Fxq 'cannot merge master into itself' "$master_error"

printf '%s\n' 'change-merge-direct tests passed'

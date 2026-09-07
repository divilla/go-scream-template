#!/usr/bin/env bash
set -euo pipefail
export COLUMNS=80

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
test_root=$(mktemp -d)
repo="$test_root/repo"
remote="$test_root/origin.git"
fake_bin="$test_root/bin"
implementation_count="$test_root/implementation-count"
mkdir -p "$repo/scripts/lib/APIHydra" "$repo/agent/specs" "$repo/agent/changes" "$repo/other" "$fake_bin"

cleanup() {
	rm -rf -- "$test_root"
}
trap cleanup EXIT

cp "$script_dir/codex-code-spec.pl" "$repo/scripts/codex-code-spec.pl"
cp "$script_dir/lib/APIHydra/Progress.pm" "$repo/scripts/lib/APIHydra/Progress.pm"
printf '%s\n' '# Domain types' >"$repo/agent/specs/000-domain-types.md"
printf '%s\n' '# Failing change' >"$repo/agent/specs/001-failing-change.md"
printf '%s\n' '# Enhanced debug' >"$repo/agent/changes/014-enhanced-debug.md"
printf '%s\n' '# Outside specification directory' >"$repo/other/plain.md"
printf '%s\n' 'old production one' 'old production two' >"$repo/production.go"
printf '%s\n' 'old test' >"$repo/production_test.go"

cat >"$fake_bin/codex" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[[ ${1-} == exec ]]
[[ ${2-} == --json ]]
[[ ${3-} == -o ]]
result=${4-}
prompt=${5-}
[[ $# -eq 5 ]]

printf '%s\n' '{"type":"started"}'
if [[ ${CODEX_TEST_FAIL-} == 1 ]]; then
	printf '%s\n' 'simulated implementation failure' >&2
	exit 42
fi

[[ "$prompt" == "\$change-code $CODEX_TEST_EXPECTED_SPECIFICATION" ]]
count=0
[[ ! -f "$CODEX_TEST_IMPLEMENTATION_COUNT" ]] || count=$(<"$CODEX_TEST_IMPLEMENTATION_COUNT")
((count += 1))
printf '%s\n' "$count" >"$CODEX_TEST_IMPLEMENTATION_COUNT"
sleep 1.1
printf '%s\n' '{"type":"completed"}'
printf '%s\n' 'Implementation complete.' >"$result"
printf '%s\n' "$count" >"implemented-$count.txt"
printf '%s\n' 'new production' >production.go
printf '%s\n' 'new test one' 'new test two' >production_test.go
EOF
chmod +x "$fake_bin/codex"

git init --bare -q "$remote"
git -C "$repo" init -q -b master
git -C "$repo" config user.name 'Implement Test'
git -C "$repo" config user.email 'implement@example.invalid'
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

run_implementation() {
	local output=$1
	local specification=${2:-agent/specs/000-domain-types.md}
	(
		cd "$repo"
		PATH="$fake_bin:$PATH" \
			CODEX_TEST_EXPECTED_SPECIFICATION="$specification" \
			CODEX_TEST_IMPLEMENTATION_COUNT="$implementation_count" \
			./scripts/codex-code-spec.pl "$specification"
	) >"$output"
}

printf '%s\n' 'uncommitted' >"$repo/uncommitted.txt"
dirty_error="$test_root/dirty-error"
set +e
run_implementation /dev/null 2>"$dirty_error"
dirty_status=$?
set -e
[[ $dirty_status -eq 1 ]]
[[ $(git -C "$repo" branch --show-current) == source-branch ]]
[[ -z $(git -C "$repo" branch --list change/000-domain-types) ]]
[[ ! -f "$implementation_count" ]]
grep -Fxq 'codex-code-spec: working tree must be clean' "$dirty_error"
rm "$repo/uncommitted.txt"

wrong_branch_error="$test_root/wrong-branch-error"
set +e
run_implementation /dev/null 2>"$wrong_branch_error"
wrong_branch_status=$?
set -e
[[ $wrong_branch_status -eq 1 ]]
[[ $(git -C "$repo" branch --show-current) == source-branch ]]
[[ -z $(git -C "$repo" branch --list change/000-domain-types) ]]
[[ ! -f "$implementation_count" ]]
grep -Fxq 'codex-code-spec: current branch must be change/000-domain-types' "$wrong_branch_error"

git -C "$repo" checkout -q master
git -C "$repo" checkout -q -b change/000-domain-types
first_output="$test_root/first-output"
run_implementation "$first_output"

[[ $(git -C "$repo" branch --show-current) == 'change/000-domain-types' ]]
[[ $(git -C "$repo" log -1 --format=%s) == 'Implement change 000-domain-types' ]]
[[ $(git -C "$repo" rev-parse HEAD^) == $(git -C "$repo" rev-parse master) ]]
[[ $(git -C "$repo" rev-parse HEAD^) != $(git -C "$repo" rev-parse source-branch) ]]
! git -C "$repo" cat-file -e HEAD:source-branch-only.txt 2>/dev/null
[[ $(git -C "$repo" rev-parse change/000-domain-types) == \
	$(git -C "$repo" rev-parse origin/change/000-domain-types) ]]
[[ $(<"$implementation_count") == 1 ]]
grep -Fxq 'Branch: change/000-domain-types' "$first_output"
awk -v repo="$repo" '
$0 == "Repository: " repo {
	if ((getline line) <= 0 || line != "Specification: agent/specs/000-domain-types.md") exit 1
	if ((getline line) <= 0 || line != "Branch: change/000-domain-types") exit 1
	if ((getline line) <= 0 || line != "") exit 1
	if ((getline line) <= 0 || line != "=== Implementation ===") exit 1
	found = 1
}
END { if (!found) exit 1 }
' "$first_output"
grep -Eq '^codex exec --json -o /.+/implementation-result.md ' "$first_output"
grep -Fq "'\$change-code agent/specs/000-domain-types.md'" "$first_output"
awk '
/^-+$/ {
	separator = $0
	if ((getline command) <= 0 || command !~ /^codex /) exit 1
	if ((getline closing) <= 0 || closing != separator) exit 1
	if (length(separator) != length(command)) exit 1
	commands++
}
END { if (commands != 1) exit 1 }
' "$first_output"
grep -Eq '^\[✅\] 00:0[1-9] ••$' "$first_output"
awk '
/^\[✅\]/ { finished = 1; next }
finished && !before_result && $0 == "" { before_result = 1; next }
$0 == "Implementation complete." && before_result { result = 1; next }
result && $0 == "" { after_result = 1; next }
$0 == "Changed files:" && after_result { found = 1 }
END { if (!found) exit 1 }
' "$first_output"
grep -Fxq 'Implementation complete.' "$first_output"
! grep -Eq '^Implement [0-9]' "$first_output"
grep -Fxq 'Commit: Implement change 000-domain-types' "$first_output"
[[ -z $(git -C "$repo" status --short) ]]

git -C "$repo" checkout -q master
git -C "$repo" checkout -q -b change/014-enhanced-debug
change_output="$test_root/change-output"
run_implementation "$change_output" agent/changes/014-enhanced-debug.md
[[ $(git -C "$repo" log -1 --format=%s) == 'Implement change 014-enhanced-debug' ]]
[[ $(<"$implementation_count") == 2 ]]
grep -Fq "'\$change-code agent/changes/014-enhanced-debug.md'" "$change_output"

git -C "$repo" checkout -q master
git -C "$repo" checkout -q -b change/001-failing-change
failed_output="$test_root/failed-output"
failed_error="$test_root/failed-error"
set +e
(
	cd "$repo"
	PATH="$fake_bin:$PATH" \
		CODEX_TEST_FAIL=1 \
		./scripts/codex-code-spec.pl agent/specs/001-failing-change.md
) >"$failed_output" 2>"$failed_error"
failed_status=$?
set -e
[[ $failed_status -eq 42 ]]
[[ $(git -C "$repo" branch --show-current) == 'change/001-failing-change' ]]
[[ $(git -C "$repo" rev-parse HEAD) == $(git -C "$repo" rev-parse master) ]]
grep -Eq '^\[❌\] 00:00 •$' "$failed_output"
grep -Fxq 'codex-code-spec: Codex command failed with exit code 42' "$failed_error"
grep -Fq 'simulated implementation failure' "$failed_error"

git -C "$repo" checkout -q master
invalid_error="$test_root/invalid-error"
set +e
(
	cd "$repo"
	PATH="$fake_bin:$PATH" ./scripts/codex-code-spec.pl other/plain.md
) >/dev/null 2>"$invalid_error"
invalid_status=$?
set -e
[[ $invalid_status -eq 1 ]]
grep -Fxq 'codex-code-spec: specification path must match agent/{specs,changes}/<change-slug>.md' "$invalid_error"

printf '%s\n' 'codex-code-spec tests passed'

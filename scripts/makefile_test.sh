#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT

repo="$test_root/skeleton/repo with spaces"
fake_bin="$test_root/bin"
log="$test_root/tool.log"
mkdir -p "$repo/pkg/runner" "$repo/scripts" "$repo/agent/specs" "$repo/agent/changes" "$fake_bin"
cp "$script_dir/../Makefile" "$repo/Makefile"
printf '%s\n' '# Domain types' >"$repo/agent/specs/000-domain-types.md"
printf '%s\n' '# Enhanced debug' >"$repo/agent/changes/014-enhanced-debug.md"

cat >"$fake_bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ ${1-} == vet || ${1-} == test ]]; then
	printf 'go:%s\n' "$*" >>"$MAKEFILE_TEST_LOG"
	exit 0
fi

[[ ${1-} == list ]]
if [[ $# -eq 2 && $2 == github.com/divilla/apihydra/... ]]; then
	if [[ ${MAKEFILE_TEST_EMPTY-} == 1 ]]; then
		printf '%s\n' github.com/divilla/apihydra/skeleton github.com/divilla/apihydra/skeleton/internal/domain
		exit 0
	fi
	printf '%s\n' github.com/divilla/apihydra/pkg/runner github.com/divilla/apihydra/skeleton github.com/divilla/apihydra/skeleton/internal/domain
	exit 0
fi

[[ $# -eq 4 && $2 == -f && $3 == '{{.Dir}}' ]]
printf 'go-list-dir:%s\n' "$4" >>"$MAKEFILE_TEST_LOG"
case "$4" in
github.com/divilla/apihydra/pkg/runner)
	printf '%s\n' "$MAKEFILE_TEST_REPO/pkg/runner"
	;;
github.com/divilla/apihydra/...)
	printf '%s\n' \
		"$MAKEFILE_TEST_REPO/pkg/runner" \
		"$MAKEFILE_TEST_REPO/skeleton" \
		"$MAKEFILE_TEST_REPO/skeleton/internal/domain"
	;;
*)
	exit 1
	;;
esac
EOF

cat >"$fake_bin/goimports" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[[ $# -eq 2 && $1 == -w ]]
printf 'goimports:%s\n' "$2" >>"$MAKEFILE_TEST_LOG"
EOF

cat >"$fake_bin/staticcheck" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf 'staticcheck:%s\n' "$*" >>"$MAKEFILE_TEST_LOG"
EOF

cat >"$fake_bin/golint" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf 'golint:%s\n' "$*" >>"$MAKEFILE_TEST_LOG"
EOF

chmod +x "$fake_bin/go" "$fake_bin/goimports" "$fake_bin/staticcheck" "$fake_bin/golint"

for tool in create-change-branch.sh codex-code-spec.pl codex-review-loop.pl; do
	printf '%s\n' \
		'#!/usr/bin/env bash' \
		'set -euo pipefail' \
		'printf "%s:%s\n" "$(basename "$0")" "$1" >>"$MAKEFILE_TEST_LOG"' \
		>"$repo/scripts/$tool"
	chmod +x "$repo/scripts/$tool"
done

(
	cd "$repo"
	PATH="$fake_bin:$PATH" \
		MAKEFILE_TEST_LOG="$log" \
		MAKEFILE_TEST_REPO="$repo" \
		make lint
)

[[ $(grep -c '^goimports:' "$log") -eq 1 ]]
grep -Fxq "go-list-dir:github.com/divilla/apihydra/pkg/runner" "$log"
grep -Fxq "goimports:$repo/pkg/runner" "$log"
grep -Fxq 'staticcheck:github.com/divilla/apihydra/pkg/runner' "$log"
grep -Fxq 'golint:github.com/divilla/apihydra/pkg/runner' "$log"
! grep -Fq 'go-list-dir:github.com/divilla/apihydra/skeleton' "$log"

: >"$log"
(
	cd "$repo"
	PATH="$fake_bin:$PATH" \
		MAKEFILE_TEST_EMPTY=1 \
		MAKEFILE_TEST_LOG="$log" \
		MAKEFILE_TEST_REPO="$repo" \
		make lint vet race
)
[[ ! -s "$log" ]]

script_tests=(
	makefile_test.sh
	commit_test.pl
	create-change-branch_test.sh
	change-merge-direct_test.sh
	codex-code-spec_unit_test.pl
	codex-code-spec_test.sh
	codex-review-loop_unit_test.pl
	codex-review-loop_test.sh
)
for tool in "${script_tests[@]}"; do
	printf '%s\n' \
		'#!/usr/bin/env bash' \
		'printf "script:%s\n" "$(basename "$0")" >>"$MAKEFILE_TEST_LOG"' \
		>"$repo/scripts/$tool"
	chmod +x "$repo/scripts/$tool"
done

for target in check test check-scripts tooling-test; do
	: >"$log"
	(
		cd "$repo"
		PATH="$fake_bin:$PATH" \
			MAKEFILE_TEST_LOG="$log" \
			MAKEFILE_TEST_REPO="$repo" \
			make "$target"
	)
	case "$target" in
	check)
		! grep -q '^script:' "$log"
		grep -Fxq "goimports:$repo/pkg/runner" "$log"
		grep -Fxq 'staticcheck:github.com/divilla/apihydra/pkg/runner' "$log"
		grep -Fxq 'golint:github.com/divilla/apihydra/pkg/runner' "$log"
		grep -Fxq 'go:vet github.com/divilla/apihydra/pkg/runner' "$log"
		grep -Fxq 'go:test -race github.com/divilla/apihydra/pkg/runner' "$log"
		grep -Fxq 'go:test -tags=integration ./int-tests -count=1' "$log"
		;;
	test)
		[[ $(<"$log") == 'go:test -short github.com/divilla/apihydra/pkg/runner' ]]
		;;
	check-scripts|tooling-test)
		[[ $(<"$log") == "$(printf 'script:%s\n' "${script_tests[@]}")" ]]
		;;
	esac
done

: >"$log"
(
	cd "$repo"
	PATH="$fake_bin:$PATH" \
		MAKEFILE_TEST_LOG="$log" \
		make implement agent/specs/000-domain-types.md
)
expected_implement_log=$'create-change-branch.sh:agent/specs/000-domain-types.md\ncodex-code-spec.pl:agent/specs/000-domain-types.md\ncodex-review-loop.pl:agent/specs/000-domain-types.md'
[[ $(<"$log") == "$expected_implement_log" ]]

: >"$log"
(
	cd "$repo"
	PATH="$fake_bin:$PATH" \
		MAKEFILE_TEST_LOG="$log" \
		make implement agent/changes/014-enhanced-debug.md
)
expected_change_log=$'create-change-branch.sh:agent/changes/014-enhanced-debug.md\ncodex-code-spec.pl:agent/changes/014-enhanced-debug.md\ncodex-review-loop.pl:agent/changes/014-enhanced-debug.md'
[[ $(<"$log") == "$expected_change_log" ]]

usage_error="$test_root/implement-usage-error"
set +e
(
	cd "$repo"
	PATH="$fake_bin:$PATH" make implement
) > /dev/null 2>"$usage_error"
usage_status=$?
set -e
[[ $usage_status -eq 2 ]]
grep -Fxq 'usage: make implement <spec-or-change-path>' "$usage_error"

printf '%s\n' 'Makefile tests passed'

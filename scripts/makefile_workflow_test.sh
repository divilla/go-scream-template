#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT

repo="$test_root/repo with spaces"
fake_bin="$test_root/bin"
mkdir -p "$repo" "$fake_bin"
cp "$script_dir/../Makefile" "$repo/Makefile"
mkdir -p "$repo/scripts"
cp "$script_dir/check_coverage.py" "$script_dir/check_coverage_test.py" "$repo/scripts/"
touch "$repo/.env"

# Exercise Make's real scheduler without changing source or starting containers.
cat >"$fake_bin/stub" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
command="$(basename -- "$0") $*"
printf 'start %s\n' "$command" >>"$MAKEFILE_TEST_LOG"
if [[ $command == "${MAKEFILE_TEST_FAIL-}" ]]; then
	exit 7
fi
case "$command" in
    'go test '*)
        for arg in "$@"; do
            if [[ $arg == -coverprofile=* ]]; then
                printf 'mode: atomic\nexample/pkg/code.go:1.1,2.1 1 1\n' >"${arg#-coverprofile=}"
            fi
        done
        ;;
	'go fix '*|'go tool mockgen '*|'go tool golangci-lint fmt'|'go mod tidy'|'go mod verify'|'swag '*|'protoc '*) sleep 0.1 ;;
	'docker compose -f docker-compose.yml config --format json')
		cat "$MAKEFILE_TEST_COMPOSE_CONFIG"
		;;
	'migrate create '*)
		[[ $# == 7 && $6 == -- && $7 == "$NAME" ]]
		mkdir -p migrations
		touch "migrations/001_${7}.up.sql" "migrations/001_${7}.down.sql"
		;;
esac
printf 'end %s\n' "$command" >>"$MAKEFILE_TEST_LOG"
EOF
chmod +x "$fake_bin/stub"
for tool in go docker hadolint dotenv-linter swag protoc migrate; do
	ln -s stub "$fake_bin/$tool"
done

export PATH="$fake_bin:$PATH"
export MAKEFILE_TEST_LOG="$test_root/tool.log"

run_make() {
	: >"$MAKEFILE_TEST_LOG"
	# Skip this target inside the fixture to avoid recursively testing the tests.
	make --no-print-directory -C "$repo" -j8 -o test-makefile "$@" >"$test_root/output" 2>&1
}

# Help must list the versioned generator targets.
run_make help
for target in swag-v1 proto-v1; do
	grep -Eq "$target[[:space:]]" "$test_root/output"
done

# Startup and logs must use the same stack even with a different COMPOSE_FILE.
for stack in 'docker compose -f docker-compose.yml' 'docker compose -f custom-compose.yml -p custom-project'; do
	run_make compose-up "BASE_STACK=$stack" COMPOSE_FILE=other-compose.yml
	expected_log=$(printf 'start %s up --build -d db rabbitmq nats\nend %s up --build -d db rabbitmq nats\nstart %s logs -f\nend %s logs -f' "$stack" "$stack" "$stack" "$stack")
	[[ $(cat "$MAKEFILE_TEST_LOG") == "$expected_log" ]]
done

# Preview must remain non-mutating, even if a file shares the target name.
touch "$repo/fix-diff"
run_make fix-diff
if [[ $(cat "$MAKEFILE_TEST_LOG") != $'start go fix -diff ./...\nend go fix -diff ./...' ]]; then
	printf '%s\n' 'fix-diff should only preview go fix changes' >&2
	exit 1
fi

# dotenv-linter 4 requires a subcommand and path; bare invocation prints help.
run_make lint-env
[[ $(cat "$MAKEFILE_TEST_LOG") == $'start dotenv-linter check .\nend dotenv-linter check .' ]]

export MAKEFILE_TEST_FAIL='dotenv-linter check .'
if run_make lint-env; then
	printf '%s\n' 'lint-env should fail when environment linting fails' >&2
	exit 1
fi
unset MAKEFILE_TEST_FAIL

run_make test
awk '
	/^start docker / { exit 1 }
	/^start go test / {
		if ($0 !~ /-race/ || $0 !~ /-covermode=atomic/ ||
		    $0 !~ /-coverprofile=coverage.txt/ || $0 !~ /\.\/internal\/\.\.\. \.\/pkg\/\.\.\./) exit 1
		tests++
	}
	/^start go tool cover / { coverage++ }
	END { if (tests != 1 || coverage != 1) exit 1 }
' "$MAKEFILE_TEST_LOG"

for target in check pre-commit; do
	run_make "$target"
	awk -v target="$target" '
		/^end go tool mockgen / { mocks++ }
		/^start go fix / { if (mocks != 2) exit 1 }
		/^end go tool golangci-lint fmt$/ { formatted = 1 }
		/^start go tool golangci-lint run$/ { if (!formatted) exit 1; lint++ }
		/^start go test / { if (!formatted || mocks != 2) exit 1; tests++ }
		/^start docker / { if (!formatted || tests != 1) exit 1; docker++ }
		END {
			if (mocks != 2 || lint != 1 || tests != 1) exit 1
			if (target == "check" && docker != 2) exit 1
			if (target == "pre-commit" && docker != 0) exit 1
		}
	' "$MAKEFILE_TEST_LOG"

	export MAKEFILE_TEST_FAIL='go tool golangci-lint fmt'
	if run_make "$target"; then
		printf '%s should fail when formatting fails\n' "$target" >&2
		exit 1
	fi
	unset MAKEFILE_TEST_FAIL
	if grep -Eq '^start (go tool golangci-lint run|go test|docker)( |$)' "$MAKEFILE_TEST_LOG"; then
		printf '%s ran checks after formatting failed\n' "$target" >&2
		exit 1
	fi
done

# Local startup must finish module updates before either generator reads source.
run_make run
awk '
	/^end go mod verify$/ { verified = 1 }
	/^start swag / { if (!verified) failed = 1; swag++ }
	/^end swag / { swagger_done = 1 }
	/^start protoc / { if (!verified || !swagger_done) failed = 1; proto++ }
	/^end protoc / { proto_done = 1 }
	/^start go run / { if (!proto_done) failed = 1; app++ }
	END { if (failed || swag != 1 || proto != 1 || app != 1) exit 1 }
' "$MAKEFILE_TEST_LOG"

export MAKEFILE_TEST_FAIL='go mod verify'
if run_make run; then
	printf '%s\n' 'run should fail when module verification fails' >&2
	exit 1
fi
unset MAKEFILE_TEST_FAIL
if grep -Eq '^start (swag|protoc|go run) ' "$MAKEFILE_TEST_LOG"; then
	printf '%s\n' 'run started generators or application after module verification failed' >&2
	exit 1
fi

# Resolve the database volume from Compose, including custom project/volume names.
export MAKEFILE_TEST_COMPOSE_CONFIG="$test_root/compose.json"
for volume in go-scream-template_db_data custom_project_db_data explicit_database_volume; do
	printf '{"volumes":{"db_data":{"name":"%s"},"rabbitmq_data":{"name":"keep_me"}}}\n' "$volume" >"$MAKEFILE_TEST_COMPOSE_CONFIG"
	run_make docker-rm-volume
	grep -Fxq "start docker volume rm $volume" "$MAKEFILE_TEST_LOG"
	[[ $(grep -c '^start docker volume rm ' "$MAKEFILE_TEST_LOG") == 1 ]]
done

export MAKEFILE_TEST_FAIL='docker compose -f docker-compose.yml config --format json'
if run_make docker-rm-volume; then
	printf '%s\n' 'docker-rm-volume should fail when Compose configuration fails' >&2
	exit 1
fi
unset MAKEFILE_TEST_FAIL
if grep -q '^start docker volume rm ' "$MAKEFILE_TEST_LOG"; then
	printf '%s\n' 'docker-rm-volume removed a volume after configuration failed' >&2
	exit 1
fi

for invalid_config in 'not json' '{"volumes":{}}'; do
	printf '%s\n' "$invalid_config" >"$MAKEFILE_TEST_COMPOSE_CONFIG"
	if run_make docker-rm-volume; then
		printf '%s\n' 'docker-rm-volume should fail without a resolved database volume' >&2
		exit 1
	fi
	if grep -q '^start docker volume rm ' "$MAKEFILE_TEST_LOG"; then
		printf '%s\n' 'docker-rm-volume removed a volume with invalid configuration' >&2
		exit 1
	fi
done

# NAME is a variable, so Make should create the pair and exit successfully.
run_make migrate-create NAME=create_tasks
[[ -f "$repo/migrations/001_create_tasks.up.sql" && -f "$repo/migrations/001_create_tasks.down.sql" ]]
if run_make migrate-create NAME=; then
	printf '%s\n' 'migrate-create should reject a missing name' >&2
	exit 1
fi
grep -Fq 'Usage: make migrate-create NAME=<name>' "$test_root/output"
[[ ! -s "$MAKEFILE_TEST_LOG" ]]

printf '%s\n' 'Makefile workflow tests passed'

# Standardize Makefile checks and enforce production coverage

## Outcome

Provide predictable local and Docker verification commands, enforce independent
unit and integration coverage gates, and expand cross-transport regression tests.
Make production migration startup testable and propagate translation-history row
errors instead of returning incomplete successful results.

## Authority and scope

- Follow [AGENTS.md](../../AGENTS.md) for development, testing, coverage scope,
  completion, and permissions. Its committed consolidation is the repository
  policy; this specification does not authorize further edits to that file.
- Reconcile the commits from merge base `700d763` with `origin/master` through
  `848a835` on `change/002-makefile-and-tests-coverage`, plus the user's decisions
  below. This document specifies the resulting requirements, including remaining
  work; it is not a claim that implementation checks have passed.
- Include Makefile workflows, test paths, coverage tooling, Docker and CI wiring,
  migration startup, translation-history error handling, documentation, branch
  statistics, and the repository change-write skill.
- Preserve existing API definitions, shared business rules, authentication, task
  ownership, and transport error mappings except the history correction below.
  No database schema or protected API contract changes are required.
- Ignore the renamed `_CLAUDE.md`, as directed by the user. No skeleton is present.

## Makefile workflows and command compatibility

- `make check` runs dependency tidy/verification, Swagger, protobuf/gRPC and mock
  generation, Go fixes/formatting, Go/Docker/environment linting, and gated unit
  tests in that order. Finish each source-writing stage before readers start,
  including under `make -j8`; a failed prerequisite prevents dependent stages.
- `make check-all` completes `check` successfully before running `int-tests`.
  Routine `check` and `test` must not start Docker or the integration runner.
  Preserve `run` ordering: dependencies and generators finish before startup.
- Replace `pre-commit` with `check`; move the Docker suite previously included in
  `check` to `check-all`. This provides a Docker-independent routine workflow.
- Rename `unit-test` to `unit-tests`, remove the redundant `race` alias, and have
  `test` depend on `coverage`. `unit-tests` generates mocks, verifies coverage
  tooling, and runs each unit build once; `coverage` checks and reports both
  profiles. Benchmarks continue to skip ordinary tests.
- Rename `integration-test/`, its Compose file/service/container, and public
  integration commands to `int-tests/`, `docker-compose-int-tests.yml`,
  `int-tests`, and `compose-up-int-tests`. The last command aliases the same
  coverage-enforcing runner. Retire the old command names; update Docker build
  paths, lint exclusions, teardown wiring, and CI to use the new names.
- Preserve Compose command overrides and argument boundaries, including custom
  files and project names. Continue loading/exporting `.env` with `.env.example`
  as fallback. Operational targets remain phony even when matching files exist.
- Group help into Development, Format & Lint, Tests, and Migrate. For readable
  output, previously silent recipes now echo commands, with one leading blank
  line per recipe command in normal and dry runs. A continued command gets one
  separator; dry runs must not execute it.

## Unit coverage and migration startup

- Measure default and `migrate` builds separately with race detection, atomic
  coverage, and cross-package instrumentation over `cmd`, `config`, `internal`,
  and `pkg`. Save profiles to `coverage.txt` and `.coverage/unit-migrate.txt`.
  Enforce the per-package threshold in AGENTS.md independently for each build;
  success in one cannot compensate for failure in the other. Previously only
  the default build was measured; the additional build exposes tagged code gaps.
- Share handwritten-source filtering between unit and integration checkers.
  Exclude generated files, including protobuf and Swagger header variants.
  Reject missing/empty/invalid profiles, negative counts, inconsistent duplicate
  blocks, unexpected dependencies or test sources, unsafe source paths, missing
  files, and missing module declarations. Validate before filtering and reject
  profiles with no remaining executable statements. Merge duplicate blocks by
  execution presence and compare exact statement counts without rounding.
- Replace migration side effects in package initialization with an explicit
  `app.Migrate` call from `main`, before configuration loading. Importing the
  package must no longer connect to PostgreSQL, allowing tagged unit tests to
  run without it. The executable preserves migration-before-configuration
  behavior; builds without `migrate` expose a no-op migration function.
- Preserve migration inputs and outcomes: require nonempty `PG_URL`, use
  `file://migrations`, append `?sslmode=disable`, and attempt opening at most
  twenty times, sleeping one second after each failed attempt. Missing URL,
  exhausted retries, and migration-up errors retain their fatal diagnostics.
  Success and `ErrNoChange` retain their respective log messages. Close the
  migration on returning paths after `Up`; do not change fatal-exit semantics.

## Integration coverage, lifecycle, and CI

- Build an atomic-coverage service image with the production `migrate` tag using
  Docker's `integration` target. Keep the default `production` image
  uninstrumented, with the same configuration, migrations, and certificates.
- The integration Compose override mounts `GOCOVERDIR` and shares an
  integration-only JWT secret between service and tests. Preserve the local
  HTTPS translation fixture and container-network aliases. Signed fixture
  identities exercise real authentication and database failures without changing
  shared tables or injecting service mocks.
- Each run replaces only `.coverage/integration/` with fresh data and leaves unit
  reports intact. Run one integration suite per checkout. Forward an absolute
  coverage-directory mount to Compose.
- Start dependencies, tracing, service, and tests; wait for the test container.
  Verify the app is still running, then stop it gracefully with a thirty-second
  timeout while dependencies remain available. Require a stopped app with exit
  code zero, no OOM kill, and coverage metadata and counters before conversion.
- Exercise additional startup scenarios using the same instrumented image and
  available dependencies. Check both exit status and diagnostic, wait at most
  seventy-five seconds per scenario, and remove each scenario container.
- Convert service counters into `service.txt`; save the filtered `coverage.txt`,
  per-package and aggregate results in `summary.txt`, and original data in `raw/`
  under `.coverage/integration/`. Count only integration executions, including
  startup/shutdown and startup scenarios, across linked handwritten production
  packages. Apply AGENTS.md's integration threshold to aggregate statement
  counts, not an average of package percentages or merged unit coverage.
- Previously integration commands checked test success alone. They now also
  fail on insufficient or invalid coverage, startup-scenario failures, premature
  app exit, failed shutdown, conversion errors, or failed log/cleanup commands.
  Test failure still permits collection when shutdown succeeds. Always attempt
  logs and Compose teardown; successful cleanup cannot erase an earlier failure.
- CI runs `make int-tests` with Go matching the module and uploads the hidden
  integration report directory even after failure. Ignore `.coverage/` in Git
  and Docker build contexts. Document Docker Compose `wait`, host Go, Python,
  generator/linter prerequisites, report paths, and command side effects.

## Service regressions and compatibility

- After reading history rows, check `rows.Err()`. A failure before the first row
  or after partial results returns a nil result and an error wrapped with
  `TranslationRepo - GetHistory - rows.Err:`, preserving the underlying database
  error. Previously either case could return a successful empty or partial
  history; the correction prevents silent data loss. Existing mappings produce
  HTTP 500, gRPC `Internal`, or the broker's generic internal-error response.
- Preserve the moved integration scenarios and extend REST, gRPC, RabbitMQ, and
  NATS coverage for authentication, validation, dependency errors, task ownership
  and missing resources, task lifecycle/pagination, translation and history.
  Keep transport-specific error behavior: another owner's task is forbidden on
  get/update/transition, while delete returns not found; malformed database IDs
  produce internal errors. Broker failures retain generic responses, and unknown
  operations retain the unregistered-handler response.
- Cover invalid envelopes/data, missing or invalid credentials, invalid JWT
  subjects and unsigned tokens, duplicate registration, incorrect passwords,
  invalid transitions, normalized pagination, missing owners, and translation
  fixture failures. Verify successful broker task transitions through
  `todo -> in_progress -> todo -> in_progress -> done`, deletion, and persisted
  translation history. Coverage percentages do not replace operation assertions.

## Repository tooling and documentation

- As explicitly selected by the user, branch statistics recognizes only
  `int-tests/` as the integration category. Previously it also recognized
  `integration-test/`; retiring that category follows the directory rename.
  Old-path Go test files now use the ordinary unit-test category, and old-path
  Dockerfiles/fixtures use the documentation fallback. Update category regression
  expectations while preserving aggregate totals and destination-based rename
  classification. The existing tests still expect both paths and must be fixed.
- Provide the change-write skill and its display metadata/default prompt. It
  writes specifications without implementing them, accepts an explicit path or
  derives `agent/changes/<name>.md` from a `change/<name>` branch, and creates a
  missing document. Reject absent/ambiguous path or base information and request
  guidance when the template is missing or intent is not established.
- On a matching branch, reconcile individual commits and the aggregate merge-base
  diff against origin's default branch, excluding uncommitted changes. Preserve
  planned requirements, resolve conflicts with the user, follow the template,
  preserve unrelated edits, reread the saved result, and display its full contents.
- Align README and scripts documentation with the resulting workflows and
  compatibility differences. Retain generated protobuf/gRPC output from the
  existing definitions; the committed regeneration changes generator-version
  headers without changing wire contracts. Keep general repository rules in
  AGENTS.md and reference them here.

## Required verification

Follow [AGENTS.md](../../AGENTS.md) for required implementation commands and
completion checks. Verify this change with these specific scenarios:

- `make test-makefile`: ordering/failure propagation under parallel Make,
  Docker isolation, both unit builds and independent gate failures, integration
  aliases/overrides, phony targets, help, and normal/dry-run command separators.
- `make test-coverage`: unit boundaries at and above 95%, integration boundaries
  at and above 90%, statement weighting, duplicate blocks, filtering and invalid
  data; fresh-run isolation, shutdown-before-collection, failure preservation,
  report retention, startup diagnostics/timeouts, and cleanup.
- Migration unit tests: no-tag no-op, import without database side effects,
  entry-point ordering with valid/invalid configuration, missing URL, invalid
  source driver, initial/retried/final-attempt success, exhaustion, up failure,
  no-change, logging, and resource closure on returning paths.
- History unit tests: success/empty/query/scan cases plus iteration failures
  before and after a row, nil results, wrapping, and preserved PostgreSQL code.
  Integration tests verify the failure response across all four transports.
- Docker scenarios: invalid configuration/missing database URL exit 1; invalid
  gRPC/HTTP listeners and NATS subscription exit 0 with server notifications;
  RabbitMQ/NATS connection failures exit 1 with constructor diagnostics.
- `make test-branch-stats`: updated old/new directory classifications, renamed
  destinations, comment/non-comment counts, and unchanged overall totals.
- Repository review: old command removal, renamed paths, uninstrumented default
  image, CI artifacts, documentation/API alignment, and skill path selection,
  reconciliation, conflict handling, and complete-output requirements.

## Acceptance criteria

1. Local and full checks expose the documented commands, execute in order under
   parallel Make, propagate failures, and keep routine checks independent of Docker.
2. Both unit builds independently satisfy AGENTS.md's coverage gate; integration
   coverage satisfies its separate aggregate gate, with invalid data failing.
3. Integration runs use fresh service data, validate shutdown and startup
   scenarios, retain available reports, and always attempt cleanup and CI upload.
4. Migration imports have no database side effects; executable ordering and
   existing migration outcomes remain covered and compatible.
5. History iteration failures cannot return successful incomplete data, and
   operation/error regressions are verified across the four transports.
6. Statistics tests enforce only the selected `int-tests/` category, current
   documentation matches behavior, generated API contracts remain compatible,
   and the change-write skill produces reconciled specifications.

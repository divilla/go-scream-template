# Repository Instructions

This Go service template uses clean architecture. Shared `user`, `task`, and
`translation` use cases serve REST, gRPC, RabbitMQ RPC, and NATS RPC.

## Implementation rules

- If behavior is ambiguous, ask before choosing how to proceed.
- Keep general development, testing, and completion rules in this file. Change
  documents reference these rules and define scope, behavior, acceptance criteria,
  and scenarios.
- Document each intentional compatibility difference, including its reason and
  observable behavior before and after the change. Test each difference.
- Keep documentation and generated API definitions aligned with behavior.
- Unit-test handwritten production behavior. Verify every acceptance criterion
  with an appropriate unit test, integration test, repository check, or documented
  review. Avoid redundant tests that cover no additional behavior, edge case,
  regression, or acceptance criterion.

## Required checks

For any code implementation work:

1. Run `make check`  after each coherent implementation increment, including any 
   subsequent fixes.
2. Run `make int-tests` just before declaring implementation complete.
3. Run `make deps-audit` if `go.mod` changed. 

All required commands must pass before declaring implementation complete.

Unit tests must enforce statement coverage greater than 95% in every handwritten
production Go package under `cmd`, `config`, `internal`, and `pkg`, excluding
generated code and dependencies. Measure the default build and the production
`migrate` build separately, including tagged production files. Each build must
pass the threshold. Keep the coverage scope aligned with production packages and
build tags as they change.

`make int-tests` must enforce aggregate statement coverage greater than 90% across
handwritten production packages linked into the service, excluding generated code
and dependencies. Measure integration coverage independently of unit tests.
Missing or invalid coverage must fail the check.

## Other commands and setup

Use Makefile targets, not the underlying tools, wherever possible. Run `make help` 
to familiarize yourself with the available commands.

## Permissions

Modify `AGENTS.md` only when the user explicitly requests a change to that specific
path. General implementation, fix, refactor, format, test, or documentation
requests do not authorize editing it.

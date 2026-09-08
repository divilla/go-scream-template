# Repository Instructions

Go service template using clean architecture: shared `user`, `task`, and
`translation` use cases serve REST, gRPC, RabbitMQ RPC, and NATS RPC.

## Implementation rules

- Ask before choosing ambiguous behavior.
- Keep general development, testing, and completion rules here; change documents
  reference them and define scope, behavior, acceptance criteria, and scenarios.
- Document intentional compatibility differences with reasons and observable
  before/after behavior; test each difference.
- Keep documentation and generated API definitions aligned with behavior.
- Unit-test all production code; require **greater than 95% coverage in every
  production package**. Maintain a unit test for every acceptance-criteria bullet.
  Avoid tests that add no coverage unless they prove an acceptance criterion.

## Required checks

Use Makefile targets, not underlying tools. **Before completing each implementation
phase**, all three commands must pass, plus applicable checks below:

1. `make format`
2. `make linter-golangci`
3. `make test`

| Phase/change | Additional required commands |
| --- | --- |
| Dependencies | `make deps` (tidy, verify, remove obsolete dependencies/checksums) |
| REST routes, handlers, or annotations | `make swag-v1` |
| Protobuf definitions | `make proto-v1` |
| Mocked interfaces | `make mock` |
| API behavior, transport wiring, or shared application behavior | `make compose-up-integration-test` (affected flows and cross-transport regressions) |
| Before pushing | `make pre-commit` |

Run applicable dependency/generation commands before the three phase checks.
Report implementation complete only after all required checks pass.

`make test` runs `./internal/... ./pkg/... ./config/... ./cmd/...` with `-race`,
atomic coverage, and `coverage.txt`; it counts execution across test packages and
enforces >95% statement coverage per production package with executable statements.
The coverage checker and its regression tests require `python3`.

## Other commands and setup

Make loads/exports `.env`, falling back to `.env.example`; `make` shows `make help`.
`make bin-deps` installs Go tools and protobuf plugins; install `protoc` separately.
`make run` and `make pre-commit` regenerate Swagger/protobuf and require `swag` and
`protoc` on `PATH`. Integration tests run in Docker for container DNS;
`make integration-test` aliases `make compose-up-integration-test`.

| Task | Command |
| --- | --- |
| Vulnerability scan / preview Go fixes | `make deps-audit` / `make fix-diff` |
| Start dependencies and follow logs / start full stack | `make compose-up` / `make compose-up-all` |
| Stop/remove stack containers | `make compose-down` |
| Run locally with startup migrations | `make run` |
| Create migration pair / apply migrations | `make migrate-create NAME=<name>` / `make migrate-up` |

## Permissions

Modify `AGENTS.md` only when the user explicitly directs that specific path to
change. General implementation, fix, refactor, format, test, or documentation
requests do not authorize editing it.

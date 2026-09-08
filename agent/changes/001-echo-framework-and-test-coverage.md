# Migrate to Echo v5 and enforce per-package test coverage

## Outcome

Complete the partially started migration from Fiber to LabStack Echo v5. The
service must build and run with Echo v5 throughout its HTTP stack, with existing
API behavior preserved as closely as practical.

Remove prefork support and all Fiber references outside `agent/`. Require unit
test coverage greater than 95% in every production package.

## Authority and scope

- Use `github.com/labstack/echo/v5` as the REST framework.
- Cover HTTP server lifecycle, application wiring, REST routes and handlers,
  middleware, HTTP configuration, dependencies, tests, and documentation.
- Include the Docker PostgreSQL port and credential changes specified below.
- Use the existing handlers, request and response types, Swagger definitions,
  and tests as the compatibility baseline. The current mixed-framework code is
  an incomplete migration, not a working implementation to preserve literally.
- Keep business rules and shared use-case responsibilities intact. Changes to
  gRPC, RabbitMQ RPC, NATS RPC, or database contracts are outside this migration.
- The user explicitly authorizes removing or updating Fiber references in
  `AGENTS.md` and moving generally applicable development, testing, and
  completion instructions there. Keep its testing guidance aligned with the
  per-package coverage requirement and preserve unrelated instructions.
- Fiber references are allowed anywhere inside `agent/`, including this change
  document. The cleanup applies to project files outside that directory,
  including code, tests, dependency metadata, documentation, and agent guidance.

## HTTP server and configuration

- Complete the Echo v5 server implementation in `pkg/httpserver` and its wiring
  in `internal/app`. Remove commented-out Fiber setup and obsolete framework
  abstractions.
- Start one HTTP listener using the configured address. Report startup failures
  through the server's notification mechanism and shut down gracefully within
  the configured shutdown timeout.
- Preserve the port, read timeout, write timeout, and shutdown timeout options,
  and ensure the running server honors them.
- Remove `Prefork`, the server's prefork field, `UsePreforkMode`, and
  `HTTP_USE_PREFORK_MODE` from code, configuration, environment examples, Compose
  files, tests, and documentation. Do not retain a no-op compatibility option.

## Docker PostgreSQL configuration

- Publish PostgreSQL on host port `15432`, keeping port `5432` inside the
  container. Use the Docker Compose port mapping `15432:5432`.
- Set Docker PostgreSQL's `POSTGRES_USER` and `POSTGRES_PASSWORD` to `postgres`.
- Update the corresponding connection URLs, environment examples, and
  documentation to match the port and credentials. Host connections use
  `localhost:15432`; connections within the Docker network use `db:5432`.

## Routes, requests, and responses

- Migrate all REST route registration, handlers, and response helpers to Echo
  v5, including authentication, user profile, task operations, and translation.
- Preserve existing route paths and methods, request fields, validation,
  pagination defaults, response bodies, and status codes as closely as practical.
  Check trailing-slash handling explicitly, particularly for `/v1/tasks`.
- Preserve authentication and authorization rules, including public registration
  and login, protected routes, and task ownership checks. Framework migration
  must not weaken these rules.
- Pass the HTTP request's standard Go context into use cases so cancellation
  and tracing propagate through the application.
- Handle malformed bodies, invalid parameters, validation failures, and use-case
  errors deliberately. Preserve the existing JSON error shape where practical;
  do not expose internal errors or panic details to clients.
- Exact framework-level compatibility is not required. Apply the compatibility
  documentation and testing rules in `AGENTS.md` to intentional differences.

## Middleware and operational endpoints

- Replace all Fiber middleware with implementations compatible with Echo v5.
  Verify compatibility of the existing partially migrated integrations rather
  than assuming that an Echo-named dependency supports v5.
- Preserve JWT validation and make the authenticated user ID available to
  handlers without allowing request data to override it.
- Preserve request logging and panic recovery through the project's logging
  facilities. Log useful request and error context and recover panics without
  terminating the service.
- Keep `/healthz` available and successful when the HTTP server is running.
- Keep Prometheus metrics at `/metrics` and Swagger UI under `/swagger/` when
  their respective configuration flags are enabled. Preserve disabled behavior.
- Preserve configurable HTTP tracing and propagation into use cases. Keep
  metrics and tracing behavior as close as practical and document any changes
  to metric names, labels, or trace metadata.

## Dependency and reference cleanup

- Remove Fiber imports, types, adapters, middleware dependencies, comments,
  examples, and obsolete test helpers outside `agent/`.
- Make Echo v5 a direct dependency and use compatible integration dependencies.
  Remove obsolete Fiber dependencies and checksums.
- Update README content, badges, links, `CLAUDE.md`, any Fiber references in
  `AGENTS.md`, and other project documentation to describe the final Echo setup.
- Check project file contents and names case-insensitively for remaining Fiber
  references outside `agent/`, including `go.mod`, `go.sum`, and hidden project
  configuration. Git history and downloaded dependency caches are not project
  source files to rewrite.

## Required verification

Follow the development, unit-test, and completion requirements in
[`AGENTS.md`](../../AGENTS.md). The migration requires these specific checks:

- Require unit-test coverage greater than 95% in every production package.
  Measure each package across the complete unit-test suite, including tests
  in other packages that exercise it.
- Cover HTTP handlers and middleware with request/response tests, including
  success paths, invalid input, missing or invalid credentials, forbidden
  access, missing resources, use-case failures, and panic recovery.
- Verify request-context propagation, operational endpoint configuration,
  startup failure reporting, timeout configuration, and graceful shutdown.
- Supplement behavioral tests with repository checks for removed dependencies,
  prefork configuration, and Fiber references outside `agent/`.
- Verify the REST user, task, and translation flows with integration tests and
  check for regressions in the gRPC, RabbitMQ RPC, and NATS RPC transports.

## Acceptance criteria

1. The application builds and serves every existing REST feature through Echo
   v5, preserving behavior as closely as practical and documenting and testing
   intentional compatibility differences.
2. Authentication, authorization, validation, and error handling remain
   effective after migration, and use cases receive the HTTP request context.
3. The HTTP server starts one listener, reports startup failures, honors port
   and timeout configuration, and shuts down gracefully. Prefork support and
   its configuration are removed.
4. Request logging, panic recovery, health checks, optional metrics, optional
   Swagger UI, and optional tracing work with Echo v5.
5. Project files outside `agent/` contain no Fiber references or dependencies,
   and documentation and agent guidance describe the resulting implementation.
   References inside `agent/` are allowed.
6. Docker PostgreSQL publishes host port `15432` to container port `5432` and
   uses username and password `postgres`, with matching host and Docker-network
   connection settings and documentation.

## Implementation compatibility notes

- `AGENTS.md` was compacted with explicit user authorization, preserving its core
  guidance, removing all skeleton mentions, and listing mandatory Make checks
  for every implementation phase plus applicable dependency, generation, and
  integration checks. Command guidance now matches the Makefile.
- Makefile coverage changes expand unit tests to `internal`, `pkg`, `config`, and
  `cmd`, count execution across test packages with `-coverpkg`, and enforce
  strictly greater than 95% statement coverage in each production package.
  The unit-test target also runs the Python coverage-checker regression tests.
- Makefile workflow checks cover generator/check ordering and test isolation.
  `make integration-test` delegates to the Docker integration target, and
  migration creation uses `make migrate-create NAME=<name>`.
- Routing uses Echo's case-sensitive matching instead of Fiber's default
  case-insensitive matching. API trailing slashes remain accepted without redirects.
  GET routes retain automatic HEAD handling, including authentication and empty
  response bodies. `TestRoutingErrors`, `TestTaskTrailingSlash`, `TestHEADRoutes`,
  and `TestHTTPHEAD` cover these decisions.
- Validation and domain error bodies remain `{"error":"..."}`. Unmatched routes,
  unsupported methods, and recovered panics use Echo's safe default
  `{"message":"..."}` envelope instead of Fiber's default plain-text errors.
  `TestRoutingErrors` and `TestObservability` cover these framework errors.
- Metrics use the Echo integration's `echo_` names and `code`, `method`, `host`,
  and `url` labels. This replaces Fiber's HTTP metrics and the incomplete
  migration's invalid `my-service-name` metric subsystem. The Echo integration
  adds request/response size histograms; it does not emit the previous
  in-flight gauge or `service` label. `TestOperationalEndpoints` verifies the
  metrics endpoint and registry isolation.
- HTTP tracing uses `github.com/labstack/echo-opentelemetry` as its instrumentation
  scope and names spans as `METHOD /route`. W3C parent context and request
  cancellation still reach use cases. `TestTracing` covers enabled and disabled
  tracing, parent linkage, span naming, and cancellation.

- Integration tests use a local HTTPS translation fixture through a
  `translate.google.com` alias scoped to the integration Docker network. This
  verifies the real translation client, REST responses, and persisted history
  without depending on Google's availability or rate limits. Live Google
  availability is outside this verification.

- Review fixes restore the 4 MiB request-body limit for fixed-length and chunked
  requests, including public registration and login. The complete body is checked
  before handlers run, including XML content after the first document element.
  Oversized bodies return HTTP 413 using Echo's `{"message":"Request Entity Too Large"}` envelope instead of
  the former plain-text framework error. `TestBodyLimit`, `TestRequestBodyLimit`,
  `TestXMLRequestBodyLimit`, and `TestHTTPBodyLimit` cover the limit and transport
  behavior.
- With Swagger enabled, `/swagger` and `/swagger/` explicitly redirect with HTTP
  301 to `/swagger/index.html`. Disabled Swagger still returns 404.
  `TestOperationalEndpoints` and `TestHTTPSwaggerRoots` cover the entry points.

- Review fixes restore URL-encoded and multipart binding for every request DTO,
  structured JSON media types (including parameters), and gzip, deflate, and
  Brotli request decoding. Form binding uses body fields only, preserving the
  previous exclusion of query parameters. The existing content-encoding decode
  order is retained. `TestBodyCompatibility`, `TestInvalidCompatibleBodies`,
  `TestCompressedRoutes`, and `TestHTTPRequestDecoding` cover these behaviors.
- Expanded bodies and each intermediate decoding stage now share the 4 MiB
  wire-body limit. Previously a small compressed body could expand beyond that
  limit; it now returns HTTP 413 before handlers run, bounding decompression
  memory. Malformed compressed streams return the safe HTTP 400 framework
  envelope instead of passing decoder error text to the body parser.
  `TestCompressedBodyLimit`, `TestCompressedBodyErrors`,
  `TestStackedContentEncodings`, and `TestHTTPCompressedBodyLimit` cover decoding,
  errors, fixed-length/chunked bodies, and expansion limits.
- Review fixes restore case-insensitive media types for XML, URL-encoded forms,
  and multipart forms as well as JSON. Media-type parameters, including
  case-sensitive multipart boundaries, are preserved. `TestBodyCompatibility`,
  `TestInvalidCompatibleBodies`, and `TestHTTPRequestDecoding` cover successful
  binding, invalid bodies, and restoration of the original request header.
- Review fixes bind the HTTP metrics `host` label to the configured `APP_NAME`
  instead of the request's Host header. Distinct client hosts now aggregate into
  one host label, preventing unbounded series growth in the counter and all three
  histograms. `TestMetricsHostCardinality` verifies aggregation and labels across
  all four families, including an empty application name.
- Review fixes bound the HTTP metrics `method` label to GET, HEAD, POST, PUT,
  DELETE, CONNECT, OPTIONS, TRACE, PATCH, or `UNKNOWN`. Previously every raw
  method allocated separate series; custom methods and case variants now
  aggregate under `UNKNOWN` in the counter and all three histograms, preventing
  unbounded method-label cardinality. Routing and response codes are preserved.
  `TestMetricsMethodCardinality`, `TestMetricsStandardMethods`, and
  `TestHTTPMetricsMethodCardinality` cover aggregation and standard labels.
- Review fixes place body validation around the resolved route handler, inside
  global request logging and metrics. Malformed compressed streams, body read
  errors, and oversized bodies retain their 400/413 responses and are now logged
  and counted. `TestRejectedBodyObservability` covers these rejected requests.
- Nonpositive shutdown timeout options retain immediate-deadline behavior by
  passing one nanosecond to Echo. This avoids Echo's ten-second default for zero
  and disabled shutdown for negative values. `TestShutdownDeadline` covers zero,
  negative, and positive deadlines, forced connection closure, and listener release.
- Application logs no longer include the automatic `caller` field; timestamp,
  level, and message remain. HTTP logs use Echo's built-in RequestLogger with
  zerolog to emit request details as JSON fields (`request_id`, `remote_ip`,
  `method`, `uri`, `status`, `response_size`, and `latency` in milliseconds).
  Echo's RequestID middleware preserves a supplied `X-Request-ID` or generates
  one, returning it in the response. This allows failures to be correlated using
  an ID instead of a source location. One completed-request event replaces the
  separate error and access messages: returned errors log at error level and
  successful handler returns at info level. Echo's built-in recovery retains
  panic error text and the current goroutine's stack in the `error` and `stack`
  fields; responses still hide panic details. `TestLogFields`, `TestObservability`,
  `TestRejectedBodyObservability`, and `TestHTTPRequestID` cover these changes.

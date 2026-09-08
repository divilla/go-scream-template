# `<component>`

<!--
Keep the guide aligned with the binding skeleton. Summarize its contract; do
not copy declarations or invent behavior. Skeleton placeholders use the single
`// TODO: implement` plus zero-value-return convention (or no return for a void
method). They mark required implementation work and are not behavior that
production code preserves. Remove optional sections and links that do not apply.
-->

## Status and ownership

- Binding reference: [`skeleton/path/file.go`](../../skeleton/path/file.go)
- Reference tests: [`skeleton/path/file_test.go`](../../skeleton/path/file_test.go)
- Shared product contract: [`prd.md`](../prd.md)
- Shared domain types: [`000-domain-types.md`](../specs/000-domain-types.md)
- Related guide: [`NNN-related.md`](../specs/NNN-related.md)
- Status: skeleton-aligned implementation guide

## Reference contract

The binding skeleton defines {API, behavior, and owned responsibility}. This
guide does not reproduce those declarations or algorithms. {Briefly explain
the component's role, inputs, outputs, and collaboration boundaries.}

## Boundaries

{State what this component owns and what remains with other packages. Cite the
owning guide or skeleton contract where useful.}

<!-- Remove this section if the reference-contract summary is sufficient. -->

## Required implementation and tests

- Production output: `{production/path.go}` implements the binding declarations
  and contracts without retaining skeleton placeholders.
- Test output: `{production/path_test.go}` covers the component's behavior,
  errors, edge cases, and ownership boundaries.
- Each numbered acceptance criterion is traced to at least one meaningful test.
- Component unit-test statement coverage remains greater than 95%; tests must
  contribute to the repository-wide coverage target and must not merely execute
  declarations without asserting behavior.

<!--
For a test-only specification, replace the production and unit-test bullets
with the concrete harness, fixture, command, and coverage obligation. Do not
pretend that a verification-only guide owns production behavior.
-->

## Deliberately unspecified

The skeleton does not define:

- {material implementation choice or edge case};
- {behavior that must not become a product requirement}.

<!-- Remove this section when nothing material is unspecified. -->

## Acceptance criteria

<!-- Use one independently testable requirement per item. Every item needs a
meaningful test. Component criteria normally need unit tests; a test-only guide
may use integration tests. Do not require behavior absent from the skeleton. -->

1. Names, signatures, static errors, and constructor state match the reference.
2. {Observable behavior and state mutation match the reference contract.}
3. {Integration and ownership boundaries are preserved.}
4. {Important unspecified behavior is not introduced as a requirement.}

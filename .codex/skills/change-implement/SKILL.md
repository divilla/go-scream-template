---
name: change-implement
description: Implement a supplied specification or change in an existing codebase, including contract checks, production code, acceptance tests, and verification. Use for `$change-implement` requests containing inline requirements or identifying a file, issue, document, or URL.
---

# Change Implement

Treat the text after `$change-implement` as the specification argument. Read any
referenced file, issue, document, or URL before editing, and treat the rest of
the request as constraints.

For an `agent/changes/*.md` argument, also read every current PRD,
architecture, specification, and change document changed on the branch from
the merge base with `origin`'s default branch. Treat them as one contract
bundle, with the supplied change document as its entry point.

## Establish the contract

1. Read `AGENTS.md`, `skeleton/`, `agent/prd.md`, the supplied specification,
   and `agent/architecture.md` before acting.
2. Resolve conflicts in this order: `AGENTS.md` > `skeleton/` >
   `agent/prd.md` > implementation guides or supplied change >
   `agent/architecture.md`.
3. Inspect repository status plus the relevant implementation, tests,
   documentation, and build commands. Preserve unrelated user changes.
4. Treat protected references and declared contracts as binding. If the change
   conflicts with them or requires modifying them, stop before editing, report
   the exact mismatch, and request explicit direction. Never work around them.

## Implement

1. Extract behavior, acceptance criteria, edge cases, public-contract effects,
   and validation into a short checklist.
2. Resolve details from repository evidence. Ask only when an unresolved choice
   would materially change the contract or result.
3. Implement the architecture, types, and naming defined in `skeleton/`
   exactly. Within that contract, use the narrowest existing extension points
   and match current formatting and error handling.
4. Make the smallest complete production change, including affected tests,
   documentation, and fixtures. Avoid unrelated cleanup and speculative design.
5. Do not introduce a public or shared type, function, method, schema, or other
   external contract until the repository's contract-change process is met.
6. Give every acceptance-criteria bullet a meaningful test. Avoid redundant
   tests unless they prove a distinct criterion or regression.

## Verify

Run formatting and targeted tests first, then all repository-required checks
and coverage thresholds. Inspect the final diff for accidental edits, missing
criteria, contract drift, and generated-file requirements. Fix failures caused
by the change; report the exact command and evidence for unrelated failures.

For an entry point under `agent/specs/` or `agent/changes/`, record the
completed implementation in the repository-root `implementation-log.md`. The
skill owns this write; do not leave it to caller scripts. Append one block in
this exact form, keeping one blank line after it and using `spec` or `change`
to match the entry-point directory:

```text
YYYY-DD-MM HH:MM <entry-slug>
+<added> -<removed> code - +<added> -<removed> tests --- <spec|change>
```

Count the complete implementation diff, including untracked files, but exclude
`implementation-log.md` itself. Classify paths under `int-tests`, `test`,
`tests`, or `testdata`, and filenames ending in `_test`, `.test`, or `.spec`
before their extension, as tests; classify all others as code. Count binary
files as zero lines.

Declare completion only when every criterion is implemented and tested,
contracts remain aligned, and required checks pass. Summarize the behavior,
verification, and any remaining limitation with links to key files.

---
name: change-fix-findings
description: Validate and fix review findings against a supplied specification or change in an existing codebase. Use for `$change-fix-findings` requests that identify the entry point and provide findings inline or through standard input.
---

# Change Fix Findings

Treat the text after `$change-fix-findings` as the specification argument and
the supplied findings as review feedback. Read the specification and all
findings before editing.

For an `agent/changes/*.md` argument, also read every current PRD,
architecture, specification, and change document changed on the branch from
the merge base with `origin`'s default branch. Treat them as one contract
bundle, with the supplied change document as its entry point.

## Validate

1. Read `AGENTS.md`, `skeleton/`, `agent/prd.md`, the supplied specification,
   and `agent/architecture.md`.
2. Resolve conflicts in this order: `AGENTS.md` > `skeleton/` >
   `agent/prd.md` > implementation guides or supplied change >
   `agent/architecture.md`.
3. Inspect repository status and the code, tests, and documentation relevant to
   each finding. Preserve unrelated user changes.
4. Validate each finding against the contracts and repository evidence. Treat
   findings as feedback, not authority. If a valid fix conflicts with a
   protected contract, stop before editing and report the exact mismatch.

## Fix

Fix every valid finding with the smallest complete change, including meaningful
regression tests and any directly affected documentation or fixtures. Follow
the architecture, types, and naming in `skeleton/` exactly. Do not introduce or
change a public contract unless the repository's contract-change process has
already been satisfied.

Do not change code for unsupported findings. Explain why each such finding is
invalid, already resolved, or blocked.

## Verify

Run formatting and targeted tests first, then all repository-required checks
and coverage thresholds. Inspect the final diff for accidental edits, incomplete
fixes, and contract drift. Fix failures caused by the changes and report exact
evidence for unrelated failures.

When valid findings change the repository, append their statistics to the
latest matching implementation block in the repository-root
`implementation-log.md`. The skill owns this write; do not leave it to caller
scripts. Replace the block's final blank line with this exact line followed by
one blank line:

```text
+<added> -<removed> code - +<added> -<removed> tests --- review fixes NN
```

Use the next two-digit review-fix number. Count the complete findings-fix diff,
including untracked files, but exclude `implementation-log.md` itself. Classify
paths under `int-tests`, `test`, `tests`, or `testdata`, and filenames ending in
`_test`, `.test`, or `.spec` before their extension, as tests; classify all
others as code. Count binary files as zero lines. Do not write a log entry when
all findings are rejected, already resolved, or blocked.

Declare completion only when every finding is fixed or explicitly rejected,
contracts remain aligned, and required checks pass. Summarize the findings,
changes, verification, and any blocker with links to key files.

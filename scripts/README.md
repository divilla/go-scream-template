# Repository Helper Scripts

This directory contains maintained, user-owned helpers for working with the APIHydra repository. These files are intentional project tooling, not generated output or repository-hygiene candidates. Do not delete or relocate them without explicit user approval.

## Run checks

Run `make check` for application linting, vetting, race tests, and black-box
integration tests. Run `make check-scripts` separately for the repository script
tests. `make test` runs only short application tests; `make tooling-test` remains
an alias for `make check-scripts`.

Run `make install` from the repository root to execute `go install ./cmd/apih`.
Go installs the binary into `GOBIN` when set or otherwise `$GOPATH/bin`
(normally `~/go/bin`). Example suites and their run instructions are in
[`docs/examples`](../docs/examples/README.md).

## Implement a specification or change

Run the complete implementation and review workflow with:

```shell
make implement agent/specs/000-domain-types.md
make implement agent/changes/014-enhanced-debug.md
```

This creates and checks out the entry's change branch, uses `$change-code` to
implement the specification or change, commits that result, and then starts the
review loop. Each findings pass uses `$change-fix-findings` before the loop
commits and pushes the resulting fixes.

## `commit-user.pl` and `commit-agent.pl`

Both scripts stage all repository changes, create a commit, and push the current branch to `origin`. Use `commit-user.pl` for user-authored changes; with no argument, its commit message defaults to `User commit`:

```shell
scripts/commit-user.pl
```

Use `commit-agent.pl` for agent-authored changes; with no argument, its commit message defaults to `Agent commit`:

```shell
scripts/commit-agent.pl
```

Pass either script one argument to provide a different commit message:

```shell
scripts/commit-user.pl "Initial commit"
scripts/commit-agent.pl "Implement runner"
```

Both scripts resolve the repository root from their own location, so they can be invoked from any working directory. They refuse to run from a detached `HEAD`.

## `create-change-branch.sh`

Create and check out a change branch for a specification or change:

```shell
scripts/create-change-branch.sh agent/specs/000-domain-types.md
scripts/create-change-branch.sh agent/changes/014-enhanced-debug.md
```

The path must have the form `agent/{specs,changes}/<change-slug>.md`. The script
requires a clean working tree, checks out `master`, fetches remote refs, and
verifies that `master` is clean. It then checks out an existing local
`change/<change-slug>` branch, creates a local tracking branch when only
`origin/change/<change-slug>` exists, or creates a new branch when neither
exists. On success, it prints the checked-out branch name.

## `change-merge-direct.pl`

Squash and fast-forward a branch directly into `master`, push `master`, and
delete the remote source branch. With no argument, the current branch must be a
numbered `change/<change-slug>` branch and the commit message defaults to
`Implement change <change-slug>`:

```shell
scripts/change-merge-direct.pl
```

Pass one non-empty argument to set the squash commit message and permit any
source branch other than `master`, including `change/`, `fix/`, and `test/`
branches:

```shell
scripts/change-merge-direct.pl "Fix request validation"
```

The source branch must have a matching branch on `origin`, the worktree must be
clean, and `origin/master` must be its ancestor. The script rejects detached
`HEAD` and never permits `master` to be used as its own source branch.

## `codex-code-spec.pl`

Implement a specification or change on its already-checked-out change branch:

```shell
scripts/codex-code-spec.pl agent/specs/000-domain-types.md
scripts/codex-code-spec.pl agent/changes/014-enhanced-debug.md
```

The path must have the form `agent/{specs,changes}/<change-slug>.md`, and the
current branch must be `change/<change-slug>`. Change documents make both Codex
skills read the branch's changed PRD, architecture, specification, and change
documents as one contract bundle. The script requires a clean working tree so
the automated commit cannot absorb unrelated changes.

The startup context is printed in `Repository`, `Specification`, `Branch`
order, followed by an `=== Implementation ===` heading and the rendered Codex
command. In a color-capable terminal, labels remain white while repository,
specification, and branch values are blue, magenta, and green respectively.

The implementation runs as
`codex exec --json -o <temporary-result> '$change-code <specification>'` with
the same elapsed-time, output-marker, activity-marker, success, failure, and
interrupt behavior as `codex-review-loop.pl`. Raw JSON output is suppressed on
success and printed on failure. When Codex succeeds, the script requires both a
final response and repository changes. It prints the final response between the
progress line and the changed-file list, then commits and pushes the changes as
`Implement change <change-slug>`. Temporary output stays outside the repository
and is removed on exit.

## `codex-review-loop.pl`

Run Codex review-and-fix passes until the native review returns no comments:

```shell
scripts/codex-review-loop.pl agent/specs/010-executor-service.md
```

The required first positional argument is the specification file. The script
always reviews a branch range so every pass includes fixes committed by earlier
passes. It uses the default remote branch from `origin/HEAD` unless
`--base BRANCH` is supplied; arguments after the specification are forwarded
to Codex. Every review pass uses the native `codex exec review --base` target,
with the base resolved to a pinned commit before the loop begins. The
specification is supplied only to the subsequent `$change-fix-findings` fixer
because Codex treats a custom review prompt and `--base` as conflicting review
targets.
Custom review instructions from the caller are therefore rejected, whether
supplied as a bare positional prompt, `-` for standard input, or after `--`.
For example:

```shell
scripts/codex-review-loop.pl agent/specs/010-executor-service.md --base develop
```

The `--uncommitted` and `--commit SHA` review targets are rejected because they
cannot include the loop's later fix commits.

Each invocation creates a private temporary directory outside the repository,
preferring `TMPDIR`, `TMP`, `TEMP`, and then `/tmp`, and prints its
`findings.md` path. A candidate that resolves inside the checkout is skipped.
Every findings pass redirects that file through standard input to a fresh
`codex exec` invocation and explicitly invokes `$change-fix-findings` with the
positional specification file. The skill validates the findings against the
specification and repository contracts, implements valid fixes with tests and
verification, and preserves unrelated changes. The prompt leaves commits and
pushes to the loop. Each review and fix command has a numbered heading, and the
loop prints each captured final response after its progress line. If the fixer
makes no repository changes, the response keeps protected-contract blockers and
rejected findings visible before the loop stops without committing. Otherwise,
it prints changed files, then commits and pushes them as
`review fixes 01`, `review fixes 02`, and so on. Native Codex review rendering
adds an exact `Review comment:` or `Full review comments:` header whenever its
structured result contains findings. The loop checks those headers instead of
searching unconstrained review prose for verdict text, and rejects empty output
plus Codex's failed-response and interrupted-review fallbacks before a
headerless result can be accepted as clean. `codex exec review`
ignores `--output-schema`, so the script rejects that option. The commit
operation is built into the running review-loop process, so a fixer-modified
repository helper is never executed with the caller's permissions. The script
requires a clean working tree so automated commits cannot include unrelated
work. All generated findings, final-response, and progress-log artifacts
stay outside the repository, and their private directory is removed on every
exit path.
The default `/tmp` location works on Linux and macOS. The script requires Perl
and a POSIX environment because interruption is propagated to the active Codex
process group.

While Codex runs, its output is replaced by a progress line such as
`[ ] 00:00 •••••••••●`, containing elapsed minutes and seconds plus accumulated
output bullets, rate-limited to at most one bullet per second while output is
received. The activity marker is appended directly to the bullets without
intervening whitespace. In a terminal, Perl's timed I/O
multiplexing updates the activity marker every 250 milliseconds independently
of Codex output, without delivering asynchronous progress signals to the
process. The reusable implementation lives in `lib/APIHydra/Progress.pm`. The
status resolves to `[✅]` on success or `[❌]` on failure or interruption; a
finished line looks like `[✅] 11:11 ••••••••••••`. Pressing Ctrl+C
terminates the active Codex command and the loop. Review startup output uses the
same repository, specification, and branch order and colors as the
implementation script, followed by the base, pinned base, review options, and
findings file. Additional values retain their established colors. All terminal
colors are disabled when `NO_COLOR` is set.
Before each run, the Codex command is printed with readable single-quoted
arguments between separator lines sized to its longest rendered line. The
fixer's stdin redirection is rendered on a separate line, so neither separator
extends into that line. Codex runs in JSON mode so progress events remain
streamable while their raw content is suppressed on success. If Codex fails,
its captured output is printed to standard error for diagnosis.

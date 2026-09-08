---
name: change-write
description: Create or update a compact change specification from a supplied path or the current change branch, reconcile it with branch commits, resolve ambiguities with the user, and format it using the repository change template. Use for writing or refreshing change documents, including $change-write invocations.
---

# Change Write

Write the change document; do not implement its requirements.

## Resolve the change file

1. Read the applicable `AGENTS.md` instructions and inspect repository status
   and the current branch name. Resolve relative paths from the repository root.
2. If invocation arguments supply `<change-path>`, use that path. Treat any
   additional instructions as requirements for the document. Ask if the path
   argument is ambiguous.
3. Otherwise, search the branch name using `/change\/(.+)/`. Use capture group
   1 as `<change-name>` and set `<change-path>` to
   `agent/changes/<change-name>.md`. If the regex fails, including when HEAD is
   detached, stop with an error explaining that a path or a matching change
   branch is required.
4. If the file does not exist, create its parent directories as needed and an
   empty file. Preserve existing contents for comparison when it does exist.

## Reconcile requirements and commits

1. Read the entire change file and `agent/templates/change-template.md`.
   If the template is missing, report the missing file and ask how to proceed;
   do not invent a replacement format.
2. Independently of how the path was selected, check the branch name using
   `/change\/(.+)/`. On a match, inspect the branch's commits from its merge
   base with `origin`'s default branch through `HEAD`, including commit diffs
   and the aggregate diff. Follow this repository's existing comparison
   convention; if the base cannot be established unambiguously, ask the user.
3. Compare the committed code changes with the document's requirements. Read
   relevant code, tests, and contracts to understand observable behavior.
   Account for every code change in the branch commits, consolidating repeated
   edits into the resulting requirements. Do not rely only on commit messages
   or present reverted intermediate behavior as a current requirement.
   Do not treat uncommitted changes as branch commits.
4. Add missing requirements supported by the commits. Preserve existing planned
   requirements that have not been implemented; absence from the commits alone
   is not a contradiction. When the branch does not match, work from the change
   file and the user's supplied requirements without branch reconciliation.
5. Resolve every ambiguous instruction and every conflict between the change
   file, user instructions, and branch commits with the user. State the exact
   conflict or missing decision, ask a concrete clarification question, and
   wait for the answer before rewriting the affected requirements. Neither
   the commits nor the existing document automatically wins. If the file is
   empty and the available evidence does not establish the intended change,
   ask what it should specify. Ask, do not guess.

## Format and finish

1. Use `agent/templates/change-template.md` for the document's structure.
   Replace placeholders, group requirements by responsibility, and keep the
   result as compact as possible without losing scope, behavior, edge cases,
   verification, or observable acceptance criteria. Describe each intentional
   compatibility difference with its reason, before/after behavior, and a
   verification scenario. Reference `AGENTS.md` for general development,
   testing, and completion rules rather than duplicating them.
2. Save the document at `<change-path>`. Preserve unrelated user edits and
   do not change implementation files to resolve document conflicts.
3. Re-read the saved file and verify that it follows the template, accounts
   for all committed code changes on a matching branch, and has no unresolved
   ambiguities or conflicts.
4. When finished, identify `<change-path>` and display the complete saved file
   contents in the final response, not just a summary or a link.

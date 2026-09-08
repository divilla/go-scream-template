#!/usr/bin/env python3
"""Enforce the repository's strict per-package statement coverage threshold."""

import sys
from collections import defaultdict
from pathlib import Path


def package_coverage(profile):
    lines = profile.splitlines()
    if not lines or lines[0] not in {"mode: set", "mode: count", "mode: atomic"}:
        raise ValueError("missing or invalid coverage mode")
    blocks = {}
    for line in lines[1:]:
        location, statements, executions = line.rsplit(maxsplit=2)
        statements, executions = int(statements), int(executions)
        if statements < 0 or executions < 0:
            raise ValueError("negative coverage count")
        previous = blocks.get(location)
        if previous and previous[0] != statements:
            raise ValueError(f"inconsistent block size: {location}")
        # Cross-package instrumentation can report a block in several profiles.
        blocks[location] = (statements, executions > 0 or bool(previous and previous[1]))
    packages = defaultdict(lambda: [0, 0])
    for location, (statements, executed) in blocks.items():
        filename, _ = location.rsplit(":", 1)
        package = filename.rsplit("/", 1)[0]
        packages[package][0] += statements
        packages[package][1] += statements if executed else 0
    result = {package: counts for package, counts in packages.items() if counts[0]}
    if not result:
        raise ValueError("coverage profile contains no statements")
    return result


def check(profile, output):
    failed = False
    for package, (total, covered) in sorted(package_coverage(profile).items()):
        passed = covered * 100 > total * 95
        print(f"{'PASS' if passed else 'FAIL'} {package}: {covered / total:.2%} ({covered}/{total})", file=output)
        failed |= not passed
    return not failed


def main():
    try:
        passed = check(Path(sys.argv[1]).read_text(), sys.stdout)
    except (IndexError, OSError, ValueError) as error:
        print(f"Coverage check failed: {error}", file=sys.stderr)
        return 1
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())

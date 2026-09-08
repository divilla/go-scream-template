#!/usr/bin/env python3
"""Enforce the repository's strict per-package statement coverage threshold."""

import re
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


def handwritten_profile(profile, root):
    # Validate before filtering so malformed excluded entries cannot hide errors.
    package_coverage(profile)
    module = re.search(r"^module\s+(\S+)", (root / "go.mod").read_text(), re.MULTILINE)
    if module is None:
        raise ValueError("missing module declaration")
    prefix = module[1] + "/"
    lines = profile.splitlines()
    filtered = [lines[0]]
    for line in lines[1:]:
        location = line.rsplit(maxsplit=2)[0]
        filename = location.rsplit(":", 1)[0]
        if not filename.startswith(prefix):
            raise ValueError(f"unexpected dependency in service profile: {filename}")
        relative = Path(filename[len(prefix):])
        if relative.is_absolute() or ".." in relative.parts or relative.suffix != ".go":
            raise ValueError(f"invalid source path: {filename}")
        source = (root / relative).read_text()
        header = source.split("\npackage ", 1)[0]
        if re.search(r"^// (?:Package \w+ )?Code generated .* DO NOT EDIT\.?$", header, re.MULTILINE):
            continue
        if relative.name.endswith("_test.go") or relative.parts[0] == "int-tests":
            raise ValueError(f"test code in service profile: {filename}")
        filtered.append(line)
    result = "\n".join(filtered) + "\n"
    package_coverage(result)
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
        if len(sys.argv) < 2:
            raise ValueError("missing unit coverage profiles")
        root = Path(__file__).resolve().parent.parent
        passed = True
        for filename in sys.argv[1:]:
            path = Path(filename)
            profile = handwritten_profile(path.read_text(), root)
            print(f"Unit coverage: {filename}")
            passed = check(profile, sys.stdout) and passed
            path.write_text(profile)
    except (OSError, ValueError) as error:
        print(f"Coverage check failed: {error}", file=sys.stderr)
        return 1
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())

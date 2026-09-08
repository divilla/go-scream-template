#!/usr/bin/env python3
"""Filter generated files and enforce integration-only service statement coverage."""

from check_coverage import handwritten_profile, package_coverage


def check(profile, root, destination, output):
    filtered_profile = handwritten_profile(profile, root)
    packages = package_coverage(filtered_profile)
    destination.write_text(filtered_profile)
    total = covered = 0
    for package, (statements, executed) in sorted(packages.items()):
        print(f"{package}: {executed / statements:.2%} ({executed}/{statements})", file=output)
        total += statements
        covered += executed
    passed = covered * 100 > total * 90
    print(f"{'PASS' if passed else 'FAIL'} integration coverage: {covered / total:.2%} "
          f"({covered}/{total}); required >90%", file=output)
    return passed

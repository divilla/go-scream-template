"""Exercise the statistics script's actual AWK report with known Git line counts."""

import pathlib
import subprocess
import tempfile
import unittest


class BranchStatsTests(unittest.TestCase):
    def test_renames_with_real_git_and_cloc(self):
        script = pathlib.Path(__file__).resolve().parents[1] / (
            ".codex/skills/branch-stats/scripts/branch-stats.sh"
        )
        cases = [
            ("pkg/unchanged.go", "pkg/unchanged.go", "Production application code", True),
            ("pkg/old.go", "pkg/new.go", "Production application code", True),
            ("pkg/old/file.go", "pkg/new/file.go", "Production application code", True),
            ("old.go", "pkg/new.go", "Production application code", True),
            ("pkg/old.go", "internal/new_test.go", "Unit tests", True),
            ("pkg/old name.go", "pkg/new name.go", "Production application code", True),
            ("pkg/old.go", "pkg/new.go", "Production application code", False),
        ]
        for old, new, category, edit in cases:
            with self.subTest(old=old, new=new, edit=edit), tempfile.TemporaryDirectory() as directory:
                def git(*args):
                    return subprocess.check_output(
                        ["git", "-C", directory, *args], text=True,
                    ).strip()

                git("init", "-q")
                git("config", "user.name", "Test")
                git("config", "user.email", "test@example.com")
                git("config", "commit.gpgsign", "false")
                old_file = pathlib.Path(directory) / old
                old_file.parent.mkdir(parents=True, exist_ok=True)
                source = "package example\n// old comment\nfunc one() {}\nfunc two() {}\nfunc three() {}\n"
                old_file.write_text(source)
                git("add", ".")
                git("commit", "-qm", "base")
                base = git("rev-parse", "HEAD")
                new_file = pathlib.Path(directory) / new
                new_file.parent.mkdir(parents=True, exist_ok=True)
                old_file.rename(new_file)
                if edit:
                    new_file.write_text(source.replace("old comment", "new comment"))
                git("add", ".")
                git("commit", "-qm", "rename")
                result = subprocess.run(
                    ["bash", str(script), "--base", base], cwd=directory,
                    check=True, capture_output=True, text=True,
                )
                changed = int(edit)
                self.assertIn(
                    f"| {category} | 1 | 0 | {changed} | 0 | {changed} | {2 * changed} | {2 * changed} | +0 |",
                    result.stdout,
                )
                self.assertIn(
                    f"| **Entire branch** | **1** | **0** | **{changed}** | **0** | **{changed}** | **{2 * changed}** | **{2 * changed}** | **+0** |",
                    result.stdout,
                )

    def test_categories_and_totals(self):
        script = pathlib.Path(__file__).resolve().parents[1] / (
            ".codex/skills/branch-stats/scripts/branch-stats.sh"
        )
        report = script.read_text().split("awk -F '\\t' '\n", 1)[1].rsplit(
            "' \"$comments_file\" \"$numstat_file\"", 1
        )[0]
        categories = {
            "Production application code": [
                "cmd/app/main.go", "config/config.go", "internal/app/app.go",
                "pkg/httpserver/server.go",
            ],
            "Unit tests": [
                "config/config_test.go", "internal/app/app_test.go",
                "scripts/check_coverage_test.py", "scripts/commit_test.pl",
                "scripts/codex-review-loop_unit_test.pl", "scripts/makefile_test.sh",
                "scripts/test.sh", ".codex/skills/example/scripts/tool_test.sh",
                "scripts/tool.test.py", "scripts/tool.spec.sh",
            ],
            "Integration tests and fixtures": [
                "int-tests/user_test.go", "int-tests/Dockerfile",
                "int-tests/fixtures/data.json", "int-tests/scenario.yaml",
            ],
            "Developer tooling": [
                "Makefile", "scripts/codex-code-spec.pl", "scripts/codex-review-loop.pl",
                "scripts/check_coverage.py", "scripts/create-change-branch.sh",
                "scripts/lib/APIHydra/Progress.pm", "scripts/report.awk",
                ".codex/skills/branch-stats/scripts/branch-stats.sh",
            ],
            "Skeleton - production-shaped": ["skeleton/internal/app/app.go"],
            "Skeleton - unit-test-shaped": ["skeleton/internal/app/app_test.go"],
            "Documentation, specifications, and logs": [
                "README.md", "scripts/README.md", ".codex/skills/branch-stats/SKILL.md",
                "agent/changes/001-echo-framework-and-test-coverage.md",
                "implementation-log.md",
            ],
            "Dependency metadata": ["go.mod", "go.sum"],
        }
        # Unique weights ensure that swapped/misclassified paths cannot hide in totals.
        counts = {}
        expected = {}
        for label, paths in categories.items():
            added = deleted = 0
            for path in paths:
                weight = 2 ** (len(counts) + 1)
                counts[path] = (weight, weight // 2)
                added += weight
                deleted += weight // 2
            expected[label] = [len(paths), added - len(paths), len(paths),
                               deleted - len(paths), len(paths), 2 * len(paths),
                               added + deleted, added - deleted]

        with tempfile.TemporaryDirectory() as directory:
            comments = pathlib.Path(directory) / "comments.tsv"
            numstat = pathlib.Path(directory) / "numstat.tsv"
            comments.write_text("".join(f"{path}\t1\t1\n" for path in counts))
            numstat.write_text("".join(
                f"{added}\t{deleted}\t{path}\n"
                for path, (added, deleted) in counts.items()
            ))
            result = subprocess.run(
                ["awk", "-F", "\t", report, str(comments), str(numstat)],
                check=True, capture_output=True, text=True,
            )

        rows = {}
        for line in result.stdout.splitlines():
            if line.startswith("| ") and not line.startswith("| Category"):
                cells = [cell.strip().replace("**", "") for cell in line.split("|")[1:-1]]
                rows[cells[0]] = [int(cell) for cell in cells[1:]]
        for label, values in expected.items():
            with self.subTest(category=label):
                self.assertEqual(rows[label], values)
        self.assertEqual(rows["Entire branch"], [sum(column) for column in zip(*expected.values())])

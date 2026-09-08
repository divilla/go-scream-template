import contextlib
import io
import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from run_int_tests import run


class IntegrationWorkflowTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        (self.root / "go.mod").write_text("module example/app\n")
        (self.root / "main.go").write_text("package main\n")
        self.docker = ["docker"]
        self.compose = [*self.docker, "compose", "-f", "custom.yml", "-p", "custom"]
        self.calls = []
        self.fail = None
        self.shutdown_code = 0
        self.running = True
        self.missing = None
        self.profile = "mode: atomic\nexample/app/main.go:1.1,2.1 1 1\n"
        self.startup_error = None
        self.report = self.root / ".coverage" / "integration"

    def command(self, args, **kwargs):
        self.calls.append(args)
        self.assertEqual(self.root, kwargs["cwd"])
        self.assertTrue(kwargs["check"])
        raw = Path(kwargs["env"]["INT_COVERAGE_DIR"])
        self.assertEqual(self.report / "raw", raw)
        if args[:len(self.compose)] == self.compose:
            operation = args[len(self.compose)]
            if operation == self.fail:
                raise subprocess.CalledProcessError(7, args)
            if operation == "up":
                self.assertEqual([], list(raw.iterdir()))
                for filename in ["covmeta.test", "covcounters.test"]:
                    if filename != self.missing:
                        (raw / filename).touch()
            if operation == "ps":
                return subprocess.CompletedProcess(args, 0, "app-id\n")
        if args[:len(self.docker) + 1] == [*self.docker, "inspect"]:
            stopped = any("stop" in call for call in self.calls)
            return subprocess.CompletedProcess(args, 0, json.dumps({
                "Running": self.running and not stopped,
                "ExitCode": self.shutdown_code if stopped else 0, "OOMKilled": False}))
        if args[:4] == ["go", "tool", "covdata", "textfmt"]:
            if self.fail == "convert":
                raise subprocess.CalledProcessError(1, args)
            Path(args[-1].removeprefix("-o=")).write_text(self.profile)
        return subprocess.CompletedProcess(args, 0, "")

    def run_suite(self):
        with patch("run_int_tests.subprocess.run", side_effect=self.command), \
                patch("run_int_tests.run_scenarios", side_effect=self.startup_error) as scenarios, \
                contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            result = run(self.compose, self.root)
            if scenarios.called:
                self.assertEqual(self.compose, scenarios.call_args.args[0])
                self.assertEqual(self.docker, scenarios.call_args.args[1])
            return result

    def test_success_orders_shutdown_collection_and_cleanup(self):
        self.assertEqual(0, self.run_suite())
        operations = [call[len(self.compose)] for call in self.calls if call[:len(self.compose)] == self.compose]
        self.assertEqual(["up", "wait", "ps", "stop", "logs", "down"], operations)
        stop = self.calls.index([*self.compose, "stop", "--timeout", "30", "app"])
        conversion = next(i for i, call in enumerate(self.calls) if call[:3] == ["go", "tool", "covdata"])
        self.assertLess(stop, conversion)
        self.assertLess(conversion, len(self.calls) - 2)
        self.assertIn("PASS", (self.report / "summary.txt").read_text())

    def test_endpoint_overrides_reach_both_inspections_and_startup_scenarios(self):
        for docker in [
            ["docker", "--context", "test"],
            ["docker", "-c", "compose", "--config", "compose"],
            ["docker", "--context=compose"],
            ["/custom path/docker", "--host=tcp://test:2376", "--tlsverify",
             "--tlscacert", "/cert path/ca.pem"],
        ]:
            with self.subTest(docker=docker):
                self.calls.clear()
                self.docker = docker
                self.compose = [*docker, "compose", "-f", "custom file.yml", "-p", "custom"]
                self.assertEqual(0, self.run_suite())
                inspection = [*docker, "inspect", "--format", "{{json .State}}", "app-id"]
                self.assertEqual(2, self.calls.count(inspection))
                self.assertEqual([*self.compose, "down", "--remove-orphans"], self.calls[-1])

    def test_invalid_stack_fails_before_startup_and_attempts_cleanup(self):
        self.compose = ["docker", "--context", "compose"]
        self.assertEqual(1, self.run_suite())
        self.assertEqual([[*self.compose, "logs", "--no-color"],
                          [*self.compose, "down", "--remove-orphans"]], self.calls)

    def test_fresh_run_discards_stale_integration_data_and_preserves_unit_data(self):
        (self.report / "raw").mkdir(parents=True)
        (self.report / "raw" / "covcounters.stale").touch()
        (self.report / "coverage.txt").write_text("stale")
        (self.root / "coverage.txt").write_text("unit data")
        self.assertEqual(0, self.run_suite())
        self.assertEqual("unit data", (self.root / "coverage.txt").read_text())
        self.assertNotIn("stale", (self.report / "coverage.txt").read_text())

    def test_test_failure_still_collects_coverage_and_cleans_up(self):
        self.fail = "wait"
        self.assertEqual(1, self.run_suite())
        self.assertTrue((self.report / "coverage.txt").exists())
        self.assertEqual([*self.compose, "down", "--remove-orphans"], self.calls[-1])

    def test_failures_cannot_be_overwritten_by_cleanup_success(self):
        for operation in ["up", "stop", "convert", "logs", "down"]:
            with self.subTest(operation=operation):
                self.calls.clear()
                self.fail = operation
                self.assertEqual(1, self.run_suite())
                self.assertEqual([*self.compose, "down", "--remove-orphans"], self.calls[-1])

    def test_shutdown_timeout_or_early_exit_fails(self):
        self.shutdown_code = 137
        self.assertEqual(1, self.run_suite())
        self.assertFalse((self.report / "coverage.txt").exists())
        self.calls.clear()
        self.running = False
        self.assertEqual(1, self.run_suite())
        self.assertFalse(any("stop" in call for call in self.calls))

    def test_missing_or_invalid_coverage_fails(self):
        for missing in ["covmeta.test", "covcounters.test"]:
            with self.subTest(missing=missing):
                self.calls.clear()
                self.missing = missing
                self.assertEqual(1, self.run_suite())
        self.missing = None
        self.calls.clear()
        self.profile = "invalid"
        self.assertEqual(1, self.run_suite())

    def test_low_coverage_fails_but_keeps_report(self):
        self.profile = "mode: atomic\nexample/app/main.go:1.1,2.1 10 0\n"
        self.assertEqual(1, self.run_suite())
        self.assertIn("FAIL", (self.report / "summary.txt").read_text())
        self.assertTrue((self.report / "coverage.txt").exists())

    def test_startup_failure_still_reports_coverage_and_fails(self):
        self.startup_error = ValueError("wrong startup diagnostic")
        self.assertEqual(1, self.run_suite())
        self.assertTrue((self.report / "coverage.txt").exists())
        self.assertEqual([*self.compose, "down", "--remove-orphans"], self.calls[-1])

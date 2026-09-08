import contextlib
import io
import subprocess
import unittest

from int_startup_scenarios import run_scenarios


class StartupScenariosTests(unittest.TestCase):
    def setUp(self):
        self.docker = ["docker"]
        self.compose = [*self.docker, "compose"]
        self.calls = []
        self.status = 0
        self.logs = ""
        self.failure = None

    def command(self, args, **kwargs):
        self.calls.append(args)
        output, errors = "", ""
        if args[:len(self.compose) + 1] == [*self.compose, "run"]:
            environment = [args[i + 1] for i, arg in enumerate(args) if arg == "-e"]
            expectations = {
                "PG_POOL_MAX=invalid": (1, "Config error:"),
                "PG_URL=": (1, "environment variable not declared: PG_URL"),
                "GRPC_PORT=invalid": (0, "grpcServer.Notify"),
                "HTTP_PORT=invalid": (0, "httpServer.Notify"),
                "NATS_RPC_SERVER=invalid subject": (0, "natsServer.Notify"),
                "RMQ_URL=invalid": (1, "rmqServer - server.New"),
                "NATS_URL=nats://127.0.0.1:1": (1, "natsServer - server.New"),
            }
            self.status, self.logs = expectations[environment[0]]
            output = "scenario-id\n" if self.failure != "missing" else ""
        elif args[:len(self.docker) + 1] == [*self.docker, "wait"]:
            self.assertEqual(75, kwargs["timeout"])
            if self.failure == "timeout":
                raise subprocess.TimeoutExpired(args, 75)
            output = str(self.status if self.failure != "exit" else 99)
        elif args[:len(self.docker) + 1] == [*self.docker, "logs"]:
            # Go's log.Fatalf writes stderr; zerolog normally writes stdout.
            errors = self.logs if self.failure != "diagnostic" else "unrelated error"
        return subprocess.CompletedProcess(args, 0, output, errors)

    def test_scenarios_validate_exit_and_diagnostic_and_remove_containers(self):
        with contextlib.redirect_stdout(io.StringIO()):
            run_scenarios(self.compose, self.docker, self.command)
        self.assertEqual(7, self.calls.count(["docker", "rm", "-f", "scenario-id"]))

    def test_wrong_exit_or_diagnostic_fails_with_cleanup(self):
        for failure in ["exit", "diagnostic"]:
            with self.subTest(failure=failure):
                self.failure = failure
                with self.assertRaises(ValueError):
                    run_scenarios(self.compose, self.docker, self.command)
                self.assertEqual(["docker", "rm", "-f", "scenario-id"], self.calls[-1])

    def test_timeout_fails_with_cleanup(self):
        self.failure = "timeout"
        with self.assertRaises(subprocess.TimeoutExpired):
            run_scenarios(self.compose, self.docker, self.command)
        self.assertEqual(["docker", "rm", "-f", "scenario-id"], self.calls[-1])

    def test_missing_container_fails(self):
        self.failure = "missing"
        with self.assertRaises(ValueError):
            run_scenarios(self.compose, self.docker, self.command)
        self.assertEqual(1, len(self.calls))

    def test_endpoint_overrides_reach_wait_logs_and_cleanup_even_on_failure(self):
        for docker in [
            ["docker", "--context", "test"],
            ["/custom path/docker", "-H", "tcp://test:2376", "--tlsverify",
             "--tlscacert", "/cert path/ca.pem"],
        ]:
            for failure in [None, "timeout", "diagnostic"]:
                with self.subTest(docker=docker, failure=failure):
                    self.calls.clear()
                    self.docker = docker
                    self.compose = [*docker, "compose", "-f", "custom file.yml", "-p", "custom"]
                    self.failure = failure
                    if failure:
                        error = subprocess.TimeoutExpired if failure == "timeout" else ValueError
                        with self.assertRaises(error):
                            run_scenarios(self.compose, self.docker, self.command)
                    else:
                        with contextlib.redirect_stdout(io.StringIO()):
                            run_scenarios(self.compose, self.docker, self.command)
                    count = 1 if failure else 7
                    self.assertEqual(count, self.calls.count([*docker, "wait", "scenario-id"]))
                    self.assertEqual(0 if failure == "timeout" else count,
                                     self.calls.count([*docker, "logs", "scenario-id"]))
                    self.assertEqual(count, self.calls.count([*docker, "rm", "-f", "scenario-id"]))
                    self.assertEqual([*docker, "rm", "-f", "scenario-id"], self.calls[-1])

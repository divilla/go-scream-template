#!/usr/bin/env python3
"""Run the Docker suite, flush service counters, and enforce its coverage gate."""

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

from check_int_coverage import check
from int_startup_scenarios import run_scenarios


def docker_prefix(compose):
    # Skip option values so a context or config directory named "compose" works.
    value_options = {"--config", "-c", "--context", "-H", "--host", "-l", "--log-level",
                     "--tlscacert", "--tlscert", "--tlskey"}
    index = 1
    while index < len(compose):
        if compose[index] == "compose":
            return compose[:index]
        index += 2 if compose[index] in value_options else 1
    raise ValueError("integration stack must contain a Docker compose command")


def run(compose, root):
    report = root / ".coverage" / "integration"
    # This fixed, ignored directory belongs exclusively to this integration run.
    if report.exists():
        shutil.rmtree(report)
    raw = report / "raw"
    raw.mkdir(parents=True)
    env = dict(os.environ, INT_COVERAGE_DIR=str(raw.resolve()))

    def command(args, capture=False, timeout=None):
        return subprocess.run(args, cwd=root, env=env, check=True, text=True,
                              timeout=timeout, stdout=subprocess.PIPE if capture else None,
                              stderr=subprocess.PIPE if capture else None)

    failed = False
    try:
        # Keep Docker's global options, but exclude Compose's files/project flags.
        docker = docker_prefix(compose)
        command([*compose, "up", "--build", "-d", "db", "rabbitmq", "nats", "jaeger", "app", "int-tests"])
        try:
            command([*compose, "wait", "int-tests"])
        except subprocess.CalledProcessError:
            failed = True
        app = command([*compose, "ps", "-aq", "app"], capture=True).stdout.strip()
        if not app:
            raise ValueError("application container is missing")
        state = json.loads(command([*docker, "inspect", "--format", "{{json .State}}", app], capture=True).stdout)
        if not state["Running"]:
            raise ValueError("application exited before graceful shutdown")
        command([*compose, "stop", "--timeout", "30", "app"])
        state = json.loads(command([*docker, "inspect", "--format", "{{json .State}}", app], capture=True).stdout)
        if state["Running"] or state["ExitCode"] != 0 or state["OOMKilled"]:
            raise ValueError(f"application did not shut down successfully: {state}")
        if not any(raw.glob("covmeta.*")) or not any(raw.glob("covcounters.*")):
            raise ValueError("missing integration coverage metadata or counters")
        try:
            run_scenarios(compose, docker, command)
        except (OSError, ValueError, subprocess.SubprocessError) as error:
            print(f"Startup scenario failed: {error}", file=sys.stderr)
            failed = True
        profile = report / "service.txt"
        command(["go", "tool", "covdata", "textfmt", "-i=" + str(raw), "-o=" + str(profile)])
        with (report / "summary.txt").open("w") as summary:
            passed = check(profile.read_text(), root, report / "coverage.txt", summary)
        print((report / "summary.txt").read_text(), end="")
        failed |= not passed
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        print(f"Integration tests failed: {error}", file=sys.stderr)
        failed = True
    finally:
        for args in ([*compose, "logs", "--no-color"], [*compose, "down", "--remove-orphans"]):
            try:
                command(args)
            except (OSError, subprocess.CalledProcessError) as error:
                print(f"Integration cleanup failed: {error}", file=sys.stderr)
                failed = True
    return int(failed)


if __name__ == "__main__":
    sys.exit(run(sys.argv[1:], Path(__file__).resolve().parent.parent))

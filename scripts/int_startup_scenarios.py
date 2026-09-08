"""Exercise observable startup failures using the same instrumented service image."""


def run_scenarios(compose, docker, command):
    scenarios = [
        ("invalid-config", ["PG_POOL_MAX=invalid"], 1, "Config error:"),
        ("missing-database", ["PG_URL="], 1, "environment variable not declared: PG_URL"),
        ("grpc-listener", ["GRPC_PORT=invalid", "LOG_LEVEL=info", "TRACING_ENABLED=false"], 0, "grpcServer.Notify"),
        ("http-listener", ["HTTP_PORT=invalid", "LOG_LEVEL=warn", "TRACING_ENABLED=false"], 0, "httpServer.Notify"),
        ("nats-subscription", ["NATS_RPC_SERVER=invalid subject", "LOG_LEVEL=error", "TRACING_ENABLED=false"], 0, "natsServer.Notify"),
        ("rabbitmq-connection", ["RMQ_URL=invalid", "LOG_LEVEL=unknown", "TRACING_ENABLED=false"], 1, "rmqServer - server.New"),
        ("nats-connection", ["NATS_URL=nats://127.0.0.1:1", "TRACING_ENABLED=false"], 1, "natsServer - server.New"),
    ]
    for name, environment, expected, diagnostic in scenarios:
        args = [*compose, "run", "-d", "--no-deps"]
        for value in environment:
            args.extend(["-e", value])
        container = command([*args, "app"], capture=True).stdout.strip()
        if not container:
            raise ValueError(f"{name}: missing scenario container")
        try:
            # RabbitMQ startup retries ten times with a five-second delay.
            result = command([*docker, "wait", container], capture=True, timeout=75)
            captured = command([*docker, "logs", container], capture=True)
            logs = captured.stdout + (captured.stderr or "")
            if int(result.stdout.strip()) != expected or diagnostic not in logs:
                raise ValueError(f"{name}: expected exit {expected} and {diagnostic!r}; got {result.stdout!r}\n{logs}")
            print(f"PASS startup scenario: {name}", flush=True)
        finally:
            command([*docker, "rm", "-f", container])

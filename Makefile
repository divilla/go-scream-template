ifneq ($(wildcard .env),)
include .env
export
else
$(warning WARNING: .env file not found! Using .env.example)
include .env.example
export
endif

BASE_STACK = docker compose -f docker-compose.yml
INTEGRATION_TEST_STACK = $(BASE_STACK) -f docker-compose-integration-test.yml
ALL_STACK = $(INTEGRATION_TEST_STACK)

# HELP =================================================================================================================
# This will output the help for each task
# thanks to https://marmelab.com/blog/2016/02/29/auto-documented-makefile.html
.PHONY: help

help: ## Display this help screen
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

# Init

.PHONY: init bin-deps pre-commit

# Finish generators and formatters before checks read the source, even with -j.
.NOTPARALLEL: check pre-commit run

init: bin-deps ## Install Go development tools

bin-deps: ## Install module-managed Go tools
	go install tool
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate

pre-commit: deps swag-v1 proto-v1 mock format linter-golangci test ### run pre-commit

compose-up: ### Run docker compose (without backend and reverse proxy)
	$(BASE_STACK) up --build -d db rabbitmq nats && $(BASE_STACK) logs -f
.PHONY: compose-up

compose-up-all: ### Run docker compose (with backend and reverse proxy)
	$(BASE_STACK) up --build -d
.PHONY: compose-up-all

compose-up-integration-test: ### Run docker compose with integration test
	$(INTEGRATION_TEST_STACK) up --build --abort-on-container-exit --exit-code-from integration-test; exit_code=$$?; \
	$(INTEGRATION_TEST_STACK) down --remove-orphans; exit $$exit_code
.PHONY: compose-up-integration-test

compose-down: ### Down docker compose
	$(ALL_STACK) down --remove-orphans
.PHONY: compose-down

swag-v1: ### swag init
	swag init --parseDependency -d internal/controller/restapi,internal/entity -g router.go
.PHONY: swag-v1

proto-v1: ### generate source files from proto
	protoc --go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		docs/proto/v1/*.proto
.PHONY: proto-v1

deps: ### deps tidy + verify
	go mod tidy && go mod verify
.PHONY: deps

deps-audit: ### check dependencies vulnerabilities
	govulncheck ./...
.PHONY: deps-audit

run: deps swag-v1 proto-v1 ### swag run for API v1
	go mod download && \
	CGO_ENABLED=0 go run -tags migrate ./cmd/app
.PHONY: run

docker-rm-volume: ## Remove this Compose project's database volume (requires python3)
	@config=$$($(BASE_STACK) config --format json) && \
	volume=$$(printf '%s\n' "$$config" | python3 -c 'import json, sys; print(json.load(sys.stdin)["volumes"]["db_data"]["name"])') && \
	docker volume rm "$$volume"
.PHONY: docker-rm-volume

check: mock format lint test integration-test ## Check application code and integration tests
.PHONY: check

# Format & Lint

.PHONY: fix-diff format lint lint-go lint-docker lint-env linter-golangci

fix-diff: ## Preview code changes suggested by go fix
	go fix -diff ./...

format: ### Run code formatter
	@go fix ./...
	@go tool golangci-lint fmt

lint: lint-go lint-docker lint-env ## Run all lint checks

lint-go: linter-golangci ## Lint Go code

linter-golangci: ## Lint Go code
	go tool golangci-lint run

lint-docker: ## Lint Dockerfiles
	hadolint Dockerfile integration-test/Dockerfile

lint-env: ## Lint environment files
	dotenv-linter check .

# Tests

empty :=
space := $(empty) $(empty)
comma := ,

UNIT_PACKAGES := ./internal/... ./pkg/... ./config/... ./cmd/...
COVERAGE_FILE := coverage.txt

.PHONY: test test-makefile mock unit-test coverage race benchmark integration-test

test: unit-test race coverage ## Run unit tests with race detection and coverage

test-makefile: ## Test Makefile workflow ordering and isolation
	bash scripts/makefile_workflow_test.sh

test-branch-stats: ## Test branch statistics categories and totals
	python3 -B -m unittest discover -s scripts -p branch_stats_test.py
.PHONY: test-branch-stats

mock: ### run mockgen
	go tool mockgen -source ./internal/repo/contracts.go -package usecase_test -destination ./internal/usecase/mocks_repo_test.go
	go tool mockgen -source ./internal/usecase/contracts.go -package usecase_test -destination ./internal/usecase/mocks_usecase_test.go

unit-test: mock test-coverage ## Run unit tests with race detection and coverage
	go test -v -race -covermode=atomic \
			-coverpkg=$(subst $(space),$(comma),$(strip $(UNIT_PACKAGES))) \
			-coverprofile=$(COVERAGE_FILE) $(UNIT_PACKAGES)

test-coverage: ## Test the per-package coverage checker
	python3 -B -m unittest discover -s scripts -p check_coverage_test.py
.PHONY: test-coverage

coverage: unit-test ## Run tests and enforce coverage above 95% in every production package
	go tool cover -func=$(COVERAGE_FILE)
	python3 scripts/check_coverage.py $(COVERAGE_FILE)

race: unit-test ## Alias for unit tests with race detection

benchmark: ## Run benchmarks without ordinary tests
	go test -run='^$$' -bench=. -benchmem $(UNIT_PACKAGES)

integration-test: compose-up-integration-test ## Run integration tests in Docker

# Migrate

migrate-create: ## Create a migration pair: make migrate-create NAME=create_table
	@if [ -z "$$NAME" ]; then \
		printf '%s\n' 'Usage: make migrate-create NAME=<name>' >&2; exit 1; \
	fi; \
	migrate create -ext sql -dir migrations -- "$$NAME"
.PHONY: migrate-create

migrate-up: ### migration up
	migrate -path migrations -database '$(PG_URL)?sslmode=disable' up
.PHONY: migrate-up

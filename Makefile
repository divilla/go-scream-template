ifneq ($(wildcard .env),)
include .env
else
$(warning WARNING: .env file not found! Using .env.example)
include .env.example
endif
export

# Expand for each recipe command to print a blank line before Make echoes it.
.SHELLFLAGS = $(info )-c

BASE_STACK = docker compose -f docker-compose.yml
INT_TESTS_STACK = $(BASE_STACK) -f docker-compose-int-tests.yml
ALL_STACK = $(INT_TESTS_STACK)

.PHONY: help

help: ## Display this help screen
	awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: init bin-deps compose-up compose-up-all compose-up-int-tests compose-down \
        swag-v1 proto-v1 deps deps-audit run docker-rm-volume check check-all

# Finish generators and formatters before checks read the source, even with -j.
.NOTPARALLEL: check check-all run

init: bin-deps ## Install Go development tools

bin-deps: ## Install module-managed Go tools
	go install tool
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate

compose-up: ## Run docker compose (without backend and reverse proxy)
	$(BASE_STACK) up --build -d db rabbitmq nats && $(BASE_STACK) logs -f

compose-up-all: ## Run docker compose (with backend and reverse proxy)
	$(BASE_STACK) up --build -d

compose-up-int-tests: int-tests ## Alias for integration tests with coverage enforcement

compose-down: ## Down docker compose
	$(ALL_STACK) down --remove-orphans

swag-v1: ## swag init
	swag init --parseDependency -d internal/controller/restapi,internal/entity -g router.go

proto-v1: ## generate source files from proto
	protoc --go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		docs/proto/v1/*.proto

deps: ## deps tidy + verify
	go mod tidy && go mod verify

deps-audit: ## check dependencies vulnerabilities
	govulncheck ./...

run: deps swag-v1 proto-v1 ## swag run for API v1
	go mod download && \
	CGO_ENABLED=0 go run -tags migrate ./cmd/app

docker-rm-volume: ## Remove this Compose project's database volume (requires python3)
	config=$$($(BASE_STACK) config --format json) && \
	volume=$$(printf '%s\n' "$$config" | python3 -c 'import json, sys; print(json.load(sys.stdin)["volumes"]["db_data"]["name"])') && \
	docker volume rm "$$volume"

check: deps swag-v1 proto-v1 mock format lint test ## Tidy, verify, generate, format, lint, and unit-tests

check-all: check int-tests ## Run check followed by Docker integration tests

##@ Format & Lint

.PHONY: fix-diff format lint lint-go lint-docker lint-env linter-golangci

fix-diff: ## Preview code changes suggested by go fix
	go fix -diff ./...

format: ## Run code formatter
	go fix ./...
	go tool golangci-lint fmt

lint: lint-go lint-docker lint-env ## Run all lint checks

lint-go: linter-golangci ## Lint Go code

linter-golangci: ## Lint Go code
	go tool golangci-lint run

lint-docker: ## Lint Dockerfiles
	hadolint Dockerfile int-tests/Dockerfile

lint-env: ## Lint environment files
	dotenv-linter check .

##@ Tests

empty :=
space := $(empty) $(empty)
comma := ,

UNIT_PACKAGES := ./internal/... ./pkg/... ./config/... ./cmd/...
COVERAGE_FILE := coverage.txt
MIGRATE_COVERAGE_FILE := .coverage/unit-migrate.txt

.PHONY: test test-makefile test-branch-stats test-coverage mock unit-tests coverage benchmark int-tests

test: coverage ## Run unit tests with race detection and coverage

test-makefile: ## Test Makefile workflow ordering and isolation
	bash scripts/makefile_workflow_test.sh

test-branch-stats: ## Test branch statistics categories and totals
	python3 -B -m unittest discover -s scripts -p branch_stats_test.py

mock: ## run mockgen
	go tool mockgen -source ./internal/repo/contracts.go -package usecase_test -destination ./internal/usecase/mocks_repo_test.go
	go tool mockgen -source ./internal/usecase/contracts.go -package usecase_test -destination ./internal/usecase/mocks_usecase_test.go

unit-tests: mock test-coverage ## Run unit tests with race detection and coverage
	mkdir -p .coverage
	go test -v -race -covermode=atomic \
			-coverpkg=$(subst $(space),$(comma),$(strip $(UNIT_PACKAGES))) \
			-coverprofile=$(COVERAGE_FILE) $(UNIT_PACKAGES)
	go test -v -race -tags migrate -covermode=atomic \
			-coverpkg=$(subst $(space),$(comma),$(strip $(UNIT_PACKAGES))) \
			-coverprofile=$(MIGRATE_COVERAGE_FILE) $(UNIT_PACKAGES)

int-tests: ## Run Docker integration tests and enforce >90% service statement coverage
	python3 -B scripts/run_int_tests.py $(INT_TESTS_STACK)

test-coverage: ## Test coverage gates and integration workflow
	python3 -B -m unittest discover -s scripts -p '*coverage_test.py'
	python3 -B -m unittest discover -s scripts -p run_int_tests_test.py
	python3 -B -m unittest discover -s scripts -p int_startup_scenarios_test.py

coverage: unit-tests ## Run tests and enforce coverage above 95% in every production package
	python3 scripts/check_coverage.py $(COVERAGE_FILE) $(MIGRATE_COVERAGE_FILE)
	go tool cover -func=$(COVERAGE_FILE)
	go tool cover -func=$(MIGRATE_COVERAGE_FILE)

benchmark: ## Run benchmarks without ordinary tests
	go test -run='^$$' -bench=. -benchmem $(UNIT_PACKAGES)

##@ Migrate

.PHONY: migrate-create migrate-up

migrate-create: ## Create a migration pair: make migrate-create NAME=create_table
	if [ -z "$$NAME" ]; then \
		printf '%s\n' 'Usage: make migrate-create NAME=<name>' >&2; exit 1; \
	fi; \
	migrate create -ext sql -dir migrations -- "$$NAME"

migrate-up: ## migration up
	migrate -path migrations -database '$(PG_URL)?sslmode=disable' up

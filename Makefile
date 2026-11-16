# go-migrate developer tasks. Every target runs with GOWORK=off so the module
# is verified exactly as consumers build it.
export GOWORK := off

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
COMPOSE := docker compose -f docker-compose.test.yml

PG_DSN    := postgres://postgres:pw@localhost:55432/postgres?sslmode=disable
MYSQL_DSN := root:pw@tcp(localhost:53306)/

.PHONY: all build test test-race test-integration cover bench fuzz lint vuln fmt check clean

all: check

build: ## Build the gomigrate binary (CGO-free)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/gomigrate ./cmd/gomigrate

test: ## Unit and SQLite tests
	go test ./... -count=1

test-race: ## Tests with the race detector (needs cgo)
	go test ./... -race -count=1

test-integration: ## Conformance suite against real PostgreSQL and MySQL
	$(COMPOSE) up -d --wait
	GOMIGRATE_TEST_POSTGRES_DSN='$(PG_DSN)' GOMIGRATE_TEST_MYSQL_DSN='$(MYSQL_DSN)' \
		go test ./... -race -count=1 -coverpkg=./... -coverprofile=coverage.out; \
		status=$$?; $(COMPOSE) down -v; exit $$status

cover: ## Coverage report across all packages
	go test ./... -count=1 -coverpkg=./... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

bench: ## Benchmarks with allocation counts
	go test ./... -run '^$$' -bench . -benchmem

fuzz: ## Fuzz the SQL splitter for 60s
	go test ./internal/script -run '^$$' -fuzz FuzzSplit -fuzztime 60s

lint:
	golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...

vuln:
	govulncheck ./...

fmt:
	gofmt -w . && go mod tidy

check: lint test vuln ## The pre-commit gate (plus test-race / test-integration in CI)

clean:
	rm -rf bin coverage.out

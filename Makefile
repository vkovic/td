# td — build, test and lint.
#
# Nothing here has to be installed first. The linter is fetched and cached by
# `go run` at the version pinned below, so a checkout, a fresh machine and CI
# all run the same one, and go.mod stays free of a tool the binary never
# imports.

GOLANGCI_VERSION ?= v2.13.2
GOLANGCI        ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

BIN := bin/td

.PHONY: all build install test race cover lint fmt fmt-check tidy-check clean

## all: what CI runs, and what to run before pushing.
all: fmt-check lint test

## build: put a td in ./bin.
build:
	go build -o $(BIN) ./cmd/td

## install: put a td on your PATH, in $(go env GOPATH)/bin.
install:
	go install ./cmd/td

## test: the whole suite.
test:
	go test ./...

## race: the whole suite under the race detector. The TUI runs a watcher
## goroutine and the epilogue runs off the event loop, so this is the run that
## has something to find.
race:
	go test -race ./...

## cover: the suite with a coverage profile, and the total.
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

## lint: vet and golangci-lint.
lint:
	go vet ./...
	$(GOLANGCI) run

## fmt: rewrite anything that is not gofmt-clean.
fmt:
	gofmt -l -w .

## fmt-check: fail if anything is not gofmt-clean, naming it. gofmt -l alone
## exits 0 whatever it prints, so CI needs the test.
fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then echo "not gofmt-clean:"; echo "$$out"; exit 1; fi

## tidy-check: fail if go.mod or go.sum would change under go mod tidy.
tidy-check:
	@cp go.mod go.mod.bak && cp go.sum go.sum.bak; \
	go mod tidy; \
	status=0; \
	if ! cmp -s go.mod go.mod.bak || ! cmp -s go.sum go.sum.bak; then \
		echo "go.mod or go.sum is not tidy; run go mod tidy"; status=1; \
	fi; \
	mv go.mod.bak go.mod && mv go.sum.bak go.sum; \
	exit $$status

## clean: remove build and coverage output.
clean:
	rm -rf bin coverage.out

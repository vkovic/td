# td — build, test and lint.
#
# Nothing here has to be installed first. The linter is fetched and cached by
# `go run` at the version pinned below, so a checkout, a fresh machine and CI
# all run the same one, and go.mod stays free of a tool the binary never
# imports.

GOLANGCI_VERSION ?= v2.13.2
GOLANGCI        ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

BIN := bin/td

# PREFIX is where `make install` puts td. It defaults to ~/.local/bin rather
# than the $(go env GOPATH)/bin that `go install` obeys, because a checkout
# build is the whole point of this target and GOPATH/bin is often not even on
# a PATH.
PREFIX ?= $(HOME)/.local/bin

# VERSION is what the built binary reports as `td --version`. Without it a
# checkout build names itself by its commit, so an installed binary cannot say
# which release it is. git describe answers with the tag when HEAD carries one,
# falls back to the commit when it does not, and to dev outside a checkout —
# the empty string would be worse than any of them, because main.version treats
# anything other than "dev" as authoritative and would print nothing at all.
VERSION ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: all build install test race cover lint fmt fmt-check tidy-check clean

## all: what CI runs, and what to run before pushing.
all: fmt-check lint test

## build: put a td in ./bin, stamped with the version.
build:
	go build $(LDFLAGS) -o $(BIN) ./cmd/td

## install: put a td in $(PREFIX), stamped with the version, and say what
## landed there. It goes through go build rather than cp for a reason: go build
## writes a temp file and renames it into place, while copying over a binary
## where it lies invalidates the code signature macOS cached against that file,
## and every later run is killed on sight with no output at all.
install:
	@mkdir -p $(PREFIX)
	go build $(LDFLAGS) -o $(PREFIX)/td ./cmd/td
	@$(PREFIX)/td --version

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

BIN      ?= bin/helpdrift
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)

.PHONY: all build test vet fmt check demo clean

all: check build

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/helpdrift

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

# Fails if any file is not gofmt-clean, then vets and tests.
check:
	@test -z "$$(gofmt -s -l .)" || (echo "gofmt needed:"; gofmt -s -l .; exit 1)
	go vet ./...
	go test ./...

# Builds the binary and runs it against the demo fixture in a throwaway git repo.
demo: build
	./scripts/demo.sh $(BIN)

clean:
	rm -rf bin dist

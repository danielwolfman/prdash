VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

MODULE := github.com/danielwolfman/prdash
BINARY := prdash
MAIN := ./cmd/prdash
DIST := ./dist
LDFLAGS := -s -w -X $(MODULE)/internal/app.Version=$(VERSION) -X $(MODULE)/internal/app.Commit=$(COMMIT) -X $(MODULE)/internal/app.Date=$(DATE)

.PHONY: test build install snapshot clean

test:
	go test ./...

build:
	mkdir -p $(DIST)
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BINARY) $(MAIN)

install:
	go install -trimpath -ldflags '$(LDFLAGS)' $(MAIN)

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf $(DIST)

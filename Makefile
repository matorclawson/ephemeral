GO ?= go
VERSION := $(shell cat VERSION)
DIST := dist
PKG := ephemeral-$(VERSION)
FLAGS := -mod=vendor -buildvcs=false -trimpath -ldflags="-s -w"
PREFIX ?= /usr/local
.PHONY: build arm64 test test-portable bench demo release install verify
build:
	CGO_ENABLED=1 $(GO) build $(FLAGS) -o ephemeral ./cmd/ephemeral
arm64:
	mkdir -p $(DIST)/linux-arm64
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build $(FLAGS) -o $(DIST)/linux-arm64/ephemeral ./cmd/ephemeral
test:
	CGO_ENABLED=1 $(GO) test -mod=vendor -race ./...
test-portable:
	CGO_ENABLED=0 $(GO) test -mod=vendor ./...
bench:
	MEASURE=1 $(GO) test -count=1 -run TestMeasurement -v
demo: build
	python3 examples/demo.py
install: build
	install -Dm755 ephemeral "$(DESTDIR)$(PREFIX)/bin/ephemeral"
release: arm64
	python3 scripts/package.py
verify:
	cd $(DIST) && sha256sum -c SHA256SUMS

.PHONY: examples
examples:
	CGO_ENABLED=0 $(GO) build $(FLAGS) -o ephemeral ./cmd/ephemeral
	@set -e; d=$$(mktemp -d); trap 'rm -rf "$$d"' EXIT; ./ephemeral "$$d/example.db" < examples/approval.jsonl > "$$d/replies"; python3 -c 'import json,sys; r=list(map(json.loads,open(sys.argv[1]))); assert r[2]==["draft-42"] and r[4]["State"]=="completed" and r[4]["Value"]=={"approved": True} and r[6][-1]["Kind"]=="WorkflowCompleted"' "$$d/replies"
	python3 examples/demo.py
	python3 examples/parallel.py
	CGO_ENABLED=0 $(GO) run -mod=vendor ./examples/retry

# --long keeps a build on a tag reading as a commit, never as that release.
VERSION := $(shell git describe --tags --always --dirty --long 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build install

build:
	go build $(LDFLAGS) -o agent-manager .

install:
	go install $(LDFLAGS) .

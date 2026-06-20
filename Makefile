BINARY      := pgflow
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     := -ldflags="-s -w -X main.version=$(VERSION)"
INSTALL_DIR ?= $(HOME)/.local/bin
DIST_DIR    := dist

# Supported platforms for releases
PLATFORMS   := \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/arm64

.PHONY: all build run test vet tidy install uninstall clean release build-all help

all: test build

## help: show this help
help:
	@echo "pgflow — make targets:"
	@echo "  build        build the binary for the current platform"
	@echo "  run          build and run the TUI"
	@echo "  test         run the test suite"
	@echo "  vet          static analysis"
	@echo "  tidy         go mod tidy"
	@echo "  install      install to \$$INSTALL_DIR"
	@echo "  uninstall    remove from \$$INSTALL_DIR"
	@echo "  clean        remove build artifacts"
	@echo "  build-all    cross-compile for darwin/linux × amd64/arm64 → dist/"
	@echo "  release      alias for build-all"

## build: build the binary for the current platform
build:
	go build $(LDFLAGS) -o $(BINARY) .

## run: build and launch the TUI
run: build
	./$(BINARY)

## test: run the test suite
test:
	go test ./...

## vet: go vet
vet:
	go vet ./...

## tidy: clean up go.mod / go.sum
tidy:
	go mod tidy

## install: build + copy binary to INSTALL_DIR
install: build
	install -d $(INSTALL_DIR)
	install -m 0755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "✅ installed → $(INSTALL_DIR)/$(BINARY)"

## uninstall: remove binary from INSTALL_DIR
uninstall:
	rm -f $(INSTALL_DIR)/$(BINARY)

## clean: remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf $(DIST_DIR)
	go clean

## build-all: cross-compile for all supported platforms
build-all: clean
	@mkdir -p $(DIST_DIR)
	@for p in $(PLATFORMS); do \
		os=$$(echo $$p | cut -d/ -f1); \
		arch=$$(echo $$p | cut -d/ -f2); \
		out=$(DIST_DIR)/$(BINARY)-$$os-$$arch; \
		echo "  → $$out"; \
		GOOS=$$os GOARCH=$$arch go build $(LDFLAGS) -o $$out . ; \
	done
	@ls -lh $(DIST_DIR)

## release: alias for build-all
release: build-all

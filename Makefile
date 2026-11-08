BIN          := clipshare
BIN_WIN      := clipshare.exe
BIN_LNX      := clipshare-linux-amd64
BIN_ALPINE   := clipshare-alpine-amd64
BIN_ARCH     := clipshare-arch-amd64
INSTALL_DIR  ?= $(HOME)/.local/bin
VERSION_FILE := src/internal/version/version.go

VERSION := $(shell sed -n 's/.*const Version = "\([^"]*\)".*/\1/p' $(VERSION_FILE))
VERSION_BASE := $(shell echo "$(VERSION)" | sed 's/-.*//')
VERSION_NUMERIC := $(VERSION_BASE).0
DIST    := dist

GO_LDFLAGS := -s -w

.PHONY: all build build-linux build-windows build-all release version install uninstall clean test installer

all: build

build: ## Build for the current platform
	go build -o $(BIN) ./src/cmd/clipshare

build-linux: ## Cross-compile a static Linux amd64 binary (works on Alpine, Arch, and other glibc/musl distros)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(GO_LDFLAGS)" -o $(BIN_LNX) ./src/cmd/clipshare

build-alpine: ## Cross-compile a static Linux amd64 binary for Alpine Linux (musl)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(GO_LDFLAGS)" -o $(BIN_ALPINE) ./src/cmd/clipshare

build-arch: ## Cross-compile a Linux amd64 binary for Arch Linux (glibc)
	GOOS=linux GOARCH=amd64 go build -ldflags "$(GO_LDFLAGS)" -o $(BIN_ARCH) ./src/cmd/clipshare

build-windows: ## Cross-compile a Windows amd64 binary
	GOOS=windows GOARCH=amd64 go build -ldflags "$(GO_LDFLAGS)" -o $(BIN_WIN) ./src/cmd/clipshare

build-all: build-alpine build-arch build-windows ## Build release binaries for Alpine, Arch, and Windows into $(DIST)
	@mkdir -p $(DIST)
	cp $(BIN_ALPINE) $(DIST)/clipshare-$(VERSION)-alpine-amd64
	cp $(BIN_ARCH) $(DIST)/clipshare-$(VERSION)-arch-amd64
	cp $(BIN_WIN) $(DIST)/clipshare-$(VERSION)-windows-amd64.exe
	cd $(DIST) && sha256sum clipshare-$(VERSION)-alpine-amd64 clipshare-$(VERSION)-arch-amd64 clipshare-$(VERSION)-windows-amd64.exe > checksums.txt
	@echo "Release artifacts in $(DIST)/"

installer: build-windows ## Build a Windows NSIS installer from the repo template
	@mkdir -p $(DIST)
	cp $(BIN_WIN) $(DIST)/clipshare-$(VERSION)-windows-amd64.exe
	@sed -e "s/__VERSION__/$(VERSION)/g" -e "s/__VERSION_NUMERIC__/$(VERSION_NUMERIC)/g" windows/clipshare-installer.nsi > $(DIST)/clipshare-installer.nsi
	@makensis -INPUTCHARSET UTF-8 $(DIST)/clipshare-installer.nsi

release: build-all installer ## Build all release binaries and the Windows installer

version: ## Print the application version used by release artifacts
	@printf '%s\n' "$(VERSION)"

install: build ## Symlink $(BIN) into $(INSTALL_DIR) and print PATH hint
	@mkdir -p $(INSTALL_DIR)
	@ln -sf "$(CURDIR)/$(BIN)" "$(INSTALL_DIR)/$(BIN)"
	@echo "Installed $(INSTALL_DIR)/$(BIN) -> $(CURDIR)/$(BIN)"
	@echo "Make sure $(INSTALL_DIR) is on your PATH (e.g. export PATH=\$$HOME/.local/bin:\$$PATH)"

uninstall: ## Remove the symlink from $(INSTALL_DIR)
	@rm -f "$(INSTALL_DIR)/$(BIN)"
	@echo "Removed $(INSTALL_DIR)/$(BIN)"

clean: ## Remove built binaries
	rm -rf $(BIN) $(BIN_WIN) $(BIN_LNX) $(DIST)

test: ## Run unit tests and go vet
	go test ./src/cmd/... ./src/internal/...
	go vet ./...

integration-test: ## Build image and run Docker integration tests
	docker build -t clipshare:integration .
	# -parallel 2: each test creates its own Docker bridge network; hosts with a
	# small default address pool (e.g. this one) exhaust it under full parallelism.
	go test ./src/tests/integration/... -count=1 -v -parallel 2

integration-test-clean: ## Remove integration test Docker images
	docker rmi clipshare:integration || true

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

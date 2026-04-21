# ABOUTME: Build automation for Sendspin Protocol server and player
# ABOUTME: Provides targets for building, testing, and cleaning binaries

.PHONY: all build player server test test-verbose test-coverage lint clean install \
	build-all build-linux build-darwin help conformance \
	install-daemon uninstall-daemon \
	install-player-daemon uninstall-player-daemon \
	install-server-daemon uninstall-server-daemon

# Version from git tag or default
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X github.com/Sendspin/sendspin-go/internal/version.Version=$(VERSION)

# Default target
all: build

# Build both player and server
build: player server

# Build the player
player:
	@echo "Building sendspin-player..."
	go build -ldflags "$(LDFLAGS)" -o sendspin-player .

# Build the server
server:
	@echo "Building sendspin-server..."
	go build -ldflags "$(LDFLAGS)" -o sendspin-server ./cmd/sendspin-server

# Run tests
test:
	@echo "Running tests..."
	go test ./...

# Run tests with verbose output
test-verbose:
	@echo "Running tests (verbose)..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run linter
lint:
	@echo "Running golangci-lint..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Install: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run --timeout=5m

# Clean built binaries and artifacts
clean:
	@echo "Cleaning binaries and artifacts..."
	rm -f sendspin-player sendspin-server resonate-player resonate-server
	rm -rf bin/
	rm -f coverage.out coverage.html

# Install both binaries to GOPATH/bin
install:
	@echo "Installing binaries..."
	go install -ldflags "$(LDFLAGS)" .
	go install -ldflags "$(LDFLAGS)" ./cmd/sendspin-server

# Build all platforms (like CI)
build-all: build-linux build-darwin

# Build Linux binaries
build-linux:
	@echo "Building Linux binaries..."
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/sendspin-player-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/sendspin-player-linux-arm64 .
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/sendspin-server-linux-amd64 ./cmd/sendspin-server
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/sendspin-server-linux-arm64 ./cmd/sendspin-server

# Build macOS binaries
build-darwin:
	@echo "Building macOS binaries..."
	@mkdir -p bin
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/sendspin-player-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/sendspin-player-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/sendspin-server-darwin-amd64 ./cmd/sendspin-server
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/sendspin-server-darwin-arm64 ./cmd/sendspin-server

# Install sendspin-player as a systemd daemon
install-player-daemon: player
	@echo "Installing sendspin-player daemon..."
	install -m 755 sendspin-player /usr/local/bin/sendspin-player
	install -m 644 dist/systemd/sendspin-player.service /etc/systemd/system/sendspin-player.service
	@if [ ! -f /etc/default/sendspin-player ]; then \
		install -m 644 dist/systemd/sendspin-player.env /etc/default/sendspin-player; \
		echo "Created /etc/default/sendspin-player — edit this file to configure."; \
	else \
		echo "/etc/default/sendspin-player already exists, not overwriting."; \
	fi
	@if [ ! -f /etc/sendspin/player.yaml ]; then \
		install -d -m 755 /etc/sendspin; \
		install -m 644 dist/config/player.example.yaml /etc/sendspin/player.yaml; \
		echo "Created /etc/sendspin/player.yaml — edit this file to configure."; \
	else \
		echo "/etc/sendspin/player.yaml already exists, not overwriting."; \
	fi
	systemctl daemon-reload
	@echo ""
	@echo "Installed. To start:"
	@echo "  sudo systemctl enable --now sendspin-player"
	@echo ""
	@echo "Configure via /etc/sendspin/player.yaml (preferred) or /etc/default/sendspin-player"

# Install sendspin-server as a systemd daemon
install-server-daemon: server
	@echo "Installing sendspin-server daemon..."
	install -m 755 sendspin-server /usr/local/bin/sendspin-server
	install -m 644 dist/systemd/sendspin-server.service /etc/systemd/system/sendspin-server.service
	@if [ ! -f /etc/default/sendspin-server ]; then \
		install -m 644 dist/systemd/sendspin-server.env /etc/default/sendspin-server; \
		echo "Created /etc/default/sendspin-server — edit this file to configure."; \
	else \
		echo "/etc/default/sendspin-server already exists, not overwriting."; \
	fi
	@if [ ! -f /etc/sendspin/server.yaml ]; then \
		install -d -m 755 /etc/sendspin; \
		install -m 644 dist/config/server.example.yaml /etc/sendspin/server.yaml; \
		echo "Created /etc/sendspin/server.yaml — edit this file to configure."; \
	else \
		echo "/etc/sendspin/server.yaml already exists, not overwriting."; \
	fi
	systemctl daemon-reload
	@echo ""
	@echo "Installed. To start:"
	@echo "  sudo systemctl enable --now sendspin-server"
	@echo ""
	@echo "Configure via /etc/sendspin/server.yaml (preferred) or /etc/default/sendspin-server"

# Aggregate: install both binaries as systemd daemons
install-daemon: install-player-daemon install-server-daemon

# Uninstall the sendspin-player systemd daemon
uninstall-player-daemon:
	@echo "Removing sendspin-player daemon..."
	-systemctl stop sendspin-player 2>/dev/null
	-systemctl disable sendspin-player 2>/dev/null
	rm -f /etc/systemd/system/sendspin-player.service
	rm -f /usr/local/bin/sendspin-player
	systemctl daemon-reload
	@echo "Removed. /etc/default/sendspin-player and /etc/sendspin/player.yaml left in place (manual cleanup if desired)."

# Uninstall the sendspin-server systemd daemon
uninstall-server-daemon:
	@echo "Removing sendspin-server daemon..."
	-systemctl stop sendspin-server 2>/dev/null
	-systemctl disable sendspin-server 2>/dev/null
	rm -f /etc/systemd/system/sendspin-server.service
	rm -f /usr/local/bin/sendspin-server
	systemctl daemon-reload
	@echo "Removed. /etc/default/sendspin-server and /etc/sendspin/server.yaml left in place (manual cleanup if desired)."

# Aggregate: uninstall both daemons
uninstall-daemon: uninstall-player-daemon uninstall-server-daemon

# Conformance test suite — runs the Sendspin protocol conformance harness
# against the local sendspin-go checkout. Mirrors what CI does so contributors
# and conformance maintainers can reproduce CI results locally.
#
# Assumes the conformance repo is checked out at ../conformance. Clones it
# (and the aiosendspin reference peer) on first run. The CONFORMANCE_REPO_SENDSPIN_GO
# env var points the harness at this checkout instead of the managed clone.
conformance:
	@command -v uv >/dev/null 2>&1 || { echo "uv is required: https://docs.astral.sh/uv/getting-started/installation/"; exit 1; }
	@command -v git >/dev/null 2>&1 || { echo "git is required"; exit 1; }
	@if [ ! -d ../conformance ]; then \
		echo "Cloning Sendspin/conformance into ../conformance..."; \
		git clone --depth 1 https://github.com/Sendspin/conformance.git ../conformance; \
	fi
	@mkdir -p ../conformance/repos
	@if [ ! -e ../conformance/repos/aiosendspin ]; then \
		echo "Cloning aiosendspin reference peer..."; \
		git clone --depth 1 https://github.com/Sendspin/aiosendspin.git ../conformance/repos/aiosendspin; \
	fi
	@if [ ! -e ../conformance/repos/sendspin-cli ]; then \
		echo "Cloning sendspin-cli (supplies the FLAC test fixture)..."; \
		git clone --depth 1 https://github.com/Sendspin/sendspin-cli.git ../conformance/repos/sendspin-cli; \
	fi
	@ln -sfn "$(CURDIR)" ../conformance/repos/sendspin-go
	@cd ../conformance && uv sync
	@cd ../conformance && CONFORMANCE_REPO_SENDSPIN_GO="$(CURDIR)" uv run python scripts/run_all.py \
		--from sendspin-go \
		--to aiosendspin,sendspin-go \
		--results-dir results/
	@echo ""
	@echo "Conformance report: $(CURDIR)/../conformance/results/index.html"
	@echo "Raw results:        $(CURDIR)/../conformance/results/data/"

# Show help
help:
	@echo "Sendspin Protocol - Build Targets"
	@echo ""
	@echo "  make              - Build player and server"
	@echo "  make player       - Build sendspin-player"
	@echo "  make server       - Build sendspin-server"
	@echo "  make test         - Run tests"
	@echo "  make test-verbose - Run tests with verbose output"
	@echo "  make test-coverage- Run tests with coverage report"
	@echo "  make lint         - Run golangci-lint"
	@echo "  make clean        - Remove built binaries"
	@echo "  make install      - Install to GOPATH/bin"
	@echo "  make build-all    - Build all platforms"
	@echo "  make build-linux  - Build Linux binaries"
	@echo "  make build-darwin - Build macOS binaries"
	@echo "  make install-daemon           - Install both player and server as systemd daemons (Linux, requires root)"
	@echo "  make install-player-daemon    - Install only the player daemon"
	@echo "  make install-server-daemon    - Install only the server daemon"
	@echo "  make uninstall-daemon         - Remove both daemons"
	@echo "  make uninstall-player-daemon  - Remove only the player daemon"
	@echo "  make uninstall-server-daemon  - Remove only the server daemon"
	@echo "  make conformance  - Run protocol conformance suite (clones ../conformance on first run)"
	@echo ""
	@echo "Version: $(VERSION)"

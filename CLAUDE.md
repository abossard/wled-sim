# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

A desktop WLED simulator — behaves like a real WLED WiFi LED controller node but runs entirely on a workstation. Provides a Fyne GUI LED matrix display, a WLED-compatible JSON REST API (Gin), and a full-speed DDP UDP listener. Primary use cases: LedFX client development and automated integration testing.

## Build & Run Commands

```bash
make build            # Build binary to build/wled-sim
make run              # Build and run with default config
make run-headless     # Run without GUI (for CI/testing)
make test             # Run all tests with -v -race
make test-coverage    # Generate coverage.html
make fmt              # Format code (gofmt)
make lint             # Lint with golangci-lint
make deps             # go mod download && tidy
```

Run a single test:
```bash
go test -v -race ./internal/ddp/ -run TestHeaderParse
```

Requires Go 1.21+ and CGO (for Fyne GUI; libGL/X11 on Linux).

## Architecture

**Entry point:** `cmd/main.go` — a `runtime` struct serializes start/stop/apply operations behind a mutex. Servers run in goroutines; GUI runs in Fyne's event loop.

**Lifecycle:** parse flags → load config.yaml → create shared LEDState → start DDP + HTTP servers concurrently → launch GUI (unless headless) → graceful shutdown on SIGTERM/SIGINT.

**Core modules under `internal/`:**

- **state/** — `LEDState` is the single source of truth. Thread-safe (sync.RWMutex). Stores `[]color.RGBA` where RGBW mode packs W into the A field. Activity counters use atomics. Provides channels for JSON/DDP activity events.
- **api/** — Gin HTTP server exposing WLED-compatible endpoints (`/json`, `/json/state`, `/json/info`, `/json/cfg`, `/json/nodes`). Deterministic MAC address derived from ports + LED count. Full LedFX compatibility (brand, vid, matrix, segments).
- **ddp/** — UDP DDP v1 listener (default port 4048). Header parsing in `header.go`, server in `server.go`. Windowed sequence rejection matching real WLED v16 behavior. Supports RGB (3-byte) and RGBW (4-byte) data types.
- **gui/** — Fyne app with LED grid, activity indicator lights (green/red), and a settings modal. Modal stops servers while editing, restarts after apply/cancel. `sidebar.go` has the settings form.
- **config/** — YAML-backed config struct with CLI flag tags. `Defaults()`, `Load(path)`, `Save(path)`.
- **recorder/** — GIF/MP4 frame capture. MP4 requires FFmpeg. API-controlled via `/api/record`.

**Data flow:** DDP packets → `state.SetLED()` → GUI refresh loop reads `state.LEDs()`. HTTP POST `/json/state` also writes through `LEDState`. Both paths are mutex-protected.

## Key Design Decisions

- RGBW mode is experimental: the W channel is stored in `color.RGBA.A` (normally alpha), which means A=255 indicates RGB mode while other A values indicate white channel intensity in RGBW mode.
- LED wiring supports row-major and column-major patterns, affecting how linear LED indices map to the 2D grid.
- The settings modal intentionally stops and restarts servers to avoid mid-edit race conditions.
- DDP sequence handling uses a sliding window (size 5) to match WLED v16's real behavior rather than strict monotonic ordering.

## Testing

Tests live alongside source files (`*_test.go`). The API tests use `httptest` with Gin's test mode. DDP tests validate header parsing and packet handling. `scripts/ddp_test.py` is a Python DDP client for manual/interactive testing.

CI runs on GitHub Actions: tests on Ubuntu, then cross-compiles for Linux/macOS/Windows (amd64 + arm64).

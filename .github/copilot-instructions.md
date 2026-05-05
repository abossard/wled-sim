# Copilot instructions for WLED Simulator

## Build, test, and lint commands

- `make build` builds the binary to `build/wled-sim`.
- `make run` builds and runs the simulator with the default config.
- `make run-headless` runs without the Fyne GUI, which is useful for CI/API/DDP testing.
- `make test` runs `go test -v ./...`.
- `go test -v -race ./...` matches the CI test command.
- `go test -v ./internal/ddp -run TestParseHeader` runs a single test; use package paths and `-run` for focused tests.
- `make test-coverage` writes `coverage.out` and `coverage.html`.
- `make fmt` runs `go fmt ./...`.
- `make lint` runs `golangci-lint run`; it requires `golangci-lint` to be installed.
- `make deps` runs `go mod download` and `go mod tidy`.

The module targets Go 1.21 and uses CGO through Fyne. On Linux, CI installs `gcc libgl1-mesa-dev xorg-dev` before tests/builds.

## High-level architecture

This is a desktop WLED simulator: a Fyne LED matrix UI, a Gin-based WLED JSON API, and a UDP DDP listener all share one `internal/state.LEDState`.

Startup is coordinated in `cmd/main.go`: parse flags, load `config.yaml`, merge explicitly provided flags back over file config, create `LEDState`, create the recorder, start DDP and HTTP servers concurrently, then either run the GUI event loop or block in headless mode until shutdown. The `runtime` type serializes start/stop/apply transitions behind a mutex so settings changes can safely restart servers.

Core flow:

- DDP packets enter `internal/ddp.Server`, are parsed by `header.go`, written to the state's pending LED buffer, and committed on PUSH packets.
- HTTP JSON updates enter `internal/api.Server` through WLED-compatible endpoints and mutate `LEDState` directly.
- The GUI in `internal/gui` polls `LEDState` for display refresh and listens to activity events for JSON/DDP indicator lights.
- The recorder in `internal/recorder` samples `LEDState` frames and encodes GIF and/or MP4 output; MP4 requires `ffmpeg` in `PATH`.

The API exists primarily for WLED and LedFX compatibility. Keep `/json`, `/json/state`, `/json/info`, `/json/cfg`, and `/json/nodes` behavior consistent with the fields tests assert, especially `brand`, `vid`, deterministic `mac`, `leds.count`, `leds.rgbw`, optional `leds.matrix`, row segments, and `if.live.port`.

## Key conventions

- `LEDState` is the single source of truth. Use its methods rather than sharing LED slices directly; `LEDs()` returns a copy, and writes are mutex-protected.
- RGBW mode stores the white channel in `color.RGBA.A`. In RGB mode alpha is normally `255`; in RGBW mode `A` is the W intensity.
- DDP rendering is PUSH-aware: non-PUSH packets buffer data once any PUSH has been seen; PUSH commits the pending buffer and updates live state. Sequence validation uses the WLED v16-style 4-bit sliding window in `checkSequenceWindow`.
- Matrix wiring is either `"row"` or `"col"`. Linear LED index-to-grid mapping is duplicated where rendering/export needs positions, so keep row/column behavior consistent across GUI, DDP interpretation, and recorder output.
- Config fields that can be set by CLI flags use matching `yaml` and `flag` struct tags in `internal/config.Config`; `cmd/main.go` relies on those `flag` tags when reapplying explicitly supplied CLI values over loaded YAML config.
- The settings modal intentionally stops DDP/HTTP servers on open and restarts them on apply or cancel. GUI callbacks are used to avoid import cycles between GUI and server/runtime packages.
- Tests live beside source files as `*_test.go`. API tests use `httptest` plus Gin routes directly; Fyne GUI tests use `fyne.io/fyne/v2/test`; DDP tests cover header parsing, validation, sequence behavior, and server lifecycle.

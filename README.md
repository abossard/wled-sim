# WLED Simulator

> ⚠️ **Disclaimer**: This project is vibe coded. It was built with AI assistance for rapid prototyping and experimentation. Use at your own risk — there be dragons. 🐉

A minimal but extensible desktop application that behaves like a real WLED node while running entirely on your workstation. It offers a Fyne-powered LED matrix, a WLED-compatible JSON REST API, and a full-speed DDP UDP listener, making it ideal for client development and automated integration tests.

## Features

* Configurable LED matrix display in a Fyne GUI with dark theme.
* Full WLED JSON API (`/json`, `/json/state`, `/json/info`, `/json/cfg`, `/json/nodes`) with `live` field support.
* **LedFX compatible**: includes `brand`, `vid`, segment array, matrix dimensions, and sync config fields required by LedFX device detection and DDP streaming.
* DDP UDP listener on port 4048 for real-time LED streaming.
* Thread-safe shared LED state with power and brightness control.
* **Modal settings**: opening settings stops servers; closing restarts them — clean separation between configuration and operation.
* Command-line flags and optional `config.yaml` for easy configuration.
* Indicators for JSON and DDP activity, green for success and red for error.
* **Experimental RGBW support**: 4-channel LED mode via `--rgbw` flag or config, with full DDP and API support.

## Screenshot

![WLED Simulator Screenshot](/docs/wled-sim-demo.png)

5x4 Matrix displays a rainbow during a DDP request.

## Quick Start

```bash
git clone https://github.com/13rac1/wled-simulator
cd wled-simulator
go mod tidy
go run ./cmd -rows 10 -cols 3 -http :9090 -init "#00FF00"
```

Open `http://localhost:9090/json` in your browser or point your WLED mobile app at the same address to test live control.

## Configuration Flags

| Flag        | Default | Description                          |
|-------------|---------|--------------------------------------|
| `-rows`     | 10      | Number of LED rows                   |
| `-cols`     | 2       | Number of LED columns                |
| `-wiring`   | row     | LED wiring pattern: 'row' or 'col'   |
| `-http`     | :8080   | HTTP listen address                  |
| `-ddp-port` | 4048    | UDP port for DDP                     |
| `-init`     | #000000 | Initial LED colour (hex)           |
| `-name`     | LED Light | Display name for the device        |
| `-controls` | false   | Show power/brightness controls in UI |
| `-headless` | false   | Disable GUI for CI (API/DDP only)    |
| `-v`        | false   | Verbose logging                      |
| `-rgbw`     | false   | Enable experimental RGBW (4-channel) LED support |

You can also create a `config.yaml` file with the same keys to persist defaults.

```yaml
rows: 4
cols: 80
wiring: "row"
http_address: ":80"
ddp_port: 4048
init_color: "#000000"
name: "LED Light"
rgbw: true
```

### LED Wiring Patterns

The simulator supports two common LED matrix wiring patterns:

- **Row-major (`-wiring row`)**: LEDs are wired left-to-right, then top-to-bottom
  ```
  0 → 1 → 2
  3 → 4 → 5
  ```

- **Column-major (`-wiring col`)**: LEDs are wired top-to-bottom, then left-to-right
  ```
  0   2   4
  ↓   ↓   ↓
  1   3   5
  ```

## WLED API Compatibility

The simulator implements the following WLED JSON API endpoints:

| Endpoint        | Method | Description                                      |
|-----------------|--------|--------------------------------------------------|
| `/json`         | GET    | Combined state, info, and config                 |
| `/json/state`   | GET    | LED state including segment array (`seg`)        |
| `/json/state`   | POST   | Update LED state (on/off, brightness, colors)    |
| `/json/info`    | GET    | Device info with `brand`, `vid`, `mac`, `leds.matrix` |
| `/json/cfg`     | GET    | Sync/live config (DDP port, timeout)             |
| `/json/nodes`   | GET    | WLED node discovery (empty for standalone)       |

### LedFX Integration Details

The API responses are designed to pass LedFX's WLED device detection:

- **`brand: "WLED"`** and **`vid: 2407260`** — required for device identification and DDP support detection.
- **`leds.matrix`** — reports `{w, h}` dimensions so LedFX detects matrix layouts (e.g., `{w: 80, h: 4}` for a 4×80 grid).
- **`seg` array** — one segment per row with WLED-compatible fields (`id`, `start`, `stop`, `len`, `on`, etc.), preventing the "seg" error toast in LedFX.
- **`/json/cfg`** — includes `if.live.port` pointing to the DDP port, enabling LedFX to discover the streaming endpoint.
- **`/json/nodes`** — returns empty nodes array to satisfy LedFX's node discovery request.

## Settings

Click the **Settings** button at the bottom of the main window to configure the simulator. Settings are **modal** — opening them pauses the DDP/HTTP servers, and closing them (via Apply or Cancel) restarts the servers. This ensures a clean, responsive settings experience.

## Testing

Run all unit tests:

```bash
go test ./...
```

### Using with LedFX

To use this simulator with [LedFX](https://github.com/LedFx/LedFx):

1. **Start the simulator on port 80** (LedFX expects WLED on the default HTTP port):
   ```bash
   # macOS/Linux may require sudo for port 80
   sudo go run ./cmd -http :80
   # Or use config.yaml with http_address: ":80"
   ```

2. **Add as a WLED device** in LedFX using IP `127.0.0.1`.

3. LedFX will detect the simulator as a WLED device (DDP mode) and stream LED data to UDP port 4048.

> **Note:** If you change settings in the simulator, delete and re-add the WLED device in LedFX to pick up the new matrix/segment configuration.

### Manual Testing with curl

Test the HTTP API endpoints with these curl commands (assumes simulator running on localhost:8080):

**Set all LEDs to blue:**
```bash
curl -X POST http://localhost:8080/json/state -H "Content-Type: application/json" -d '{"on":true,"bri":255,"seg":[{"col":[[0,0,255]]}]}'
```

**Set all LEDs to red:**
```bash
curl -X POST http://localhost:8080/json/state -H "Content-Type: application/json" -d '{"seg":[{"col":[[255,0,0]]}]}'
```

**Set all LEDs to green:**
```bash
curl -X POST http://localhost:8080/json/state -H "Content-Type: application/json" -d '{"seg":[{"col":[[0,255,0]]}]}'
```

**Set all LEDs to white:**
```bash
curl -X POST http://localhost:8080/json/state -H "Content-Type: application/json" -d '{"seg":[{"col":[[255,255,255]]}]}'
```

**Get current state (with segments):**
```bash
curl http://localhost:8080/json/state | jq '.seg'
```

**Get device info (with matrix dimensions):**
```bash
curl http://localhost:8080/json/info | jq '.leds'
```

The API responses include a `live` field that indicates when DDP data is actively being received (matches real WLED behavior).

### RGBW Mode Testing

Start the simulator with RGBW enabled:
```bash
go run ./cmd -rgbw -rows 5 -cols 3
```

**Set LEDs with RGBW color (white channel = 128):**
```bash
curl -X POST http://localhost:8080/json/state -H "Content-Type: application/json" -d '{"seg":[{"col":[[255,0,0,128]]}]}'
```

**Check RGBW is reported in device info:**
```bash
curl http://localhost:8080/json/info | jq '.leds'
# {"count":15,"rgbw":true,"matrix":{"w":3,"h":5}}
```

### Manual Testing with DDP

Test the DDP protocol using the included Python script (assumes simulator running on localhost:4048):

**Set all LEDs to blue:**
```bash
python3 scripts/ddp_test.py --color blue
```

**Set all LEDs to red:**
```bash
python3 scripts/ddp_test.py --color red
```

**Display rainbow pattern:**
```bash
python3 scripts/ddp_test.py --pattern rainbow
```

**Display gradient pattern:**
```bash
python3 scripts/ddp_test.py --pattern gradient
```

**Run chase pattern (great for testing wiring):**
```bash
python3 scripts/ddp_test.py --pattern chase --delay 0.5
```

**Cycle through colors:**
```bash
python3 scripts/ddp_test.py --pattern cycle --delay 0.5
```

**Test with custom LED count:**
```bash
python3 scripts/ddp_test.py --color green --leds 30
```

**Test with remote host:**
```bash
python3 scripts/ddp_test.py --color white --host 192.168.1.100
```

## License

AGPL
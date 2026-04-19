package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wled-simulator/internal/state"

	"github.com/gin-gonic/gin"
)

type testState struct {
	On   bool      `json:"on"`
	Bri  int       `json:"bri"`
	Live bool      `json:"live"`
	Seg  []testSeg `json:"seg"`
}

type testSeg struct {
	ID    int    `json:"id"`
	Start int    `json:"start"`
	Stop  int    `json:"stop"`
	Len   int    `json:"len"`
	On    bool   `json:"on"`
	N     string `json:"n"`
}

type testInfo struct {
	Ver     string       `json:"ver"`
	Vid     int          `json:"vid"`
	Brand   string       `json:"brand"`
	Name    string       `json:"name"`
	Live    bool         `json:"live"`
	Mac     string       `json:"mac"`
	UDPPort int          `json:"udpport"`
	Leds    testLedsInfo `json:"leds"`
}

type testLedsInfo struct {
	Count  int             `json:"count"`
	RGBW   bool            `json:"rgbw"`
	Matrix *testMatrixInfo `json:"matrix,omitempty"`
}

type testMatrixInfo struct {
	W int `json:"w"`
	H int `json:"h"`
}

type testCombined struct {
	State testState `json:"state"`
	Info  testInfo  `json:"info"`
}

// Default test configuration
const (
	testDDPPort = 4048
	testLEDs    = 20
	testRows    = 2
	testCols    = 10
)

func TestGetState(t *testing.T) {
	ledState := state.NewLEDState(testLEDs, "#000000", false)
	srv := NewServer(":0", ledState, testDDPPort, "WLED Simulator", testRows, testCols, nil)

	r := gin.Default()
	r.GET("/json/state", srv.handleGetState)

	req := httptest.NewRequest(http.MethodGet, "/json/state", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp testState
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if !resp.On {
		t.Fatalf("expected power on by default")
	}
	// Live should be false initially
	if resp.Live {
		t.Fatalf("expected live to be false initially")
	}
	// Verify segments exist (one per row)
	if len(resp.Seg) != testRows {
		t.Fatalf("expected %d segments (one per row), got %d", testRows, len(resp.Seg))
	}
	// Check first segment covers first row
	if resp.Seg[0].Start != 0 || resp.Seg[0].Stop != testCols {
		t.Fatalf("expected seg[0] start=0 stop=%d, got start=%d stop=%d", testCols, resp.Seg[0].Start, resp.Seg[0].Stop)
	}
	if resp.Seg[0].Len != testCols {
		t.Fatalf("expected seg[0] len=%d, got %d", testCols, resp.Seg[0].Len)
	}
	// Check second segment covers second row
	if resp.Seg[1].Start != testCols || resp.Seg[1].Stop != testCols*2 {
		t.Fatalf("expected seg[1] start=%d stop=%d, got start=%d stop=%d", testCols, testCols*2, resp.Seg[1].Start, resp.Seg[1].Stop)
	}
}

func TestGetInfo(t *testing.T) {
	ledState := state.NewLEDState(testLEDs, "#000000", false)
	srv := NewServer(":0", ledState, testDDPPort, "WLED Simulator", testRows, testCols, nil)

	r := gin.Default()
	r.GET("/json/info", srv.handleGetInfo)

	req := httptest.NewRequest(http.MethodGet, "/json/info", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp testInfo
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	if resp.Ver != "0.14.0" {
		t.Fatalf("expected version '0.14.0', got %s", resp.Ver)
	}
	if resp.Brand != "WLED" {
		t.Fatalf("expected brand 'WLED', got %s", resp.Brand)
	}
	if resp.Vid != 2407260 {
		t.Fatalf("expected vid 2407260, got %d", resp.Vid)
	}
	// Live should be false initially
	if resp.Live {
		t.Fatalf("expected live to be false initially")
	}
	// Verify matrix info is present (rows > 1)
	if resp.Leds.Matrix == nil {
		t.Fatal("expected leds.matrix to be present for multi-row layout")
	}
	if resp.Leds.Matrix.W != testCols {
		t.Fatalf("expected matrix.w=%d, got %d", testCols, resp.Leds.Matrix.W)
	}
	if resp.Leds.Matrix.H != testRows {
		t.Fatalf("expected matrix.h=%d, got %d", testRows, resp.Leds.Matrix.H)
	}
}

func TestGetConfig(t *testing.T) {
	ledState := state.NewLEDState(testLEDs, "#000000", false)
	srv := NewServer(":0", ledState, testDDPPort, "WLED Simulator", testRows, testCols, nil)

	r := gin.Default()
	r.GET("/json/cfg", srv.handleGetConfig)

	req := httptest.NewRequest(http.MethodGet, "/json/cfg", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	// Check the nested if.live structure that LedFX expects
	ifSection, ok := resp["if"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'if' section in config response")
	}
	liveSection, ok := ifSection["live"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'if.live' section in config response")
	}
	if port, ok := liveSection["port"].(float64); !ok || int(port) != testDDPPort {
		t.Fatalf("expected if.live.port=%d, got %v", testDDPPort, liveSection["port"])
	}
	if en, ok := liveSection["en"].(bool); !ok || !en {
		t.Fatalf("expected if.live.en=true, got %v", liveSection["en"])
	}
}

func TestLedFXCompatibility(t *testing.T) {
	// End-to-end test simulating LedFX's device creation flow
	ledState := state.NewLEDState(testLEDs, "#000000", false)
	srv := NewServer(":0", ledState, testDDPPort, "Test Device", testRows, testCols, nil)

	r := gin.Default()
	r.GET("/json/info", srv.handleGetInfo)
	r.GET("/json/cfg", srv.handleGetConfig)

	// Step 1: LedFX calls GET /json/info
	req := httptest.NewRequest(http.MethodGet, "/json/info", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /json/info: expected 200, got %d", w.Code)
	}

	var info map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("bad JSON from /json/info: %v", err)
	}

	// LedFX checks: brand must exist
	if _, ok := info["brand"]; !ok {
		t.Fatal("LedFX requires 'brand' field in /json/info")
	}

	// LedFX reads vid (must be numeric)
	vid, ok := info["vid"].(float64)
	if !ok {
		t.Fatal("LedFX requires numeric 'vid' field in /json/info")
	}

	// LedFX reads leds.count and leds.rgbw
	leds, ok := info["leds"].(map[string]interface{})
	if !ok {
		t.Fatal("LedFX requires 'leds' object in /json/info")
	}
	if _, ok := leds["count"].(float64); !ok {
		t.Fatal("LedFX requires numeric 'leds.count'")
	}
	if _, ok := leds["rgbw"].(bool); !ok {
		t.Fatal("LedFX requires boolean 'leds.rgbw'")
	}

	// LedFX reads mac and name
	if _, ok := info["mac"].(string); !ok {
		t.Fatal("LedFX requires string 'mac' field")
	}
	if _, ok := info["name"].(string); !ok {
		t.Fatal("LedFX requires string 'name' field")
	}

	// Step 2: If vid >= 2110060, LedFX fetches /json/cfg for sync settings
	if int(vid) >= 2110060 {
		req2 := httptest.NewRequest(http.MethodGet, "/json/cfg", nil)
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Fatalf("GET /json/cfg: expected 200, got %d", w2.Code)
		}
	}
}

func TestGetJSON(t *testing.T) {
	ledState := state.NewLEDState(testLEDs, "#000000", false)
	srv := NewServer(":0", ledState, testDDPPort, "WLED Simulator", testRows, testCols, nil)

	r := gin.Default()
	r.GET("/json", srv.handleGetJSON)

	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp testCombined
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	// Check state section
	if !resp.State.On {
		t.Fatalf("expected power on by default")
	}
	if resp.State.Live {
		t.Fatalf("expected state.live to be false initially")
	}

	// Check info section
	if resp.Info.Ver != "0.14.0" {
		t.Fatalf("expected version '0.14.0', got %s", resp.Info.Ver)
	}
	if resp.Info.Live {
		t.Fatalf("expected info.live to be false initially")
	}
}

func TestLiveFieldWithDDPActivity(t *testing.T) {
	ledState := state.NewLEDState(testLEDs, "#000000", false)
	srv := NewServer(":0", ledState, testDDPPort, "WLED Simulator", testRows, testCols, nil)

	r := gin.Default()
	r.GET("/json/info", srv.handleGetInfo)

	// Simulate DDP activity
	ledState.SetLive()

	req := httptest.NewRequest(http.MethodGet, "/json/info", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp testInfo
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	// Live should be true after SetLive()
	if !resp.Live {
		t.Fatalf("expected live to be true after DDP activity")
	}
}

func TestMACAddressGeneration(t *testing.T) {
	tests := []struct {
		name     string
		httpAddr string
		ddpPort  int
		ledCount int
		wantMAC  string
	}{
		{
			name:     "Default configuration",
			httpAddr: ":8080",
			ddpPort:  4048,
			ledCount: 20,
			wantMAC:  "WL:ED:90:D0:00:14", // Port 8080 = 0x1F90, last byte = 0x90, LEDs = 20 = 0x0014
		},
		{
			name:     "Custom ports and dimensions",
			httpAddr: ":9090",
			ddpPort:  5000,
			ledCount: 128,
			wantMAC:  "WL:ED:82:88:00:80", // Port 9090 = 0x2382, last byte = 0x82, LEDs = 128 = 0x0080
		},
		{
			name:     "Large LED count",
			httpAddr: ":8080",
			ddpPort:  4048,
			ledCount: 65535,
			wantMAC:  "WL:ED:90:D0:FF:FF", // Port 8080 = 0x1F90, last byte = 0x90, LEDs = 65535 = 0xFFFF
		},
		{
			name:     "IP with port",
			httpAddr: "127.0.0.1:8080",
			ddpPort:  4048,
			ledCount: 20,
			wantMAC:  "WL:ED:90:D0:00:14", // Port 8080 = 0x1F90, last byte = 0x90, LEDs = 20 = 0x0014
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ledState := state.NewLEDState(tt.ledCount, "#000000", false)
			srv := NewServer(tt.httpAddr, ledState, tt.ddpPort, "WLED Simulator", testRows, testCols, nil)

			// Test MAC in /json/info endpoint
			r := gin.Default()
			r.GET("/json/info", srv.handleGetInfo)

			req := httptest.NewRequest(http.MethodGet, "/json/info", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			var resp testInfo
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("bad JSON: %v", err)
			}

			if resp.Mac != tt.wantMAC {
				t.Errorf("MAC = %q, want %q", resp.Mac, tt.wantMAC)
			}

			// Verify MAC is consistent in /json endpoint
			r = gin.Default()
			r.GET("/json", srv.handleGetJSON)

			req = httptest.NewRequest(http.MethodGet, "/json", nil)
			w = httptest.NewRecorder()
			r.ServeHTTP(w, req)

			var combined testCombined
			if err := json.Unmarshal(w.Body.Bytes(), &combined); err != nil {
				t.Fatalf("bad JSON: %v", err)
			}

			if combined.Info.Mac != tt.wantMAC {
				t.Errorf("MAC in /json = %q, want %q", combined.Info.Mac, tt.wantMAC)
			}
		})
	}
}

func TestPortCollision(t *testing.T) {
	// Use a specific port for testing
	const testPort = ":8081"
	ledState := state.NewLEDState(testLEDs, "#000000", false)

	// Start first server
	srv1 := NewServer(testPort, ledState, testDDPPort, "WLED Simulator", testRows, testCols, nil)
	errChan1 := make(chan error, 1)
	go func() {
		err := srv1.Start()
		errChan1 <- err // Always send the error, even if nil
	}()

	// Wait for first server to start
	select {
	case err := <-errChan1:
		if err != nil {
			t.Fatalf("First server failed unexpectedly: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		// Server started successfully (no error within timeout)
	}

	// Try to start second server on same port
	srv2 := NewServer(testPort, ledState, testDDPPort, "WLED Simulator", testRows, testCols, nil)
	errChan2 := make(chan error, 1)
	go func() {
		err := srv2.Start()
		errChan2 <- err // Always send the error, even if nil
	}()

	// Wait for error from second server
	select {
	case err := <-errChan2:
		if err == nil {
			t.Fatal("Expected error when starting server on occupied port")
		}
		expectedErrMsg := "bind: address already in use"
		if !strings.Contains(err.Error(), expectedErrMsg) {
			t.Errorf("Expected error containing '%s', got: %v", expectedErrMsg, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for port collision error")
	}

	// Cleanup
	srv1.Stop()
	srv2.Stop()
}

func TestNoRouteHandler(t *testing.T) {
	// Use a specific port for testing
	const testPort = ":8082"
	ledState := state.NewLEDState(testLEDs, "#000000", false)

	// Start server
	srv := NewServer(testPort, ledState, testDDPPort, "WLED Simulator", testRows, testCols, nil)
	errChan := make(chan error, 1)
	go func() {
		err := srv.Start()
		errChan <- err
	}()

	// Wait for server to start
	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("Server failed to start: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		// Server started successfully
	}

	// Test cases for non-existent routes
	tests := []struct {
		name           string
		path           string
		method         string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Non-existent JSON endpoint",
			path:           "/json/nonexistent",
			method:         "GET",
			expectedStatus: http.StatusNotFound,
			expectedBody:   `{"error":"Not found"}`,
		},
		{
			name:           "Random path",
			path:           "/random/path",
			method:         "GET",
			expectedStatus: http.StatusNotFound,
			expectedBody:   `{"error":"Not found"}`,
		},
		{
			name:           "POST to non-existent endpoint",
			path:           "/api/v1/test",
			method:         "POST",
			expectedStatus: http.StatusNotFound,
			expectedBody:   `{"error":"Not found"}`,
		},
	}

	// Run tests
	client := &http.Client{}
	baseURL := "http://localhost" + testPort

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request
			req, err := http.NewRequest(tt.method, baseURL+tt.path, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			// Send request
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()

			// Check status code
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			// Check Content-Type header
			contentType := resp.Header.Get("Content-Type")
			if !strings.Contains(contentType, "application/json") {
				t.Errorf("Expected Content-Type to contain application/json, got %s", contentType)
			}

			// Read and verify response body
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}

			// Trim any whitespace/newlines for comparison
			actualBody := strings.TrimSpace(string(body))
			if actualBody != tt.expectedBody {
				t.Errorf("Expected body %q, got %q", tt.expectedBody, actualBody)
			}

			// Verify activity was reported for JSON endpoints
			if strings.HasPrefix(tt.path, "/json/") {
				// Give a moment for activity to be processed
				time.Sleep(50 * time.Millisecond)
				// Could add method to check ledState's last activity if needed
			}
		})
	}

	// Cleanup
	if err := srv.Stop(); err != nil {
		t.Errorf("Failed to stop server: %v", err)
	}
}

func TestRGBWInfoEndpoint(t *testing.T) {
	// Test with RGBW mode enabled
	ledState := state.NewLEDState(testLEDs, "#000000", true)
	srv := NewServer(":0", ledState, testDDPPort, "WLED Simulator", testRows, testCols, nil)

	r := gin.Default()
	r.GET("/json/info", srv.handleGetInfo)

	req := httptest.NewRequest(http.MethodGet, "/json/info", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp testInfo
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	if !resp.Leds.RGBW {
		t.Fatal("expected leds.rgbw to be true in RGBW mode")
	}
	if resp.Leds.Count != testLEDs {
		t.Fatalf("expected leds.count=%d, got %d", testLEDs, resp.Leds.Count)
	}

	// Test with RGB mode (default)
	ledState2 := state.NewLEDState(testLEDs, "#000000", false)
	srv2 := NewServer(":0", ledState2, testDDPPort, "WLED Simulator", testRows, testCols, nil)

	r2 := gin.Default()
	r2.GET("/json/info", srv2.handleGetInfo)

	req2 := httptest.NewRequest(http.MethodGet, "/json/info", nil)
	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, req2)

	var resp2 testInfo
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	if resp2.Leds.RGBW {
		t.Fatal("expected leds.rgbw to be false in RGB mode")
	}
}

func TestRGBWPostState(t *testing.T) {
	ledState := state.NewLEDState(testLEDs, "#000000", true)
	srv := NewServer(":0", ledState, testDDPPort, "WLED Simulator", testRows, testCols, nil)

	r := gin.Default()
	r.POST("/json/state", srv.handlePostState)

	// POST with RGBW color [255, 0, 128, 64]
	body := `{"seg":[{"col":[[255,0,128,64]]}]}`
	req := httptest.NewRequest(http.MethodPost, "/json/state", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", w.Code)
	}

	// Verify LED color includes W channel
	leds := ledState.LEDs()
	if leds[0].R != 255 || leds[0].G != 0 || leds[0].B != 128 || leds[0].A != 64 {
		t.Fatalf("expected RGBA{255,0,128,64}, got RGBA{%d,%d,%d,%d}",
			leds[0].R, leds[0].G, leds[0].B, leds[0].A)
	}

	// POST with RGB-only color [0, 255, 0] in RGBW mode - W should default to 0
	body2 := `{"seg":[{"col":[[0,255,0]]}]}`
	req2 := httptest.NewRequest(http.MethodPost, "/json/state", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	leds = ledState.LEDs()
	if leds[0].R != 0 || leds[0].G != 255 || leds[0].B != 0 || leds[0].A != 0 {
		t.Fatalf("expected RGBA{0,255,0,0} (W=0), got RGBA{%d,%d,%d,%d}",
			leds[0].R, leds[0].G, leds[0].B, leds[0].A)
	}
}

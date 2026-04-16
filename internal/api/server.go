package api

import (
	"context"
	"fmt"
	"image/color"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"wled-simulator/internal/recorder"
	"wled-simulator/internal/state"

	"github.com/gin-gonic/gin"
)

type Server struct {
	addr     string
	state    *state.LEDState
	server   *http.Server
	httpPort int
	ddpPort  int
	macAddr  string
	name     string
	rows     int
	cols     int
	recorder *recorder.Recorder
}

// NewServer creates a new API server with the given configuration
func NewServer(addr string, s *state.LEDState, ddpPort int, name string, rows, cols int, rec *recorder.Recorder) *Server {
	// Extract HTTP port from addr string (format ":8080" or "127.0.0.1:8080")
	parts := strings.Split(addr, ":")
	httpPort, _ := strconv.Atoi(parts[len(parts)-1])

	srv := &Server{
		addr:     addr,
		state:    s,
		httpPort: httpPort,
		ddpPort:  ddpPort,
		name:     name,
		rows:     rows,
		cols:     cols,
		recorder: rec,
	}

	// Generate MAC address once during initialization
	srv.macAddr = srv.generateMACAddress()

	// Log the MAC address at startup
	fmt.Printf("WLED Simulator MAC Address: %s (http:%d, ddp:%d, leds:%d)\n",
		srv.macAddr, srv.httpPort, srv.ddpPort, len(s.LEDs()))

	gin.SetMode(gin.ReleaseMode)
	return srv
}

// generateMACAddress creates a deterministic MAC address based on configuration
func (s *Server) generateMACAddress() string {
	// Use configuration values to generate MAC bytes
	// Format: WL:ED:HP:DP:LL:LL
	// WL:ED = Fixed prefix for WLED
	// HP = HTTP port last byte
	// DP = DDP port last byte
	// LL:LL = Total LED count as 16-bit number

	// Extract port number from HTTP address
	httpPort := s.httpPort
	if httpPort == 0 {
		// Default to 80 if port extraction fails
		httpPort = 80
	}

	// Get last byte of ports
	httpLastByte := byte(httpPort & 0xFF)
	ddpLastByte := byte(s.ddpPort & 0xFF)

	// Get total LED count as 16-bit number
	ledCount := len(s.state.LEDs())
	ledCountHigh := byte((ledCount >> 8) & 0xFF)
	ledCountLow := byte(ledCount & 0xFF)

	return fmt.Sprintf("WL:ED:%02X:%02X:%02X:%02X",
		httpLastByte,
		ddpLastByte,
		ledCountHigh,
		ledCountLow,
	)
}

func (s *Server) Start() error {
	r := gin.Default()

	// Add middleware to report all JSON API activity
	r.Use(func(c *gin.Context) {
		c.Next()
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/json") {
			s.state.ReportActivity(state.ActivityJSON, c.Writer.Status() < 400)
		}
	})

	// Add 404 handler
	r.NoRoute(func(c *gin.Context) {
		// Report failed activity for ANY 404 request to the HTTP server
		s.state.ReportActivity(state.ActivityJSON, false) // Report failed JSON activity
		c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
	})

	// Add routes
	r.GET("/json", s.handleGetJSON)
	r.GET("/json/state", s.handleGetState)
	r.GET("/json/info", s.handleGetInfo)
	r.POST("/json/state", s.handlePostState)
	r.GET("/json/cfg", s.handleGetConfig)
	r.POST("/json/cfg", s.handlePostConfig)

	// Recording API
	r.POST("/api/record", s.handleRecord)
	r.GET("/api/recordings", s.handleListRecordings)

	s.server = &http.Server{
		Addr:    s.addr,
		Handler: r,
	}

	// Try to start the server
	errChan := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
		close(errChan)
	}()

	// Wait a moment for any immediate startup errors
	select {
	case err := <-errChan:
		return err
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func (s *Server) Stop() error {
	if s.server != nil {
		return s.server.Shutdown(context.Background())
	}
	return nil
}

type statePayload struct {
	On  *bool        `json:"on,omitempty"`
	Bri *int         `json:"bri,omitempty"`
	Seg []segPayload `json:"seg,omitempty"`
}

type segPayload struct {
	Col [][]int `json:"col,omitempty"`
}

// buildLedsInfo returns the leds info object including matrix dimensions
func (s *Server) buildLedsInfo() gin.H {
	info := gin.H{
		"count": len(s.state.LEDs()),
		"rgbw":  s.state.IsRGBW(),
		"wv":    false,
		"cct":   false,
	}
	if s.rows > 1 {
		info["matrix"] = gin.H{
			"w": s.cols,
			"h": s.rows,
		}
	}
	return info
}

// buildSegments returns WLED-compatible segment array, one per row
func (s *Server) buildSegments() []gin.H {
	segs := make([]gin.H, 0, s.rows)
	for i := 0; i < s.rows; i++ {
		start := i * s.cols
		stop := start + s.cols
		segs = append(segs, gin.H{
			"id":    i,
			"start": start,
			"stop":  stop,
			"len":   s.cols,
			"grp":   1,
			"spc":   0,
			"of":    0,
			"on":    true,
			"frz":   false,
			"bri":   255,
			"cct":   127,
			"set":   0,
			"n":     fmt.Sprintf("Row %d", i),
			"col":   [][]int{{255, 160, 0}, {0, 0, 0}, {0, 0, 0}},
			"fx":    0,
			"sx":    128,
			"ix":    128,
			"pal":   0,
			"sel":   i == 0,
			"rev":   false,
			"mi":    false,
		})
	}
	return segs
}

func (s *Server) handleGetJSON(c *gin.Context) {
	ledsInfo := s.buildLedsInfo()
	c.JSON(http.StatusOK, gin.H{
		"state": gin.H{
			"on":   s.state.Power(),
			"bri":  s.state.Brightness(),
			"live": s.state.IsLive(),
			"seg":  s.buildSegments(),
		},
		"info": gin.H{
			"ver":      "0.14.0",
			"vid":      2407260,
			"brand":    "WLED",
			"ip":       "127.0.0.1",
			"name":     s.name,
			"udpport":  21324,
			"live":     s.state.IsLive(),
			"lm":       "",
			"lip":      "",
			"ws":       0,
			"fxcount":  0,
			"palcount": 0,
			"mac":      s.macAddr,
			"leds":     ledsInfo,
			"wifi": gin.H{
				"bssid":   "",
				"rssi":    0,
				"signal":  100,
				"channel": 0,
			},
		},
	})
}

func (s *Server) handleGetState(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"on":   s.state.Power(),
		"bri":  s.state.Brightness(),
		"live": s.state.IsLive(),
		"seg":  s.buildSegments(),
	})
}

func (s *Server) handleGetInfo(c *gin.Context) {
	ledsInfo := s.buildLedsInfo()
	c.JSON(http.StatusOK, gin.H{
		"ver":      "0.14.0",
		"vid":      2407260,
		"brand":    "WLED",
		"ip":       "127.0.0.1",
		"name":     s.name,
		"udpport":  21324,
		"live":     s.state.IsLive(),
		"lm":       "",
		"lip":      "",
		"ws":       0,
		"fxcount":  0,
		"palcount": 0,
		"mac":      s.macAddr,
		"leds":     ledsInfo,
		"wifi": gin.H{
			"bssid":   "",
			"rssi":    0,
			"signal":  100,
			"channel": 0,
		},
	})
}

func (s *Server) handleGetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"if": gin.H{
			"live": gin.H{
				"en":      true,
				"no-gc":   false,
				"maxbri":  false,
				"timeout": 25,
				"port":    s.ddpPort,
				"dmx": gin.H{
					"mode": 4,
					"uni":  1,
					"addr": 1,
				},
			},
		},
	})
}

func (s *Server) handlePostConfig(c *gin.Context) {
	// Accept config updates but don't persist them (simulator is stateless for config)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (s *Server) handlePostState(c *gin.Context) {
	var p statePayload
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if p.On != nil {
		s.state.SetPower(*p.On)
	}
	if p.Bri != nil {
		s.state.SetBrightness(*p.Bri)
	}

	// Process segment colors
	if len(p.Seg) > 0 && len(p.Seg[0].Col) > 0 {
		// Get the first color from the first segment
		col := p.Seg[0].Col[0]
		if len(col) >= 3 {
			r := uint8(col[0])
			g := uint8(col[1])
			b := uint8(col[2])
			a := uint8(255)
			if s.state.IsRGBW() {
				a = 0 // Default W=0 in RGBW mode
				if len(col) >= 4 {
					a = uint8(col[3]) // Use provided W value
				}
			}
			ledColor := color.RGBA{R: r, G: g, B: b, A: a}

			// Set all LEDs to this color
			leds := s.state.LEDs()
			for i := range leds {
				s.state.SetLED(i, ledColor)
			}
		}
	}

	c.Status(http.StatusNoContent)
}

type recordPayload struct {
	Action string `json:"action"` // "start" or "stop"
}

func (s *Server) handleRecord(c *gin.Context) {
	if s.recorder == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "recorder not available"})
		return
	}

	var p recordPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	switch p.Action {
	case "start":
		if err := s.recorder.Start(); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "recording"})
	case "stop":
		filename, err := s.recorder.Stop()
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "stopped", "file": filename})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "action must be 'start' or 'stop'"})
	}
}

func (s *Server) handleListRecordings(c *gin.Context) {
	cwd, err := os.Getwd()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	files, err := recorder.ListRecordings(cwd)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Return absolute paths for download
	var paths []string
	for _, f := range files {
		paths = append(paths, filepath.Base(f))
	}
	c.JSON(http.StatusOK, gin.H{"recordings": paths})
}

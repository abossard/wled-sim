package state

import (
	"fmt"
	"image/color"
	"sync"
	"sync/atomic"
	"time"
)

type ActivityType int

const (
	ActivityJSON ActivityType = iota
	ActivityDDP
)

type ActivityEvent struct {
	Type      ActivityType
	Success   bool
	Timestamp time.Time
}

type LEDState struct {
	mu              sync.RWMutex
	power           bool
	brightness      int // 0-255
	leds            []color.RGBA
	rgbw            bool               // RGBW mode: W channel stored in color.RGBA.A field
	lastLiveTime    time.Time          // Timestamp of last DDP packet received
	liveTimeout     time.Duration      // How long to consider live after last packet
	activityChannel chan ActivityEvent // Channel for activity events
	ddpCount        atomic.Uint64      // Total DDP packets received
	httpCount       atomic.Uint64      // Total HTTP requests handled
	startTime       time.Time          // When the state was created (for uptime)
}

// NewLEDState constructs a LEDState with n LEDs initialized to hex colour.
// If rgbw is true, the W channel is stored in the A field of color.RGBA.
func NewLEDState(n int, hex string, rgbw bool) *LEDState {
	leds := make([]color.RGBA, n)
	c := parseHex(hex, rgbw)
	for i := range leds {
		leds[i] = c
	}
	return &LEDState{
		power:           true,
		brightness:      255,
		leds:            leds,
		rgbw:            rgbw,
		liveTimeout:     5 * time.Second,               // Consider live for 5 seconds after last packet
		activityChannel: make(chan ActivityEvent, 100), // Buffered channel for activity events
		startTime:       time.Now(),
	}
}

// parseHex converts "#RRGGBB" or "#RRGGBBWW" to color.RGBA.
// In RGBW mode, the W channel is stored in the A field; otherwise A is 255.
func parseHex(h string, rgbw bool) color.RGBA {
	var r, g, b uint8
	if len(h) == 9 && h[0] == '#' && rgbw {
		var w uint8
		_, _ = fmt.Sscanf(h[1:], "%02x%02x%02x%02x", &r, &g, &b, &w)
		return color.RGBA{R: r, G: g, B: b, A: w}
	}
	if len(h) == 7 && h[0] == '#' {
		_, _ = fmt.Sscanf(h[1:], "%02x%02x%02x", &r, &g, &b)
	}
	if rgbw {
		return color.RGBA{R: r, G: g, B: b, A: 0}
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

// SetPower sets the on/off state
func (s *LEDState) SetPower(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.power = on
}

func (s *LEDState) Power() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.power
}

// IsRGBW returns true if this state uses RGBW mode (W stored in A field)
func (s *LEDState) IsRGBW() bool {
	return s.rgbw
}

func (s *LEDState) SetBrightness(b int) {
	if b < 0 {
		b = 0
	}
	if b > 255 {
		b = 255
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.brightness = b
}

func (s *LEDState) Brightness() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.brightness
}

func (s *LEDState) SetLED(i int, c color.RGBA) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i >= 0 && i < len(s.leds) {
		s.leds[i] = c
	}
}

func (s *LEDState) LEDs() []color.RGBA {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]color.RGBA, len(s.leds))
	copy(out, s.leds)
	return out
}

// SetLive marks that DDP data is currently being received
func (s *LEDState) SetLive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastLiveTime = time.Now()
}

// IsLive returns true if DDP data has been received recently
func (s *LEDState) IsLive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastLiveTime.IsZero() {
		return false
	}
	return time.Since(s.lastLiveTime) <= s.liveTimeout
}

// SetLiveTimeout sets the duration for which the device should be considered live after receiving data
func (s *LEDState) SetLiveTimeout(timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.liveTimeout = timeout
}

// ReportActivity reports an activity event (non-blocking) and increments the corresponding counter.
func (s *LEDState) ReportActivity(activityType ActivityType, success bool) {
	// Increment the appropriate counter
	switch activityType {
	case ActivityDDP:
		s.ddpCount.Add(1)
	case ActivityJSON:
		s.httpCount.Add(1)
	}

	event := ActivityEvent{
		Type:      activityType,
		Success:   success,
		Timestamp: time.Now(),
	}

	// Non-blocking send to avoid deadlocks
	select {
	case s.activityChannel <- event:
	default:
	}
}

// ActivityChannel returns the activity event channel for consumers
func (s *LEDState) ActivityChannel() <-chan ActivityEvent {
	return s.activityChannel
}

// DDPCount returns the total number of DDP packets received.
func (s *LEDState) DDPCount() uint64 {
	return s.ddpCount.Load()
}

// HTTPCount returns the total number of HTTP requests handled.
func (s *LEDState) HTTPCount() uint64 {
	return s.httpCount.Load()
}

// StartTime returns when the state was created.
func (s *LEDState) StartTime() time.Time {
	return s.startTime
}

// ResetCounters zeroes the DDP and HTTP counters.
func (s *LEDState) ResetCounters() {
	s.ddpCount.Store(0)
	s.httpCount.Store(0)
}

// LEDCount returns the number of LEDs.
func (s *LEDState) LEDCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.leds)
}

// Resize changes the number of LEDs, reinitializing to the given hex color.
func (s *LEDState) Resize(n int, hex string, rgbw bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rgbw = rgbw
	s.leds = make([]color.RGBA, n)
	c := parseHex(hex, rgbw)
	for i := range s.leds {
		s.leds[i] = c
	}
}

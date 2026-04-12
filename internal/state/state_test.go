package state

import (
	"testing"
	"time"
)

func TestLiveFunctionality(t *testing.T) {
	state := NewLEDState(10, "#000000", false)

	// Initially, live should be false
	if state.IsLive() {
		t.Error("Expected IsLive() to be false initially")
	}

	// After calling SetLive(), it should be true
	state.SetLive()
	if !state.IsLive() {
		t.Error("Expected IsLive() to be true after SetLive()")
	}

	// Test custom timeout
	state.SetLiveTimeout(100 * time.Millisecond)
	state.SetLive()

	// Should still be live immediately
	if !state.IsLive() {
		t.Error("Expected IsLive() to be true immediately after SetLive()")
	}

	// Wait for timeout to expire
	time.Sleep(150 * time.Millisecond)

	// Should no longer be live
	if state.IsLive() {
		t.Error("Expected IsLive() to be false after timeout")
	}
}

func TestLiveTimeout(t *testing.T) {
	state := NewLEDState(10, "#000000", false)

	// Test that default timeout is reasonable (should be 5 seconds)
	state.SetLive()
	if !state.IsLive() {
		t.Error("Expected IsLive() to be true after SetLive()")
	}

	// Should still be live after 1 second
	time.Sleep(1 * time.Second)
	if !state.IsLive() {
		t.Error("Expected IsLive() to still be true after 1 second")
	}

	// Change timeout to very short duration
	state.SetLiveTimeout(50 * time.Millisecond)
	state.SetLive()

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)
	if state.IsLive() {
		t.Error("Expected IsLive() to be false after short timeout")
	}
}

func TestRGBWMode(t *testing.T) {
	// RGB mode: A should be 255
	rgbState := NewLEDState(2, "#FF0000", false)
	if rgbState.IsRGBW() {
		t.Error("Expected IsRGBW() to be false for RGB state")
	}
	leds := rgbState.LEDs()
	if leds[0].A != 255 {
		t.Errorf("Expected A=255 in RGB mode, got %d", leds[0].A)
	}

	// RGBW mode: A should be 0 (W=0) for #RRGGBB format
	rgbwState := NewLEDState(2, "#FF0000", true)
	if !rgbwState.IsRGBW() {
		t.Error("Expected IsRGBW() to be true for RGBW state")
	}
	leds = rgbwState.LEDs()
	if leds[0].A != 0 {
		t.Errorf("Expected A=0 (W=0) in RGBW mode with RGB hex, got %d", leds[0].A)
	}
	if leds[0].R != 255 {
		t.Errorf("Expected R=255, got %d", leds[0].R)
	}

	// RGBW mode with #RRGGBBWW format
	rgbwState2 := NewLEDState(2, "#FF000080", true)
	leds = rgbwState2.LEDs()
	if leds[0].A != 128 {
		t.Errorf("Expected A=128 (W=0x80) in RGBW mode with RGBW hex, got %d", leds[0].A)
	}
	if leds[0].R != 255 {
		t.Errorf("Expected R=255, got %d", leds[0].R)
	}
}

package gui

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"wled-simulator/internal/config"
	"wled-simulator/internal/recorder"
	"wled-simulator/internal/state"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type GUI struct {
	app        fyne.App
	window     fyne.Window
	rectangles []*canvas.Rectangle
	state      *state.LEDState
	rows       int
	cols       int
	wiring     string
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	// Activity lights
	jsonLightRect *canvas.Rectangle
	ddpLightRect  *canvas.Rectangle
	flashTimers   map[*canvas.Rectangle]*time.Timer
	timersMutex   sync.Mutex
	// Settings
	grid            *fyne.Container
	gridContainer   *fyne.Container
	onSettingsOpen  func()
	onSettingsClose func(newCfg *config.Config)
	currentCfg      config.Config
	recorder        *recorder.Recorder
	// Record button
	recordBtn *widget.Button
}

// AppParams holds everything the GUI needs. Callbacks avoid import cycles.
type AppParams struct {
	App             fyne.App
	State           *state.LEDState
	Config          config.Config
	Recorder        *recorder.Recorder
	OnSettingsOpen  func()
	OnSettingsClose func(newCfg *config.Config)
}

func NewApp(p AppParams) *GUI {
	totalLEDs := p.Config.Rows * p.Config.Cols
	ctx, cancel := context.WithCancel(context.Background())

	// Force dark theme so the LED grid has a black background
	p.App.Settings().SetTheme(theme.DarkTheme())

	gui := &GUI{
		app:             p.App,
		state:           p.State,
		rectangles:      make([]*canvas.Rectangle, totalLEDs),
		rows:            p.Config.Rows,
		cols:            p.Config.Cols,
		wiring:          p.Config.Wiring,
		ctx:             ctx,
		cancel:          cancel,
		flashTimers:     make(map[*canvas.Rectangle]*time.Timer),
		onSettingsOpen:  p.OnSettingsOpen,
		onSettingsClose: p.OnSettingsClose,
		currentCfg:      p.Config,
		recorder:        p.Recorder,
	}
	gui.window = p.App.NewWindow("WLED Simulator")

	// Create activity lights
	gui.jsonLightRect = canvas.NewRectangle(color.RGBA{128, 128, 128, 255})
	gui.jsonLightRect.StrokeColor = color.Black
	gui.jsonLightRect.StrokeWidth = 1

	gui.ddpLightRect = canvas.NewRectangle(color.RGBA{128, 128, 128, 255})
	gui.ddpLightRect.StrokeColor = color.Black
	gui.ddpLightRect.StrokeWidth = 1

	jsonLabel := canvas.NewText("JSON", color.RGBA{100, 100, 100, 255})
	jsonLabel.TextSize = 10
	jsonLabel.Alignment = fyne.TextAlignLeading

	ddpLabel := canvas.NewText("DDP", color.RGBA{100, 100, 100, 255})
	ddpLabel.TextSize = 10
	ddpLabel.Alignment = fyne.TextAlignLeading

	jsonLightContainer := container.NewWithoutLayout(gui.jsonLightRect)
	gui.jsonLightRect.Resize(fyne.NewSize(12, 12))
	gui.jsonLightRect.Move(fyne.NewPos(0, 0))
	jsonLightContainer.Resize(fyne.NewSize(12, 12))

	ddpLightContainer := container.NewWithoutLayout(gui.ddpLightRect)
	gui.ddpLightRect.Resize(fyne.NewSize(12, 12))
	gui.ddpLightRect.Move(fyne.NewPos(0, 0))
	ddpLightContainer.Resize(fyne.NewSize(12, 12))

	jsonLabelContainer := container.NewWithoutLayout(jsonLabel)
	jsonLabel.Resize(fyne.NewSize(40, 12))
	jsonLabel.Move(fyne.NewPos(10, 0))
	jsonLabelContainer.Resize(fyne.NewSize(40, 12))

	ddpLabelContainer := container.NewWithoutLayout(ddpLabel)
	ddpLabel.Resize(fyne.NewSize(40, 12))
	ddpLabel.Move(fyne.NewPos(10, 0))
	ddpLabelContainer.Resize(fyne.NewSize(40, 12))

	jsonContainer := container.NewHBox(jsonLightContainer, jsonLabelContainer)
	ddpContainer := container.NewHBox(ddpLightContainer, ddpLabelContainer)

	activityContainer := container.NewHBox(
		jsonContainer,
		widget.NewLabel("    "),
		ddpContainer,
	)

	// Build LED grid
	gui.grid = container.NewGridWithColumns(p.Config.Cols)
	gui.buildGridRectangles(totalLEDs)

	mainContent := gui.buildGridArea(activityContainer, p.Config.Name)

	// Settings button — opens modal settings (stops servers first)
	settingsBtn := widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), func() {
		gui.openSettings()
	})

	// Record button — toggle recording
	gui.recordBtn = widget.NewButtonWithIcon("Record", theme.MediaRecordIcon(), func() {
		gui.toggleRecording()
	})

	bottomBar := container.NewHBox(gui.recordBtn, layout.NewSpacer(), settingsBtn)
	gui.window.SetContent(container.NewBorder(nil, bottomBar, nil, nil, mainContent))

	// Size the main window to fit the grid
	ledSize := float32(16)
	gridWidth := float32(p.Config.Cols) * ledSize
	gridHeight := float32(p.Config.Rows) * ledSize
	activityHeight := float32(35)
	nameHeight := float32(0)
	if p.Config.Name != "" {
		nameHeight = 25
	}
	padding := float32(20)
	windowWidth := gridWidth + padding
	if windowWidth < 220 {
		windowWidth = 220
	}
	gui.window.Resize(fyne.NewSize(windowWidth, gridHeight+activityHeight+nameHeight+padding))

	// Set up graceful shutdown on main window close
	gui.window.SetCloseIntercept(func() {
		fmt.Println("GUI: Window closing, shutting down gracefully...")
		gui.stop()
		gui.app.Quit()
	})

	// Start update loop
	gui.wg.Add(1)
	go gui.updateLoop()

	// Start activity monitoring
	gui.wg.Add(1)
	go gui.monitorActivity()

	return gui
}

// openSettings stops servers, shows a modal settings dialog, restarts on close/apply.
func (g *GUI) openSettings() {
	// Stop servers
	if g.onSettingsOpen != nil {
		g.onSettingsOpen()
	}

	var settingsDialog dialog.Dialog
	applied := false

	sidebar := NewSidebar(
		g.currentCfg,
		func(cfg config.Config) {
			// Apply: close dialog, restart with new config
			applied = true
			g.currentCfg = cfg
			settingsDialog.Hide()
			if g.onSettingsClose != nil {
				g.onSettingsClose(&cfg)
			}
		},
		func() {
			// Cancel: close dialog, restart with old config
			settingsDialog.Hide()
		},
	)

	settingsDialog = dialog.NewCustomWithoutButtons("Settings", sidebar.Container, g.window)
	settingsDialog.SetOnClosed(func() {
		// Handle dismiss (X button or Cancel) — only if Apply wasn't already called
		if !applied {
			if g.onSettingsClose != nil {
				g.onSettingsClose(nil)
			}
		}
	})
	settingsDialog.Resize(fyne.NewSize(340, 500))
	settingsDialog.Show()
}

// stop cancels the context and waits for goroutines to finish
func (g *GUI) stop() {
	g.cancel()

	// Clean up flash timers (with mutex protection)
	g.timersMutex.Lock()
	for light, timer := range g.flashTimers {
		timer.Stop()
		delete(g.flashTimers, light)
	}
	g.flashTimers = make(map[*canvas.Rectangle]*time.Timer)
	g.timersMutex.Unlock()

	g.wg.Wait()
}

// buildGridRectangles creates LED rectangles and adds them to the grid.
func (g *GUI) buildGridRectangles(totalLEDs int) {
	ledSize := float32(16)
	g.rectangles = make([]*canvas.Rectangle, totalLEDs)
	for i := 0; i < totalLEDs; i++ {
		rect := canvas.NewRectangle(color.Black)
		rect.Resize(fyne.NewSize(ledSize, ledSize))
		g.rectangles[i] = rect
		g.grid.Add(rect)
	}
}

// buildGridArea wraps the grid with activity lights and optional name label.
func (g *GUI) buildGridArea(activityContainer *fyne.Container, name string) *fyne.Container {
	g.gridContainer = container.NewBorder(nil, nil, nil, nil, g.grid)

	if name != "" {
		nameLabel := widget.NewLabel(name)
		nameLabel.Alignment = fyne.TextAlignCenter
		nameLabel.TextStyle = fyne.TextStyle{Bold: true}
		nameContainer := container.NewCenter(nameLabel)

		topSection := container.NewWithoutLayout(activityContainer, nameContainer)
		activityContainer.Resize(fyne.NewSize(120, 35))
		activityContainer.Move(fyne.NewPos(0, 0))
		nameContainer.Resize(fyne.NewSize(120, 20))
		nameContainer.Move(fyne.NewPos(0, 15))
		topSection.Resize(fyne.NewSize(120, 55))

		return container.NewBorder(topSection, nil, nil, nil, g.gridContainer)
	}
	return container.NewBorder(activityContainer, nil, nil, nil, g.gridContainer)
}

// RebuildGrid recreates the LED grid with new dimensions. Safe to call from any goroutine.
func (g *GUI) RebuildGrid(rows, cols int, wiring string, rgbw bool) {
	fyne.DoAndWait(func() {
		g.rows = rows
		g.cols = cols
		g.wiring = wiring

		// Clear and rebuild the grid
		g.grid.RemoveAll()
		g.grid = container.NewGridWithColumns(cols)
		totalLEDs := rows * cols
		g.buildGridRectangles(totalLEDs)

		// Replace the grid in the container
		g.gridContainer.RemoveAll()
		g.gridContainer.Add(g.grid)
		g.gridContainer.Refresh()
	})
}

// ledIndexToGridPosition converts a linear LED index to grid position based on wiring pattern
func (g *GUI) ledIndexToGridPosition(ledIndex int) (row, col int) {
	if g.wiring == "col" {
		// Column-major: LEDs go top-to-bottom, then left-to-right
		row = ledIndex % g.rows
		col = ledIndex / g.rows
	} else {
		// Row-major: LEDs go left-to-right, then top-to-bottom (default)
		row = ledIndex / g.cols
		col = ledIndex % g.cols
	}
	return row, col
}

// gridPositionToDisplayIndex converts grid position to display rectangle index
func (g *GUI) gridPositionToDisplayIndex(row, col int) int {
	// Display is always row-major (left-to-right, top-to-bottom)
	return row*g.cols + col
}

// updateLoop periodically updates the LED display and syncs record button state
func (g *GUI) updateLoop() {
	defer g.wg.Done()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	wasRecording := false
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.updateDisplay()
			g.syncRecordButton(&wasRecording)
		}
	}
}

// updateDisplay updates all rectangles from the current LED state
func (g *GUI) updateDisplay() {
	// Check if context is cancelled before attempting GUI operations
	select {
	case <-g.ctx.Done():
		return
	default:
	}

	leds := g.state.LEDs()

	// Use fyne.Do to avoid race conditions during shutdown
	fyne.Do(func() {
		isRGBW := g.state.IsRGBW()
		for ledIndex, ledColor := range leds {
			row, col := g.ledIndexToGridPosition(ledIndex)
			displayIndex := g.gridPositionToDisplayIndex(row, col)
			if displayIndex < len(g.rectangles) {
				displayColor := ledColor
				if isRGBW {
					displayColor.A = 255
				}
				g.rectangles[displayIndex].FillColor = displayColor
			}
		}
		// Single refresh of the entire grid — avoids partial repaints
		g.grid.Refresh()
	})
}

// SetOnClose sets a custom close handler for the window
func (g *GUI) SetOnClose(handler func()) {
	g.window.SetCloseIntercept(func() {
		g.stop()
		handler()
	})
}

// Run starts the GUI
func (g *GUI) Run() {
	fmt.Println("GUI: Showing window...")

	// Set up signal handling for graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Handle shutdown signal in a separate goroutine
	go func() {
		<-c
		fmt.Println("GUI: Received shutdown signal, closing window...")
		// Use fyne.DoAndWait to ensure window close happens in UI thread
		fyne.DoAndWait(func() {
			g.window.Close()
		})
	}()

	g.window.ShowAndRun()
	fmt.Println("GUI: Window closed")
}

// monitorActivity monitors activity events and flashes the appropriate lights
func (g *GUI) monitorActivity() {
	defer g.wg.Done()

	for {
		select {
		case <-g.ctx.Done():
			return
		case event := <-g.state.ActivityChannel():
			g.handleActivityEvent(event)
		}
	}
}

// handleActivityEvent processes an activity event and flashes the appropriate light
func (g *GUI) handleActivityEvent(event state.ActivityEvent) {
	var light *canvas.Rectangle
	switch event.Type {
	case state.ActivityJSON:
		light = g.jsonLightRect
	case state.ActivityDDP:
		light = g.ddpLightRect
	}

	if light != nil {
		if event.Success {
			g.flashLight(light, color.RGBA{0, 255, 0, 255}) // Bright green for success
		} else {
			g.flashLight(light, color.RGBA{255, 0, 0, 255}) // Bright red for failure
		}
	}
}

// flashLight flashes a light with the specified color for a brief moment
func (g *GUI) flashLight(light *canvas.Rectangle, flashColor color.RGBA) {
	// Check context before starting any timer operations
	select {
	case <-g.ctx.Done():
		return
	default:
	}

	// Cancel any existing timer for this light (with mutex protection)
	g.timersMutex.Lock()
	if timer, exists := g.flashTimers[light]; exists {
		timer.Stop()
		delete(g.flashTimers, light)
	}
	g.timersMutex.Unlock()

	// Use non-blocking fyne.Do to avoid deadlock during shutdown
	// (close intercept runs on UI thread and waits for goroutines via wg.Wait,
	// so goroutines must never block on the UI thread with DoAndWait).
	// Mutex protects light.FillColor writes since Fyne test driver runs
	// fyne.Do callbacks inline on the calling goroutine.
	fyne.Do(func() {
		g.timersMutex.Lock()
		defer g.timersMutex.Unlock()
		select {
		case <-g.ctx.Done():
			return
		default:
		}
		light.FillColor = flashColor
		light.Refresh()
	})

	// Create timer before adding it to the map
	timer := time.AfterFunc(500*time.Millisecond, func() {
		// Check context first
		select {
		case <-g.ctx.Done():
			g.timersMutex.Lock()
			delete(g.flashTimers, light)
			g.timersMutex.Unlock()
			return
		default:
		}

		// Use non-blocking fyne.Do to avoid deadlock during shutdown
		fyne.Do(func() {
			g.timersMutex.Lock()
			defer g.timersMutex.Unlock()
			select {
			case <-g.ctx.Done():
				delete(g.flashTimers, light)
				return
			default:
			}
			light.FillColor = color.RGBA{128, 128, 128, 255} // Gray (inactive)
			light.Refresh()
		})

		// Clean up timer from map (with mutex protection)
		g.timersMutex.Lock()
		delete(g.flashTimers, light)
		g.timersMutex.Unlock()
	})

	// Only add timer to map if context is still valid
	select {
	case <-g.ctx.Done():
		timer.Stop()
		return
	default:
		g.timersMutex.Lock()
		g.flashTimers[light] = timer
		g.timersMutex.Unlock()
	}
}

// toggleRecording starts or stops recording.
func (g *GUI) toggleRecording() {
	if g.recorder == nil {
		return
	}

	if g.recorder.IsRecording() {
		// Stop recording off the UI thread to avoid blocking during encode
		g.recordBtn.Disable()
		go func() {
			filename, err := g.recorder.Stop()
			fyne.Do(func() {
				g.recordBtn.Enable()
				g.recordBtn.SetText("Record")
				g.recordBtn.SetIcon(theme.MediaRecordIcon())
				g.recordBtn.Importance = widget.MediumImportance
				g.recordBtn.Refresh()

				if err != nil {
					dialog.ShowError(fmt.Errorf("Recording failed: %w", err), g.window)
				} else if filename != "" {
					dialog.ShowInformation("Recording Saved", fmt.Sprintf("Saved: %s", filename), g.window)
				}
			})
		}()
	} else {
		if err := g.recorder.Start(); err != nil {
			dialog.ShowError(fmt.Errorf("Cannot start recording: %w", err), g.window)
			return
		}
		g.recordBtn.SetText("Stop")
		g.recordBtn.SetIcon(theme.MediaStopIcon())
		g.recordBtn.Importance = widget.DangerImportance
		g.recordBtn.Refresh()
	}
}

// syncRecordButton updates the record button to match the recorder's actual state.
// This handles recording started/stopped via HTTP API or auto-stop on duration limit.
func (g *GUI) syncRecordButton(wasRecording *bool) {
	if g.recorder == nil || g.recordBtn == nil {
		return
	}

	isRecording := g.recorder.IsRecording()
	if isRecording == *wasRecording {
		return
	}
	*wasRecording = isRecording

	fyne.Do(func() {
		if isRecording {
			g.recordBtn.SetText("Stop")
			g.recordBtn.SetIcon(theme.MediaStopIcon())
			g.recordBtn.Importance = widget.DangerImportance
		} else {
			g.recordBtn.SetText("Record")
			g.recordBtn.SetIcon(theme.MediaRecordIcon())
			g.recordBtn.Importance = widget.MediumImportance
		}
		g.recordBtn.Refresh()
	})
}

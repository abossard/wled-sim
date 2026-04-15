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
	// Settings window
	sidebar        *Sidebar
	settingsWindow fyne.Window
	grid           *fyne.Container
	gridContainer  *fyne.Container
}

// AppParams holds everything the GUI needs. Callbacks avoid import cycles.
type AppParams struct {
	App         fyne.App
	State       *state.LEDState
	Config      config.Config
	Recorder    *recorder.Recorder
	OnStartStop func(start bool) error
	OnApply     func(config.Config) error
}

func NewApp(p AppParams) *GUI {
	totalLEDs := p.Config.Rows * p.Config.Cols
	ctx, cancel := context.WithCancel(context.Background())

	gui := &GUI{
		app:         p.App,
		state:       p.State,
		rectangles:  make([]*canvas.Rectangle, totalLEDs),
		rows:        p.Config.Rows,
		cols:        p.Config.Cols,
		wiring:      p.Config.Wiring,
		ctx:         ctx,
		cancel:      cancel,
		flashTimers: make(map[*canvas.Rectangle]*time.Timer),
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

	// Add a settings button at the bottom of the main window
	settingsBtn := widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), func() {
		gui.settingsWindow.Show()
		gui.settingsWindow.RequestFocus()
	})

	gui.window.SetContent(container.NewBorder(nil, settingsBtn, nil, nil, mainContent))

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
	if windowWidth < 120 {
		windowWidth = 120
	}
	gui.window.Resize(fyne.NewSize(windowWidth, gridHeight+activityHeight+nameHeight+padding))

	// Build sidebar and open it in a separate settings window
	gui.sidebar = NewSidebar(p.State, p.Config, p.Recorder, p.OnStartStop, p.OnApply)
	gui.settingsWindow = p.App.NewWindow("WLED Settings")
	gui.settingsWindow.SetContent(gui.sidebar.Container)
	gui.settingsWindow.Resize(fyne.NewSize(320, 600))
	gui.settingsWindow.SetCloseIntercept(func() {
		// Just hide the settings window instead of closing
		gui.settingsWindow.Hide()
	})
	gui.settingsWindow.Show()

	// Set up graceful shutdown on main window close
	gui.window.SetCloseIntercept(func() {
		fmt.Println("GUI: Window closing, shutting down gracefully...")
		gui.settingsWindow.Close()
		gui.stop()
		gui.app.Quit()
	})

	// Start update loop
	gui.wg.Add(1)
	go gui.updateLoop()

	// Start activity monitoring
	gui.wg.Add(1)
	go gui.monitorActivity()

	// Start stats refresh ticker
	gui.wg.Add(1)
	go gui.statsLoop()

	return gui
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

	// Wait longer for any in-flight timer callbacks to complete
	time.Sleep(200 * time.Millisecond)

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

// statsLoop refreshes the sidebar statistics every second.
func (g *GUI) statsLoop() {
	defer g.wg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			if g.sidebar != nil {
				g.sidebar.RefreshStats()
			}
		}
	}
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

// updateLoop periodically updates the LED display
func (g *GUI) updateLoop() {
	defer g.wg.Done()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			// Context cancelled, stop updating
			return
		case <-ticker.C:
			g.updateDisplay()
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
			if ledIndex < len(leds) {
				// Convert LED index to grid position based on wiring
				row, col := g.ledIndexToGridPosition(ledIndex)

				// Convert grid position to display rectangle index
				displayIndex := g.gridPositionToDisplayIndex(row, col)

				if displayIndex < len(g.rectangles) {
					displayColor := ledColor
					if isRGBW {
						// In RGBW mode, A stores W channel — force full opacity for display
						displayColor.A = 255
					}
					g.rectangles[displayIndex].FillColor = displayColor
					g.rectangles[displayIndex].Refresh()
				}
			}
		}
	}) // Non-blocking for regular updates
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

	// Use fyne.DoAndWait to ensure GUI updates complete before potential shutdown
	fyne.DoAndWait(func() {
		// Double check context hasn't been cancelled
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

		// Use fyne.DoAndWait for the revert operation too
		fyne.DoAndWait(func() {
			// Final context check before GUI update
			select {
			case <-g.ctx.Done():
				g.timersMutex.Lock()
				delete(g.flashTimers, light)
				g.timersMutex.Unlock()
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

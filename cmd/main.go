package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"sync"
	"syscall"

	"wled-simulator/internal/api"
	"wled-simulator/internal/config"
	"wled-simulator/internal/ddp"
	"wled-simulator/internal/gui"
	"wled-simulator/internal/recorder"
	"wled-simulator/internal/state"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

// Build-time metadata. Overridden via -ldflags="-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// runtime serializes lifecycle transitions (start/stop/apply) behind a mutex.
type runtime struct {
	mu         sync.Mutex
	cfg        config.Config
	state      *state.LEDState
	ddpServer  *ddp.Server
	apiServer  *api.Server
	recorder   *recorder.Recorder
	guiApp     *gui.GUI
	running    bool
	configPath string
}

func (rt *runtime) start() error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.running {
		return nil
	}

	// Recreate servers (DDP context is single-use after Stop)
	rt.ddpServer = ddp.NewServer(rt.cfg.DDPPort, rt.state)
	rt.recorder.UpdateOptions(recorder.Options{
		Format:   rt.cfg.RecordFormat,
		Duration: rt.cfg.RecordDuration,
		FPS:      rt.cfg.RecordFPS,
		Rows:     rt.cfg.Rows,
		Cols:     rt.cfg.Cols,
		Wiring:   rt.cfg.Wiring,
		Dir:      rt.cfg.RecordDir,
	})
	rt.apiServer = api.NewServer(rt.cfg.HTTPAddress, rt.state, rt.cfg.DDPPort, rt.cfg.Name, rt.cfg.Rows, rt.cfg.Cols, rt.recorder)

	if err := rt.ddpServer.Start(); err != nil {
		return fmt.Errorf("DDP start: %w", err)
	}
	if err := rt.apiServer.Start(); err != nil {
		rt.ddpServer.Stop()
		return fmt.Errorf("HTTP start: %w", err)
	}

	rt.running = true
	return nil
}

func (rt *runtime) stop() error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !rt.running {
		return nil
	}

	// Force-stop recording first
	if rt.recorder.IsRecording() {
		rt.recorder.Stop()
	}

	var errs []error
	if err := rt.ddpServer.Stop(); err != nil {
		errs = append(errs, err)
	}
	if err := rt.apiServer.Stop(); err != nil {
		errs = append(errs, err)
	}
	rt.running = false
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// applyConfig applies new config, rebuilds grid if needed, saves, and restarts servers.
// Expects servers to already be stopped.
func (rt *runtime) applyConfig(newCfg config.Config) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	oldCfg := rt.cfg
	if newCfg.RecordFormat == "" {
		newCfg.RecordFormat = oldCfg.RecordFormat
	}
	if newCfg.RecordDir == "" {
		newCfg.RecordDir = oldCfg.RecordDir
	}
	rt.cfg = newCfg

	// Check if grid dimensions changed
	gridChanged := oldCfg.Rows != newCfg.Rows ||
		oldCfg.Cols != newCfg.Cols ||
		oldCfg.Wiring != newCfg.Wiring ||
		oldCfg.RGBW != newCfg.RGBW

	if gridChanged {
		totalLEDs := newCfg.Rows * newCfg.Cols
		rt.state.Resize(totalLEDs, newCfg.InitColor, newCfg.RGBW)
		if rt.guiApp != nil {
			rt.guiApp.RebuildGrid(newCfg.Rows, newCfg.Cols, newCfg.Wiring, newCfg.RGBW)
		}
	}

	// Update recorder options
	rt.recorder.UpdateOptions(recorder.Options{
		Format:   newCfg.RecordFormat,
		Duration: newCfg.RecordDuration,
		FPS:      newCfg.RecordFPS,
		Rows:     newCfg.Rows,
		Cols:     newCfg.Cols,
		Wiring:   newCfg.Wiring,
		Dir:      newCfg.RecordDir,
	})

	// Save config
	if err := newCfg.Save(rt.configPath); err != nil {
		log.Printf("Warning: could not save config: %v", err)
	}

	// Restart servers with new config
	rt.ddpServer = ddp.NewServer(newCfg.DDPPort, rt.state)
	rt.apiServer = api.NewServer(newCfg.HTTPAddress, rt.state, newCfg.DDPPort, newCfg.Name, newCfg.Rows, newCfg.Cols, rt.recorder)
	if err := rt.ddpServer.Start(); err != nil {
		return fmt.Errorf("DDP restart: %w", err)
	}
	if err := rt.apiServer.Start(); err != nil {
		rt.ddpServer.Stop()
		return fmt.Errorf("HTTP restart: %w", err)
	}
	rt.running = true

	return nil
}

func main() {
	defaults := config.Defaults()

	// Command line flags
	var cfg config.Config
	flag.IntVar(&cfg.Rows, "rows", defaults.Rows, "Number of LED rows")
	flag.IntVar(&cfg.Cols, "cols", defaults.Cols, "Number of LED columns")
	flag.StringVar(&cfg.Wiring, "wiring", defaults.Wiring, "LED wiring pattern: 'row' or 'col'")
	flag.StringVar(&cfg.HTTPAddress, "http", defaults.HTTPAddress, "HTTP listen address")
	flag.IntVar(&cfg.DDPPort, "ddp-port", defaults.DDPPort, "UDP port for DDP")
	flag.StringVar(&cfg.InitColor, "init", defaults.InitColor, "Initial color hex")
	flag.StringVar(&cfg.Name, "name", defaults.Name, "Display name for the LED matrix")
	flag.BoolVar(&cfg.Controls, "controls", defaults.Controls, "Show power/brightness controls in GUI")
	flag.BoolVar(&cfg.Headless, "headless", defaults.Headless, "Run without GUI")
	flag.BoolVar(&cfg.Verbose, "v", defaults.Verbose, "Verbose logging")
	flag.BoolVar(&cfg.RGBW, "rgbw", defaults.RGBW, "Enable experimental RGBW (4-channel) LED support")

	configFile := flag.String("config", "", "Configuration file path (defaults to ./config.yaml if present, else OS app config dir)")
	showVersion := flag.Bool("version", false, "Print version information and exit")
	printConfig := flag.Bool("print-config", false, "Print effective configuration as YAML and exit")
	writeDefault := flag.Bool("write-default-config", false, "Write default configuration to the resolved config path and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("wled-sim %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	// Save CLI values before loading config file
	cliValues := cfg

	// Resolve config path: explicit --config wins; otherwise prefer ./config.yaml,
	// falling back to OS app config dir.
	configPath := *configFile
	configExplicit := configPath != ""
	if !configExplicit {
		resolved, err := config.ResolveConfigPath()
		if err != nil {
			log.Fatalf("Could not resolve default config path: %v", err)
		}
		configPath = resolved
	}

	// --write-default-config writes defaults to the resolved path and exits.
	if *writeDefault {
		def := config.Defaults()
		if err := def.Save(configPath); err != nil {
			log.Fatalf("Failed to write default config to %s: %v", configPath, err)
		}
		fmt.Printf("Wrote default config to %s\n", configPath)
		return
	}

	// Load config file if it exists
	if fileCfg, err := config.Load(configPath); err == nil {
		cfg = fileCfg
	} else if !os.IsNotExist(err) {
		log.Fatalf("Failed to load config %s: %v", configPath, err)
	}

	// Apply recording defaults if not set in file
	if cfg.RecordFormat == "" {
		cfg.RecordFormat = defaults.RecordFormat
	}
	if cfg.RecordDuration == 0 {
		cfg.RecordDuration = defaults.RecordDuration
	}
	if cfg.RecordFPS == 0 {
		cfg.RecordFPS = defaults.RecordFPS
	}

	// Restore CLI values that were explicitly set
	cfgValue := reflect.ValueOf(&cfg).Elem()
	cliValue := reflect.ValueOf(&cliValues).Elem()
	cfgType := reflect.TypeOf(cfg)

	flag.Visit(func(f *flag.Flag) {
		for i := 0; i < cfgType.NumField(); i++ {
			field := cfgType.Field(i)
			if flagName := field.Tag.Get("flag"); flagName == f.Name {
				cfgValue.Field(i).Set(cliValue.Field(i))
				break
			}
		}
	})

	// Resolve recordings directory: explicit value in config wins; otherwise OS default.
	if cfg.RecordDir == "" {
		dir, err := config.DefaultRecordDir()
		if err != nil {
			log.Fatalf("Could not resolve default recordings dir: %v", err)
		}
		cfg.RecordDir = dir
	}

	// --print-config prints the effective config as YAML and exits.
	if *printConfig {
		data, err := cfg.Marshal()
		if err != nil {
			log.Fatalf("Failed to marshal config: %v", err)
		}
		os.Stdout.Write(data)
		return
	}

	// Validate wiring pattern
	if cfg.Wiring != "row" && cfg.Wiring != "col" {
		log.Fatalf("Invalid wiring pattern '%s'. Must be 'row' or 'col'", cfg.Wiring)
	}

	totalLEDs := cfg.Rows * cfg.Cols
	ledState := state.NewLEDState(totalLEDs, cfg.InitColor, cfg.RGBW)

	if cfg.Verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	fmt.Printf("wled-sim %s (commit %s, built %s)\n", version, commit, date)
	fmt.Printf("Config path: %s\n", configPath)
	fmt.Printf("Recordings dir: %s\n", cfg.RecordDir)
	fmt.Printf("WLED Simulator starting with %dx%d LED matrix (%d total LEDs, %s-major wiring)\n",
		cfg.Rows, cfg.Cols, totalLEDs, cfg.Wiring)
	if cfg.RGBW {
		fmt.Println("RGBW mode enabled (experimental)")
	}
	fmt.Printf("HTTP API listening on %s\n", cfg.HTTPAddress)
	fmt.Printf("DDP listening on udp/:%d\n", cfg.DDPPort)

	// Create recorder
	rec := recorder.New(ledState, recorder.Options{
		Format:   cfg.RecordFormat,
		Duration: cfg.RecordDuration,
		FPS:      cfg.RecordFPS,
		Rows:     cfg.Rows,
		Cols:     cfg.Cols,
		Wiring:   cfg.Wiring,
		Dir:      cfg.RecordDir,
	})

	// Create servers
	ddpServer := ddp.NewServer(cfg.DDPPort, ledState)
	apiServer := api.NewServer(cfg.HTTPAddress, ledState, cfg.DDPPort, cfg.Name, cfg.Rows, cfg.Cols, rec)

	// Build runtime
	rt := &runtime{
		cfg:        cfg,
		state:      ledState,
		ddpServer:  ddpServer,
		apiServer:  apiServer,
		recorder:   rec,
		running:    false,
		configPath: configPath,
	}

	// Start servers
	startupErrors := make(chan error, 2)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := ddpServer.Start(); err != nil {
			if errors.Is(err, syscall.EADDRINUSE) {
				startupErrors <- fmt.Errorf("DDP port %d is already in use", cfg.DDPPort)
			} else {
				startupErrors <- fmt.Errorf("DDP server error: %v", err)
			}
			return
		}
		startupErrors <- nil
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := apiServer.Start(); err != nil && err != http.ErrServerClosed {
			if errors.Is(err, syscall.EADDRINUSE) {
				startupErrors <- fmt.Errorf("HTTP port %s is already in use", cfg.HTTPAddress)
			} else {
				startupErrors <- fmt.Errorf("API server error: %v", err)
			}
			return
		}
		startupErrors <- nil
	}()

	fmt.Println("Starting servers...")
	for i := 0; i < 2; i++ {
		if err := <-startupErrors; err != nil {
			ddpServer.Stop()
			apiServer.Stop()
			wg.Wait()
			log.Fatalf("Failed to start servers: %v", err)
		}
	}
	rt.running = true

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	if !cfg.Headless {
		fmt.Println("Starting GUI...")
		myApp := app.NewWithID("com.example.wled-simulator")

		guiApp := gui.NewApp(gui.AppParams{
			App:      myApp,
			State:    ledState,
			Config:   cfg,
			Recorder: rec,
			OnSettingsOpen: func() {
				rt.stop()
			},
			OnSettingsClose: func(newCfg *config.Config) {
				if newCfg != nil {
					rt.applyConfig(*newCfg)
				} else {
					rt.start()
				}
			},
		})
		rt.guiApp = guiApp

		shutdownServers := func() {
			rt.stop()
		}

		guiApp.SetOnClose(func() {
			fmt.Println("\nReceived shutdown signal...")
			shutdownServers()
			myApp.Quit()
		})

		go func() {
			<-c
			fmt.Println("\nReceived shutdown signal...")
			shutdownServers()
			fyne.Do(func() {
				myApp.Quit()
			})
		}()

		guiApp.Run()
	} else {
		<-c
		fmt.Println("\nReceived shutdown signal...")
		rt.stop()
	}

	fmt.Println("Shutting down...")
	wg.Wait()
}

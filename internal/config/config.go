package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const appDirName = "wled-sim"

// Config holds application configuration.
type Config struct {
	Rows        int    `yaml:"rows" flag:"rows"`
	Cols        int    `yaml:"cols" flag:"cols"`
	Wiring      string `yaml:"wiring" flag:"wiring"`
	HTTPAddress string `yaml:"http_address" flag:"http"`
	DDPPort     int    `yaml:"ddp_port" flag:"ddp-port"`
	InitColor   string `yaml:"init_color" flag:"init"`
	Name        string `yaml:"name" flag:"name"`
	Controls    bool   `yaml:"controls" flag:"controls"`
	Headless    bool   `yaml:"headless" flag:"headless"`
	Verbose     bool   `yaml:"verbose" flag:"v"`
	RGBW        bool   `yaml:"rgbw" flag:"rgbw"`

	// Recording settings
	RecordFormat   string `yaml:"record_format"`   // "gif", "mp4", "both"
	RecordDuration int    `yaml:"record_duration"` // seconds, default 24
	RecordFPS      int    `yaml:"record_fps"`      // frames per second, default 10
	RecordDir      string `yaml:"record_dir"`      // directory to write recordings into; empty = OS default
}

// Defaults returns a Config with sensible default values.
func Defaults() Config {
	return Config{
		Rows:           10,
		Cols:           2,
		Wiring:         "row",
		HTTPAddress:    ":8080",
		DDPPort:        4048,
		InitColor:      "#000000",
		Controls:       false,
		Headless:       false,
		Verbose:        false,
		RGBW:           false,
		RecordFormat:   "both",
		RecordDuration: 24,
		RecordFPS:      10,
	}
}

// Load reads a YAML config file and returns the parsed Config.
func Load(path string) (Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Save writes the Config as YAML to the given path. Parent directories are
// created as needed.
func (c Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0644)
}

// Marshal returns the YAML representation of the config.
func (c Config) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
}

// DefaultConfigPath returns the OS-specific default config file path,
// e.g. ~/Library/Application Support/wled-sim/config.yaml on macOS.
func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(dir, appDirName, "config.yaml"), nil
}

// ResolveConfigPath picks a config path when the user did not pass --config:
// prefer ./config.yaml if it exists (backward compatibility), otherwise the
// OS app config dir. The returned path may not exist yet.
func ResolveConfigPath() (string, error) {
	if _, err := os.Stat("config.yaml"); err == nil {
		abs, err := filepath.Abs("config.yaml")
		if err != nil {
			return "config.yaml", nil
		}
		return abs, nil
	}
	return DefaultConfigPath()
}

// DefaultRecordDir returns the OS-specific default directory for recordings,
// e.g. ~/Library/Application Support/wled-sim/recordings on macOS. The
// directory is not created here; callers that intend to write should call
// os.MkdirAll on the returned path.
func DefaultRecordDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(dir, appDirName, "recordings"), nil
}

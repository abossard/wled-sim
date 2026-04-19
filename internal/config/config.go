package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

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

// Save writes the Config as YAML to the given path.
func (c Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

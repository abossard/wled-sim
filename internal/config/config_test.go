package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigPath(t *testing.T) {
	p, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath returned error: %v", err)
	}
	if !strings.HasSuffix(p, filepath.Join("wled-sim", "config.yaml")) {
		t.Errorf("expected path to end with wled-sim/config.yaml, got %q", p)
	}
	if !filepath.IsAbs(p) {
		t.Errorf("expected absolute path, got %q", p)
	}
}

func TestDefaultRecordDir(t *testing.T) {
	p, err := DefaultRecordDir()
	if err != nil {
		t.Fatalf("DefaultRecordDir returned error: %v", err)
	}
	if !strings.HasSuffix(p, filepath.Join("wled-sim", "recordings")) {
		t.Errorf("expected path to end with wled-sim/recordings, got %q", p)
	}
	if !filepath.IsAbs(p) {
		t.Errorf("expected absolute path, got %q", p)
	}
}

func TestResolveConfigPathPrefersLocalFile(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// No local config.yaml -> falls back to OS app config dir.
	resolved, err := ResolveConfigPath()
	if err != nil {
		t.Fatalf("ResolveConfigPath: %v", err)
	}
	if !strings.HasSuffix(resolved, filepath.Join("wled-sim", "config.yaml")) {
		t.Errorf("expected fallback to OS app config, got %q", resolved)
	}

	// With local config.yaml -> prefer it.
	if err := os.WriteFile("config.yaml", []byte("rows: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	resolved, err = ResolveConfigPath()
	if err != nil {
		t.Fatalf("ResolveConfigPath with local: %v", err)
	}
	want, _ := filepath.Abs("config.yaml")
	if resolved != want {
		t.Errorf("expected local config %q, got %q", want, resolved)
	}
}

func TestSaveCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "sub", "config.yaml")
	cfg := Defaults()
	if err := cfg.Save(target); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected file at %s: %v", target, err)
	}
	loaded, err := Load(target)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Rows != cfg.Rows {
		t.Errorf("round-trip mismatch: got rows=%d want %d", loaded.Rows, cfg.Rows)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	cfg := Defaults()
	cfg.RecordDir = "/tmp/wled-sim-recordings"
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), "record_dir: /tmp/wled-sim-recordings") {
		t.Errorf("expected record_dir in YAML output, got:\n%s", data)
	}
}

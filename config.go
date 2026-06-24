package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const configDir = ".config/bt-sleepd"
const configFile = "config.json"

// Config controls bt-sleepd behaviour. The zero value (empty JSON) applies
// sensible defaults so every field is optional.
type Config struct {
	// Blacklist of device addresses (or name substrings) to never disconnect.
	Blacklist []string `json:"blacklist"`
	// Whitelist — if non-empty, *only* devices whose address or name matches an
	// entry are disconnected.
	Whitelist []string `json:"whitelist"`
	// PowerOffBT controls whether Bluetooth is powered off after disconnecting
	// devices during a sleep event. Default: true.
	PowerOffBT *bool `json:"power_off_bt,omitempty"`
	// RetryCount is the number of disconnection attempts per device. Default: 3.
	RetryCount int `json:"retry_count"`
	// RetryDelay is the initial delay (seconds) between retries; doubled each
	// attempt (exponential backoff capped at 10s). Default: 1.
	RetryDelay int `json:"retry_delay"`
}

func defaultConfig() Config {
	powerOff := true
	return Config{
		Blacklist:  nil,
		Whitelist:  nil,
		PowerOffBT: &powerOff,
		RetryCount: 3,
		RetryDelay: 1,
	}
}

func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil // no config → defaults
		}
		return cfg, fmt.Errorf("reading config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

// ensureConfigPath returns the canonical config path, creating the directory if
// needed.
func ensureConfigPath(homeDir string) (string, error) {
	dir := filepath.Join(homeDir, configDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return filepath.Join(dir, configFile), nil
}

// doInitConfig writes a default config file to ~/.config/bt-sleepd/config.json.
func doInitConfig(log *Logger, force bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	path, err := ensureConfigPath(home)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists; use --force to overwrite", path)
	}
	cfg := defaultConfig()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	log.Info("Wrote default config to %s", path)
	fmt.Println()
	fmt.Println("Edit it to customise behaviour:")
	fmt.Println()
	fmt.Printf("   open -a TextEdit %s\n", path)
	fmt.Println()
	fmt.Println("Changes take effect the next time bt-sleepd runs.")
	return nil
}

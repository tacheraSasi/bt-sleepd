package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ─── Config ──────────────────────────────────────────────────────────────────

const configDir = ".config/bt-sleepd"
const configFile = "config.json"

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

// ─── Device info ─────────────────────────────────────────────────────────────

type Device struct {
	Address         string `json:"address"`
	Name            string `json:"name,omitempty"`
	FriendlyAddress string `json:"-"` // xx:xx:xx:xx:xx:xx form
}

// blueutil JSON output for connected devices is an array of objects like:
//
//	[{"address": "xx-xx-xx-xx-xx-xx", "name": "...", ...}]

func getConnectedDevices() ([]Device, error) {
	output, err := runCommand("blueutil", "--connected", "--format", "json-pretty")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(output) == "" || strings.TrimSpace(output) == "[]" {
		return nil, nil
	}
	var devices []Device
	if err := json.Unmarshal([]byte(output), &devices); err != nil {
		return nil, fmt.Errorf("parse blueutil output: %w\noutput: %s", err, output)
	}
	// Normalise addresses: xx-xx-xx-xx-xx-xx → xx:xx:xx:xx:xx:xx
	for i := range devices {
		devices[i].FriendlyAddress = strings.ReplaceAll(devices[i].Address, "-", ":")
	}
	return devices, nil
}

// ─── Command runner ──────────────────────────────────────────────────────────

func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %v failed: %w\noutput: %s", name, args, err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

// ─── Logging ─────────────────────────────────────────────────────────────────

// Logger writes timestamped messages to stderr. When quiet is true, only
// warnings and errors are printed.
type Logger struct {
	verbose bool
}

func (l *Logger) Info(format string, args ...any) {
	if !l.verbose {
		return
	}
	fmt.Fprintf(os.Stderr, "[%s] ", time.Now().Format(time.TimeOnly))
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func (l *Logger) Warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[%s] WARN ", time.Now().Format(time.TimeOnly))
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func (l *Logger) Error(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[%s] ERROR ", time.Now().Format(time.TimeOnly))
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// ─── Matching ────────────────────────────────────────────────────────────────

// matchesAny reports whether s matches any entry in the list. Matching is
// case-insensitive substring.
func matchesAny(s string, list []string) bool {
	lower := strings.ToLower(s)
	for _, entry := range list {
		if strings.Contains(lower, strings.ToLower(entry)) {
			return true
		}
	}
	return false
}

// ─── Actions ─────────────────────────────────────────────────────────────────

type Result struct {
	Device Device
	Err    error
}

// disconnectDevice attempts to disconnect a single device with retries and
// exponential backoff.
func disconnectDevice(log *Logger, dev Device, cfg Config) error {
	delay := time.Duration(cfg.RetryDelay) * time.Second

	var lastErr error
	for attempt := 1; attempt <= cfg.RetryCount; attempt++ {
		log.Info("  → disconnect %s (%s) [attempt %d/%d]",
			dev.Name, dev.FriendlyAddress, attempt, cfg.RetryCount)

		_, err := runCommand("blueutil", "--disconnect", dev.Address)
		if err == nil {
			return nil
		}
		lastErr = err

		if attempt < cfg.RetryCount {
			jitter := time.Duration(rand.Int63n(int64(delay) / 2))
			sleep := delay + jitter
			log.Warn("  ✗ attempt %d failed for %s; retrying in %v: %v",
				attempt, dev.FriendlyAddress, sleep, err)
			time.Sleep(sleep)
			delay *= 2
			if delay > 10*time.Second {
				delay = 10 * time.Second
			}
		}
	}
	return fmt.Errorf("disconnect failed after %d attempts: %w", cfg.RetryCount, lastErr)
}

func doSleep(log *Logger, cfg Config, dryRun bool) error {
	devices, err := getConnectedDevices()
	if err != nil {
		return fmt.Errorf("listing connected devices: %w", err)
	}

	if len(devices) == 0 {
		log.Info("No connected Bluetooth devices found.")
	} else {
		// Filter devices
		var toDisconnect []Device
		for _, dev := range devices {
			// Blacklist check
			if matchesAny(dev.FriendlyAddress, cfg.Blacklist) || matchesAny(dev.Name, cfg.Blacklist) {
				log.Info("  • skipping (blacklisted) %s (%s)", dev.Name, dev.FriendlyAddress)
				continue
			}
			// Whitelist check
			if len(cfg.Whitelist) > 0 &&
				!matchesAny(dev.FriendlyAddress, cfg.Whitelist) &&
				!matchesAny(dev.Name, cfg.Whitelist) {
				log.Info("  • skipping (not in whitelist) %s (%s)", dev.Name, dev.FriendlyAddress)
				continue
			}
			toDisconnect = append(toDisconnect, dev)
		}

		if len(toDisconnect) == 0 {
			log.Info("No devices to disconnect (all filtered).")
		} else {
			log.Info("Disconnecting %d device(s)…", len(toDisconnect))

			if dryRun {
				for _, dev := range toDisconnect {
					fmt.Printf("  [dry-run] would disconnect %s (%s)\n", dev.Name, dev.FriendlyAddress)
				}
			} else {
				// Parallel disconnection
				var wg sync.WaitGroup
				results := make(chan Result, len(toDisconnect))

				for _, dev := range toDisconnect {
					wg.Add(1)
					go func(d Device) {
						defer wg.Done()
						err := disconnectDevice(log, d, cfg)
						results <- Result{Device: d, Err: err}
					}(dev)
				}

				wg.Wait()
				close(results)

				var disconnects, failures int
				for r := range results {
					if r.Err != nil {
						log.Error("✗ %s (%s): %v", r.Device.Name, r.Device.FriendlyAddress, r.Err)
						failures++
					} else {
						log.Info("✓ %s (%s) disconnected", r.Device.Name, r.Device.FriendlyAddress)
						disconnects++
					}
				}
				log.Info("Disconnected %d/%d device(s).", disconnects, disconnects+failures)
			}
		}
	}

	// Power off Bluetooth
	if cfg.PowerOffBT != nil && *cfg.PowerOffBT {
		if dryRun {
			fmt.Println("  [dry-run] would power off Bluetooth")
		} else {
			log.Info("Powering off Bluetooth…")
			if _, err := runCommand("blueutil", "--power", "0"); err != nil {
				return fmt.Errorf("powering off Bluetooth: %w", err)
			}
			log.Info("Bluetooth powered off.")
		}
	} else {
		log.Info("Skipping Bluetooth power-off (disabled by config).")
	}

	return nil
}

func doWake(log *Logger, cfg Config, dryRun bool) error {
	if dryRun {
		fmt.Println("  [dry-run] would power on Bluetooth")
		return nil
	}
	log.Info("Powering on Bluetooth…")
	if _, err := runCommand("blueutil", "--power", "1"); err != nil {
		return fmt.Errorf("powering on Bluetooth: %w", err)
	}
	log.Info("Bluetooth powered on.")
	return nil
}

// ─── Install / Uninstall ─────────────────────────────────────────────────────

const sleepScript = `#!/bin/bash
# Auto-generated by bt-sleepd
PATH=%s:%s
{
  echo "[$(date)] sleep triggered"
  %s --sleep
  echo "[$(date)] exit=$?"
} >> /tmp/bt-sleepd.log 2>&1
`

const wakeScript = `#!/bin/bash
# Auto-generated by bt-sleepd
PATH=%s:%s
{
  echo "[$(date)] wakeup triggered"
  %s --wake
  echo "[$(date)] bluetooth re-enabled"
} >> /tmp/bt-sleepd.log 2>&1
`

func doInstall(log *Logger, binaryPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}

	// Find blueutil path
	btPath, err := exec.LookPath("blueutil")
	if err != nil {
		return fmt.Errorf("blueutil not found in PATH; install with `brew install blueutil`: %w", err)
	}
	btDir := filepath.Dir(btPath)

	// Determine PATH prefix: the dir containing the binary + blueutil dir
	binDir := filepath.Dir(binaryPath)
	pathPrefix := btDir + ":" + binDir

	// Write ~/.sleep
	sleepContent := fmt.Sprintf(sleepScript, pathPrefix, "/usr/bin:/bin", binaryPath)
	sleepPath := filepath.Join(home, ".sleep")
	if err := os.WriteFile(sleepPath, []byte(sleepContent), 0o755); err != nil {
		return fmt.Errorf("writing %s: %w", sleepPath, err)
	}
	log.Info("Created %s", sleepPath)

	// Write ~/.wakeup
	wakeContent := fmt.Sprintf(wakeScript, btDir, "/usr/bin:/bin", binaryPath)
	wakePath := filepath.Join(home, ".wakeup")
	if err := os.WriteFile(wakePath, []byte(wakeContent), 0o755); err != nil {
		return fmt.Errorf("writing %s: %w", wakePath, err)
	}
	log.Info("Created %s", wakePath)

	fmt.Println()
	fmt.Println("✅ Installed! Start sleepwatcher if not already running:")
	fmt.Println()
	fmt.Println("   brew services start sleepwatcher")
	fmt.Println()
	fmt.Println("Or restart it to pick up changes:")
	fmt.Println()
	fmt.Println("   brew services restart sleepwatcher")
	return nil
}

func doUninstall(log *Logger) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}

	removed := false
	for _, name := range []string{".sleep", ".wakeup"} {
		path := filepath.Join(home, name)
		if _, err := os.Stat(path); err == nil {
			if err := os.Remove(path); err != nil {
				log.Error("failed to remove %s: %v", path, err)
			} else {
				log.Info("Removed %s", path)
				removed = true
			}
		}
	}
	if !removed {
		log.Info("No scripts found to remove (~/.sleep, ~/.wakeup).")
	}
	fmt.Println()
	fmt.Println("Done. Stop sleepwatcher if you no longer need it:")
	fmt.Println()
	fmt.Println("   brew services stop sleepwatcher")
	return nil
}

// ─── Config init ─────────────────────────────────────────────────────────────

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

// ─── Main ────────────────────────────────────────────────────────────────────

func main() {
	// Determine the binary path early (before flags so --install can use it).
	execPath, _ := os.Executable()
	// Resolve symlinks so we get the real path.
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}

	// Flags
	sleepMode := flag.Bool("sleep", false, "Run sleep handler (disconnect devices + power off BT)")
	wakeMode := flag.Bool("wake", false, "Run wake handler (power on BT)")
	dryRun := flag.Bool("dry-run", false, "Preview actions without making changes")
	verbose := flag.Bool("verbose", false, "Verbose output")
	configPath := flag.String("config", "", "Path to config file (default: ~/.config/bt-sleepd/config.json)")
	install := flag.Bool("install", false, "Create ~/.sleep and ~/.wakeup scripts for sleepwatcher")
	uninstall := flag.Bool("uninstall", false, "Remove ~/.sleep and ~/.wakeup scripts")
	initConfig := flag.Bool("init-config", false, "Write a default config file")
	force := flag.Bool("force", false, "Overwrite existing files when used with --init-config")
	listDevices := flag.Bool("list", false, "List currently connected Bluetooth devices")
	version := flag.Bool("version", false, "Print version and exit")
	showConfig := flag.Bool("show-config", false, "Print the effective configuration and exit")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: bt-sleepd [flags]

A tool that disconnects Bluetooth devices and powers the radio off/on during
macOS sleep/wake events.

Modes (default: —sleep if neither —sleep nor —wake is given):
  —sleep      Disconnect devices and optionally power off Bluetooth
  —wake       Power Bluetooth back on

Informational:
  —list           Show connected Bluetooth devices and exit
  —show-config    Print effective configuration and exit
  —version        Print version and exit

Installation:
  —install        Create ~/.sleep and ~/.wakeup scripts for sleepwatcher
  —uninstall      Remove ~/.sleep and ~/.wakeup scripts
  —init-config    Write a default config file to ~/.config/bt-sleepd/config.json

Flags:
  —dry-run        Preview actions without making changes
  —verbose        Verbose (includes timestamps and per-device progress)
  —config <path>  Path to config file
  —force          Overwrite existing files (used with —init-config)
  —help           Show this help

`)
	}
	flag.Parse()

	log := &Logger{verbose: *verbose}

	// ── Version ───────────────────────────────────────────────────────────
	if *version {
		fmt.Println("bt-sleepd v0.2.0")
		return
	}

	// ── Install / Uninstall ───────────────────────────────────────────────
	if *install {
		if err := doInstall(log, execPath); err != nil {
			log.Error("%v", err)
			os.Exit(1)
		}
		return
	}
	if *uninstall {
		if err := doUninstall(log); err != nil {
			log.Error("%v", err)
			os.Exit(1)
		}
		return
	}

	// ── Init config ───────────────────────────────────────────────────────
	if *initConfig {
		if err := doInitConfig(log, *force); err != nil {
			log.Error("%v", err)
			os.Exit(1)
		}
		return
	}

	// ── Resolve config path ──────────────────────────────────────────────
	home := homeDir()
	if *configPath == "" && home != "" {
		*configPath = filepath.Join(home, configDir, configFile)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Warn("Config load error (using defaults): %v", err)
		cfg = defaultConfig()
	}

	// ── Show config ───────────────────────────────────────────────────────
	if *showConfig {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(cfg)
		if *configPath != "" {
			fmt.Fprintf(os.Stderr, "# config source: %s\n", *configPath)
		} else {
			fmt.Fprintf(os.Stderr, "# defaults (no config file)\n")
		}
		return
	}

	// ── List devices ──────────────────────────────────────────────────────
	if *listDevices {
		devices, err := getConnectedDevices()
		if err != nil {
			log.Error("%v", err)
			os.Exit(1)
		}
		if len(devices) == 0 {
			fmt.Println("No connected Bluetooth devices.")
			return
		}
		fmt.Printf("%-20s  %s\n", "Address", "Name")
		fmt.Println(strings.Repeat("─", 40))
		for _, dev := range devices {
			name := dev.Name
			if name == "" {
				name = "(unnamed)"
			}
			fmt.Printf("%-20s  %s\n", dev.FriendlyAddress, name)
		}
		return
	}

	// ── Determine mode ───────────────────────────────────────────────────
	// If neither —sleep nor —wake is given, default to —sleep.
	modeSleep := *sleepMode
	modeWake := *wakeMode
	if !modeSleep && !modeWake {
		modeSleep = true
	}

	// ── Execute ───────────────────────────────────────────────────────────
	if modeSleep {
		log.Info("Sleep handler started")
		if err := doSleep(log, cfg, *dryRun); err != nil {
			log.Error("%v", err)
			os.Exit(1)
		}
		log.Info("Sleep handler completed.")
	}

	if modeWake {
		log.Info("Wake handler started")
		if err := doWake(log, cfg, *dryRun); err != nil {
			log.Error("%v", err)
			os.Exit(1)
		}
		log.Info("Wake handler completed.")
	}
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err == nil {
		return home
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return ""
}

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

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

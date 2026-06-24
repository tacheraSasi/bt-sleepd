package main

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// Result captures the outcome of a single device disconnection attempt.
type Result struct {
	Device Device
	Err    error
}

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

// doSleep disconnects connected Bluetooth devices (respecting blacklist /
// whitelist) and optionally powers off the Bluetooth radio.
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
			if matchesAny(dev.FriendlyAddress, cfg.Blacklist) || matchesAny(dev.Name, cfg.Blacklist) {
				log.Info("  • skipping (blacklisted) %s (%s)", dev.Name, dev.FriendlyAddress)
				continue
			}
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

// doWake powers the Bluetooth radio back on after sleep.
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

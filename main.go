package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error: %v\noutput: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

func getConnectedDevices() ([]string, error) {
	output, err := runCommand("blueutil", "--connected", "--format", "json-pretty")
	if err != nil {
		return nil, err
	}
	if output == "" || output == "[]" {
		return nil, nil
	}
	// Parse addresses from JSON output lines containing "address"
	var addresses []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, `"address"`) {
			// Extract value: "address" : "xx-xx-xx-xx-xx-xx"
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				addr := strings.TrimSpace(parts[1])
				addr = strings.Trim(addr, `",`)
				addr = strings.TrimSpace(addr)
				if addr != "" {
					addresses = append(addresses, addr)
				}
			}
		}
	}
	return addresses, nil
}

func main() {
	fmt.Println("Mac is going to sleep. Disconnecting Bluetooth devices...")

	// Get list of connected devices
	devices, err := getConnectedDevices()
	if err != nil {
		fmt.Println("Failed to list connected devices:", err)
	}

	// Disconnect each device
	for _, addr := range devices {
		fmt.Printf("Disconnecting %s...\n", addr)
		_, err := runCommand("blueutil", "--disconnect", addr)
		if err != nil {
			fmt.Printf("Failed to disconnect %s: %v\n", addr, err)
		}
	}

	// Turn off Bluetooth completely
	_, err = runCommand("blueutil", "--power", "0")
	if err != nil {
		fmt.Println("Failed to turn off Bluetooth:", err)
	}

	fmt.Println("Done.")
}

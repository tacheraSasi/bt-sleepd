package main

import (
	"fmt"
	"os/exec"
)

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error: %v\noutput: %s", err, string(output))
	}
	fmt.Println(string(output))
	return nil
}

func main() {
	fmt.Println("Mac is going to sleep. Disconnecting Bluetooth devices...")

	// Option 1: Disconnect all devices
	err := runCommand("blueutil", "--disconnect")
	if err != nil {
		fmt.Println("Failed to disconnect devices:", err)
	}

	// Option 2 (alternative): WE Turn off Bluetooth completely
	// err := runCommand("blueutil", "--power", "0")
	// if err != nil {
	// 	fmt.Println("Failed to turn off Bluetooth:", err)
	// }

	fmt.Println("Done.")
}
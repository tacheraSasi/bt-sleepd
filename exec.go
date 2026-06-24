package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// runCommand runs an external command and returns its trimmed combined output.
func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %v failed: %w\noutput: %s", name, args, err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

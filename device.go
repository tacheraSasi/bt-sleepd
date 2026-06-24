package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Device represents a Bluetooth device as reported by blueutil.
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

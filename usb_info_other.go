//go:build !windows && !linux

package main

func lookupNativeUSB(string) (*usbPortInfo, error) {
	// Do not guess CDC from an OS port name on unsupported platforms.
	return nil, nil
}

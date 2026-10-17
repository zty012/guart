package main

import (
	"path/filepath"

	"go.bug.st/serial/enumerator"
)

func lookupNativeUSB(port string) (*usbPortInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	for _, p := range ports {
		if p == nil || p.Name != port || !p.IsUSB {
			continue
		}
		info := &usbPortInfo{Port: p.Name, VID: p.VID, PID: p.PID, Serial: p.SerialNumber, Product: p.Product, Manufacturer: p.Manufacturer}
		device, err := filepath.EvalSymlinks(filepath.Join("/sys/class/tty", filepath.Base(port), "device"))
		if err != nil {
			return nil, err
		}
		for dir := device; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
			if driver, err := filepath.EvalSymlinks(filepath.Join(dir, "driver")); err == nil {
				info.Driver = filepath.Base(driver)
				if info.Driver == "cdc_acm" {
					info.CDC = true
					break
				}
			}
		}
		return info, nil
	}
	return nil, nil
}

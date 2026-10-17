package main

import (
	"strings"

	"go.bug.st/serial/enumerator"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func lookupNativeUSB(port string) (*usbPortInfo, error) {
	// No active USB probing: use only OS-cached metadata.
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	var info *usbPortInfo
	for _, p := range ports {
		if p != nil && strings.EqualFold(p.Name, port) && p.IsUSB {
			info = &usbPortInfo{Port: p.Name, VID: p.VID, PID: p.PID, Serial: p.SerialNumber, Product: p.Product, Manufacturer: p.Manufacturer}
			break
		}
	}
	if info == nil {
		return nil, nil
	}
	guids, err := windows.SetupDiClassGuidsFromNameEx("Ports", "")
	if err != nil {
		return nil, err
	}
	for _, guid := range guids {
		set, err := windows.SetupDiGetClassDevsEx(&guid, "", 0, windows.DIGCF_PRESENT, 0, "")
		if err != nil {
			return nil, err
		}
		found := inspectCDCDevice(set, port, info)
		windows.SetupDiDestroyDeviceInfoList(set)
		if found {
			return info, nil
		}
	}
	return nil, nil
}

func inspectCDCDevice(set windows.DevInfo, port string, info *usbPortInfo) bool {
	for i := 0; ; i++ {
		dev, err := windows.SetupDiEnumDeviceInfo(set, i)
		if err != nil {
			return false
		}
		h, err := windows.SetupDiOpenDevRegKey(set, dev, windows.DICS_FLAG_GLOBAL, 0, windows.DIREG_DEV, windows.KEY_READ)
		if err != nil {
			continue
		}
		key := registry.Key(h)
		name, _, _ := key.GetStringValue("PortName")
		key.Close()
		if !strings.EqualFold(name, port) {
			continue
		}
		value, _ := windows.SetupDiGetDeviceRegistryProperty(set, dev, windows.SPDRP_SERVICE)
		info.Driver, _ = value.(string)
		info.CDC = strings.EqualFold(info.Driver, "usbser")
		value, _ = windows.SetupDiGetDeviceRegistryProperty(set, dev, windows.SPDRP_COMPATIBLEIDS)
		if ids, ok := value.([]string); ok {
			for _, id := range ids {
				if strings.Contains(strings.ToUpper(id), "USB\\CLASS_02&SUBCLASS_02") {
					info.CDC = true
				}
			}
		}
		return true
	}
}

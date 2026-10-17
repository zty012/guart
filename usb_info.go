package main

import (
	"fmt"
	"strings"
)

type usbPortInfo struct {
	Port, VID, PID, Serial, Manufacturer, Product, Driver string
	CDC                                                   bool
}

// Only confirmed CDC interfaces that are not known bridges/debug probes are
// eligible. USB-UART adapters are intentionally treated like native UARTs.
func (p *usbPortInfo) eligible() bool {
	if p == nil || !p.CDC {
		return false
	}
	id := strings.ToUpper(p.VID + ":" + p.PID)
	if bridgeUSBIDs[id] || strings.EqualFold(p.VID, "1366") {
		return false
	}
	name := strings.ToUpper(p.Product)
	for _, marker := range []string{"USB-TO-UART", "USB TO UART", "USB-TO-SERIAL", "USB TO SERIAL", "USB SERIAL CONVERTER", "USB BRIDGE",
		"ST-LINK", "STLINK", "J-LINK", "JLINK", "DAPLINK", "CMSIS-DAP", "EDBG", "LPC-LINK", "MCU-LINK",
		"CP210", "FT232", "FT2232", "CH340", "CH341", "PL2303", "MCP2221"} {
		if strings.Contains(name, marker) {
			return false
		}
	}
	return true
}

var bridgeUSBIDs = map[string]bool{
	"0403:6001": true, "0403:6010": true, "0403:6011": true, "0403:6014": true, "0403:6015": true,
	"10C4:EA60": true, "10C4:EA70": true, "10C4:EA71": true, "067B:2303": true,
	"1A86:7523": true, "1A86:5523": true, "1A86:55D4": true, "1A86:55D3": true, "04D8:00DD": true,
	"0483:3744": true, "0483:3748": true, "0483:374B": true, "0483:374D": true, "0483:374E": true, "0483:374F": true, "0483:3752": true, "0483:3753": true, "0483:7270": true,
	"0D28:0204": true, "03EB:2111": true, "03EB:204B": true, "1FC9:0143": true, "303A:1002": true,
	"2E8A:0004": true, "2E8A:000C": true,
	"2341:0001": true, "2341:0010": true, "2341:003B": true, "2341:003D": true, "2341:003F": true, "2341:0042": true, "2341:0043": true, "2341:0044": true, "2341:0045": true,
	"2A03:0001": true, "2A03:0010": true, "2A03:003D": true, "2A03:0042": true, "2A03:0043": true,
}

type usbMCURule struct {
	Model, Family, Vendor, Core, Note string
}

// IDs are firmware-configurable, so even model-specific IDs remain hints.
// Shared IDs deliberately name only a family (e.g. RP MicroPython spans chips).
var usbMCURules = map[string]usbMCURule{
	"303A:1001": {"ESP32", "ESP32", "Espressif / 乐鑫", "依具体型号而定", "内置 USB Serial/JTAG，共享于多个 ESP32 型号"},
	"0483:5740": {"STM32", "STM32", "STMicroelectronics / 意法半导体", "依具体系列而定", "通用 STM32 Virtual COM Port，不能确定具体型号"},
	"0483:572A": {"STM32F401", "STM32", "STMicroelectronics / 意法半导体", "Arm Cortex-M4", "STM32F401 CDC/ACM 默认 ID"},
	"2E8A:000A": {"RP2040", "RP", "Raspberry Pi", "双核 Arm Cortex-M0+", "Pico SDK RP2040 CDC 默认 ID"},
	"2E8A:0009": {"RP", "RP", "Raspberry Pi", "依具体型号而定", "Pico SDK 非 RP2040 CDC 默认 ID"},
	"2E8A:0005": {"RP", "RP", "Raspberry Pi", "依具体型号而定", "RP2 MicroPython 共享 ID，可能为 RP2040 / RP2350"},
	"2341:8036": {"ATMEGA32U4", "ATmega", "Microchip / Atmel", "AVR 8-bit", "Arduino Leonardo 原生 USB"},
	"2341:0036": {"ATMEGA32U4", "ATmega", "Microchip / Atmel", "AVR 8-bit", "Arduino Leonardo Bootloader"},
	"2341:8037": {"ATMEGA32U4", "ATmega", "Microchip / Atmel", "AVR 8-bit", "Arduino Micro 原生 USB"},
	"2341:0037": {"ATMEGA32U4", "ATmega", "Microchip / Atmel", "AVR 8-bit", "Arduino Micro Bootloader"},
	"2A03:8036": {"ATMEGA32U4", "ATmega", "Microchip / Atmel", "AVR 8-bit", "Arduino Leonardo 原生 USB"},
	"239A:801E": {"ATSAMD21E18A", "SAM", "Microchip / Atmel", "Arm Cortex-M0+", "Adafruit Trinket M0"},
	"03EB:6119": {"SAM", "SAM", "Microchip / Atmel", "依具体系列而定", "AT91SAM CDC Demo 共享 ID"},
	"03EB:6124": {"SAM", "SAM", "Microchip / Atmel", "依具体系列而定", "SAM-BA 共享 ID，不能确定具体型号"},
	"04D8:000B": {"PIC18F2550", "PIC", "Microchip", "PIC18 8-bit", "PIC18F2550 默认 ID"},
	"1FC9:0003": {"LPC1343", "LPC", "NXP", "Arm Cortex-M3", "LPC1343 默认 ID"},
}

func (p *usbPortInfo) mcuHint() (mcuCandidate, bool) {
	if !p.eligible() {
		return mcuCandidate{}, false
	}
	id := strings.ToUpper(p.VID + ":" + p.PID)
	rule, ok := usbMCURules[id]
	if !ok {
		// Vendor-only mappings are deliberately limited to known MCU families.
		switch strings.ToUpper(p.VID) {
		case "303A":
			rule = usbMCURule{"ESP32", "ESP32", "Espressif / 乐鑫", "依具体型号而定", "原生 USB 厂商线索，PID 尚未收录"}
		case "1915":
			rule = usbMCURule{"NRF", "nRF", "Nordic Semiconductor", "依具体系列而定", "原生 USB 厂商线索，不能确定具体型号"}
		default:
			return mcuCandidate{}, false
		}
	}
	return mcuCandidate{Model: rule.Model, Family: rule.Family, Vendor: rule.Vendor, Core: rule.Core, Strength: 2,
		Evidence: "USB " + id + "：" + rule.Note}, true
}

func (a *app) loadUSBInfo(port string, generation uint64) {
	if a.lookupUSB == nil || a.post == nil {
		return
	}
	lookup, post := a.lookupUSB, a.post
	go func() {
		info, err := lookup(port)
		if info != nil {
			copy := *info
			copy.VID, copy.PID = strings.ToUpper(copy.VID), strings.ToUpper(copy.PID)
			info = &copy
		}
		post(func() {
			if a.closed || a.generation != generation || a.active == nil || a.demo {
				return
			}
			if err != nil {
				a.usbError = fmt.Sprintf("USB 元数据不可用：%v（继续按 RX 推断）", err)
				return
			}
			if info.eligible() {
				a.usb = info
			}
		})
	}()
}

func (a *app) mcuResults() []mcuCandidate {
	d := a.mcu
	d.candidates = append([]mcuCandidate(nil), a.mcu.candidates...)
	if hint, ok := a.usb.mcuHint(); ok {
		d.remember(hint)
	}
	return d.results()
}

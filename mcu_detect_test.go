package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/core"
	"github.com/egoist/mygo/ui"
)

func TestMCUModelMarkers(t *testing.T) {
	for _, tc := range []struct {
		text, model, vendor, core string
		strength                  int
	}{
		{"ESP-ROM:esp32s3-20210327", "ESP32-S3", "Espressif", "Xtensa LX7", 3},
		{"ESP-ROM:esp32c3-api1-20210207", "ESP32-C3", "Espressif", "RISC-V", 3},
		{"ESP-ROM:esp32-s3-20210327", "ESP32-S3", "Espressif", "Xtensa LX7", 3},
		{"chip: ESP32-C3", "ESP32-C3", "Espressif", "RISC-V", 3},
		{"ESP32", "ESP32", "Espressif", "依具体", 2},
		{"ESP8266EX", "ESP8266EX", "Espressif", "L106", 3},
		{"MCU=stm32f103c8t6", "STM32F103C8T6", "STMicroelectronics", "Cortex-M3", 3},
		{"STM32F4", "STM32F4", "STMicroelectronics", "Cortex-M4", 2},
		{"GD32VF103CBT6", "GD32VF103CBT6", "GigaDevice", "RISC-V", 3},
		{"CH32V003F4P6", "CH32V003F4P6", "WCH", "RISC-V", 3},
		{"nRF52840", "NRF52840", "Nordic", "Cortex-M4", 3},
		{"nRF5340", "NRF5340", "Nordic", "依具体", 3},
		{"nRF54L15", "NRF54L15", "Nordic", "依具体", 3},
		{"RP2040", "RP2040", "Raspberry", "Cortex-M0+", 3},
		{"RP2350A", "RP2350A", "Raspberry", "Hazard3", 3},
		{"ATmega328P", "ATMEGA328P", "Microchip", "AVR", 3},
		{"ATtiny85", "ATTINY85", "Microchip", "AVR", 3},
		{"LPC1768", "LPC1768", "NXP", "依具体", 3},
	} {
		t.Run(tc.text, func(t *testing.T) {
			var d mcuDetector
			d.feed([]byte(tc.text))
			got := d.results()
			if tc.model == "" {
				if len(got) != 0 {
					t.Fatalf("unexpected match: %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Model != tc.model || !strings.Contains(got[0].Vendor, tc.vendor) ||
				!strings.Contains(got[0].Core, tc.core) || got[0].Strength != tc.strength || got[0].Evidence == "" {
				t.Fatalf("candidate: %+v", got)
			}
		})
	}
}

func TestMCUStreamBoundariesAndANSI(t *testing.T) {
	data := []byte("\x1b[32mMCU: STM\x1b[1m32F103C8T6\x1b[0m\r\n")
	for split := 0; split <= len(data); split++ {
		var d mcuDetector
		d.feed(data[:split])
		d.results() // Rendering partial tokens must not commit them.
		d.feed(data[split:])
		got := d.results()
		if len(got) != 1 || got[0].Model != "STM32F103C8T6" || d.bytes != uint64(len(data)) {
			t.Fatalf("split %d: %+v", split, got)
		}
	}
	var d mcuDetector
	for _, ch := range []byte("ESP-ROM:esp32-c3-20200918\r\n") {
		d.feed([]byte{ch})
		d.results()
	}
	if got := d.results(); len(got) != 1 || got[0].Model != "ESP32-C3" {
		t.Fatalf("bytewise ROM banner: %+v", got)
	}
}

func TestMCUUnknownAndBoundaries(t *testing.T) {
	for _, text := range []string{
		"OK\r\nAT version:1.7\r\nready\r\n", "FreeRTOS Zephyr Arduino Cortex-M4 CH340 CP2102",
		"NOTSTM32F103C8T6 ESP32-C99 RP20400 ATMEGA328P_TEST",
		"\x1b]0;STM32F103C8T6\ahello", "STM\x0032F103C8T6", "STM\r32F103C8T6",
		string([]byte{0x79, 0x1f, 0x00, 0xaa, 0x55}),
	} {
		var d mcuDetector
		d.feed([]byte(text))
		if got := d.results(); len(got) != 0 {
			t.Fatalf("%q produced false candidate: %+v", text, got)
		}
	}
}

func TestMCUBootEvidenceAndRefinement(t *testing.T) {
	var d mcuDetector
	for _, ch := range []byte("ets Jan  8 2013,rst cause:2\r\n") {
		d.feed([]byte{ch})
	}
	if got := d.results(); len(got) != 1 || got[0].Model != "ESP8266" || got[0].Strength != 1 {
		t.Fatalf("boot evidence: %+v", got)
	}
	d.feed([]byte("ESP8266EX\n"))
	if got := d.results(); len(got) != 1 || got[0].Model != "ESP8266EX" || got[0].Strength != 3 {
		t.Fatalf("refinement: %+v", got)
	}
	for i := 0; i < 10; i++ {
		d.feed([]byte("ESP8266EX\n"))
	}
	if len(d.candidates) != 2 || len(d.results()) != 1 {
		t.Fatal("repeated evidence added candidates")
	}
	for _, text := range []string{"ets Jun  8 2016 00:22:57\r\n", "ets Jul 29 2019 12:21:46\r\n"} {
		d = mcuDetector{}
		for _, ch := range []byte(text) {
			d.feed([]byte{ch})
		}
		if got := d.results(); len(got) != 1 || got[0].Model != "ESP32" || got[0].Strength != 1 {
			t.Fatalf("ESP32 boot signature %q: %+v", text, got)
		}
	}
}

func TestMCUConflictsAndFamilyRefinement(t *testing.T) {
	var d mcuDetector
	d.feed([]byte("STM32 STM32F1 STM32F103 STM32F103C8T6\n"))
	if got := d.results(); len(got) != 1 || got[0].Model != "STM32F103C8T6" {
		t.Fatalf("family refinement: %+v", got)
	}
	d.feed([]byte("ESP32-C3\nSTM32F407VGT6\n"))
	if got := d.results(); len(got) != 3 {
		t.Fatalf("conflicting models were hidden: %+v", got)
	}
	d = mcuDetector{}
	d.feed([]byte("ATmega32 ATmega328\n"))
	if got := d.results(); len(got) != 2 {
		t.Fatalf("distinct AVR models merged: %+v", got)
	}
	for _, text := range []string{"STM32F4 STM32F103C8T6\n", "NRF51 NRF52840\n"} {
		d = mcuDetector{}
		d.feed([]byte(text))
		if got := d.results(); len(got) != 2 {
			t.Fatalf("unrelated family/series hidden for %q: %+v", text, got)
		}
	}
}

func TestMCUBoundedState(t *testing.T) {
	var d mcuDetector
	d.feed(bytes.Repeat([]byte("A"), 65536))
	d.feed([]byte("STM32F103C8T6\n"))
	if len(d.results()) != 0 {
		t.Fatal("matched suffix of oversized token")
	}
	for i := 0; i < 100; i++ {
		d.feed([]byte(fmt.Sprintf("ATMEGA%d\n", i)))
	}
	if len(d.candidates) != maxMCUCandidates || !d.limited || len(d.token) > 64 || len(d.bootTail) > 32 {
		t.Fatalf("unbounded state: %+v", d)
	}
	d.feed([]byte("\x1b[" + strings.Repeat(";", 4096)))
	if len(d.ansi.pending) > maxANSIPending {
		t.Fatal("unbounded ANSI state")
	}
}

func TestMCUAppOnlyAnalyzesRXAndSurvivesDisplayChanges(t *testing.T) {
	a := newApp()
	p := newStubPort()
	a.active = newSession(p)
	defer a.active.stop()
	a.handleEvent(serialEvent{Direction: "TX", Data: []byte("RP2040\n")})
	a.addPacket(packet{Direction: "SYS", Note: "STM32F103C8T6"})
	if len(a.mcu.results()) != 0 {
		t.Fatal("TX/system text participated in detection")
	}
	a.paused, a.pauseID, a.search, a.direction, a.displayMode = true, a.nextID, "absent", "TX", "HEX"
	data := []byte("ESP32-C3\r\n")
	a.handleEvent(serialEvent{Direction: "RX", Data: data})
	if got := a.mcu.results(); len(got) != 1 || got[0].Model != "ESP32-C3" || !bytes.Equal(a.packets[len(a.packets)-1].Data, data) {
		t.Fatalf("paused/filtered detection or raw data changed: %+v", got)
	}
	a.clearLog()
	if len(a.mcu.results()) != 1 || a.mcu.bytes != uint64(len(data)) {
		t.Fatal("clearing display reset connection inference")
	}
	select {
	case <-a.active.writes:
		t.Fatal("inference sent a probe")
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.written) != 0 {
		t.Fatal("inference wrote to the device")
	}
	a.disconnect(false)
	if len(a.mcu.results()) != 0 || a.mcu.bytes != 0 {
		t.Fatal("disconnected device inference leaked")
	}
}

func TestMCUNewConnectionAndDemoIsolation(t *testing.T) {
	a := newApp()
	a.lookupUSB = nil
	queue := make(chan func(), 16)
	a.post = func(fn func()) { queue <- fn }
	a.config.Port = "COM3"
	a.openPort = func(portConfig) (transport, error) { return newStubPort(), nil }
	for _, demo := range []bool{false, true} {
		a.mcu.feed([]byte("RP2040\n"))
		a.connect(demo)
		select {
		case fn := <-queue:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatal("connection did not complete")
		}
		if a.active == nil || len(a.mcu.results()) != 0 {
			t.Fatal("new connection retained old inference")
		}
		a.handleEvent(serialEvent{Direction: "RX", Data: []byte("ESP32-S3\n")})
		if got := a.mcu.results(); demo && len(got) != 0 || !demo && len(got) != 1 {
			t.Fatalf("demo=%t candidates=%+v", demo, got)
		}
		a.disconnect(false)
	}
}

func TestMCUPanelLayoutAndLifetimes(t *testing.T) {
	for _, size := range [][2]int{{1320, 900}, {960, 720}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			a := newApp()
			tt := ui.NewTester(a.view, size[0], size[1])
			if !tt.HasText("等待连接") || !tt.HasText("MCU 识别") {
				t.Fatal("inference panel is not always visible")
			}
			a.active = newSession(newStubPort())
			defer a.active.stop()
			tt.Frame()
			if !tt.HasText("尚未识别") {
				t.Fatal("unknown device state missing")
			}
			a.handleEvent(serialEvent{Direction: "RX", Data: []byte("MCU: STM32F103C8T6\r\n")})
			for i := 0; i < 3; i++ {
				tt.Frame()
			}
			panel, ok := tt.Find("MCU 识别")
			if !ok || panel.X < float32(size[0])-mcuTestPanelWidth(size[0]) || !tt.HasText("RX 型号标记：STM32F103C8T6") {
				t.Fatalf("right panel misplaced or missing evidence: %+v", panel)
			}
			for _, label := range []string{"发送", "搜索消息", "接收分组", "接收空闲间隔", "AT", "HELP", "INFO"} {
				r, ok := tt.Find(label)
				if !ok || r.W <= 0 || r.Y+r.H > float32(size[1]) || r.X+r.W > float32(size[0])-mcuTestPanelWidth(size[0]) {
					t.Fatalf("center control %s clipped by panel: %+v", label, r)
				}
			}
			a.handleEvent(serialEvent{Direction: "RX", Data: []byte("ESP32-C3\n")})
			tt.Frame()
			if !tt.HasText("线索冲突 · 无法唯一确定") {
				t.Fatal("conflict was not disclosed")
			}
			for i := 0; i < 3; i++ {
				a.demo = true
				tt.Frame()
				a.demo = false
				tt.Frame()
			}
			// A maximally widened connection sidebar must still leave a usable monitor.
			a.sidebar.Size = 1000
			tt.Frame()
			if r, ok := tt.Find("发送"); !ok || r.X+r.W > float32(size[0])-mcuTestPanelWidth(size[0]) {
				t.Fatalf("resized sidebar clipped the send control: %+v", r)
			}
			a.disconnect(false)
			tt.Frame()
			if !tt.HasText("等待连接") || tt.HasText("RX 型号标记：STM32F103C8T6") {
				t.Fatal("panel retained disconnected device information")
			}
		})
	}
}

func mcuTestPanelWidth(width int) float32 {
	if width < 1200 {
		return 216
	}
	return 248
}

func TestRenderMCUScreenshots(t *testing.T) {
	dir := os.Getenv("GUART_SCREENSHOTS")
	if dir == "" {
		t.Skip("GUART_SCREENSHOTS not set")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		w, h int
		mode core.Mode
	}{
		{"guart-mcu-light", 1320, 900, core.Light},
		{"guart-mcu-dark", 1320, 900, core.Dark},
		{"guart-mcu-compact", 960, 720, core.Light},
		{"guart-usb-light", 1320, 900, core.Light},
		{"guart-usb-compact", 960, 720, core.Light},
		{"guart-loopback", 1320, 900, core.Dark},
	} {
		a := newApp()
		a.themeMode, a.config.Port, a.sendText = tc.mode, "COM3", "AT+INFO?"
		a.active = newSession(newStubPort())
		a.connectedAt = time.Now()
		defer a.active.stop()
		a.notice = "界面测试 · 模拟 MCU 启动日志，不连接真实设备"
		a.handleEvent(serialEvent{Direction: "RX", Data: []byte("\x1b[32mMCU: STM32F103C8T6\x1b[0m\r\nFirmware: 1.2.0\r\nSystem ready\r\n")})
		if strings.HasPrefix(tc.name, "guart-usb") {
			a.usb = &usbPortInfo{Port: "COM3", VID: "0483", PID: "5740", Product: "STM32 Virtual COM Port (COM3)", Driver: "usbser", CDC: true}
		}
		if tc.name == "guart-loopback" {
			a.clearLog()
			a.mcu = mcuDetector{}
			at := time.Now()
			a.loopback.arm([]byte("hello\r\n"), "115200", at)
			a.handleEvent(serialEvent{Direction: "TX", Data: []byte("hello\r\n"), Time: at})
			a.handleEvent(serialEvent{Direction: "RX", Data: []byte("hello\r\n"), Time: at.Add(time.Millisecond)})
		}
		tt := ui.NewTester(a.view, tc.w, tc.h)
		for i := 0; i < 4; i++ {
			tt.Frame()
		}
		f, err := os.Create(filepath.Join(dir, tc.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

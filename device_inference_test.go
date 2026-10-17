package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestFirstSendLoopback(t *testing.T) {
	for _, rxFirst := range []bool{false, true} {
		for _, data := range [][]byte{[]byte("hello\r\n"), {0, 0xff, 0xaa, 0x55}, []byte("STM32F103C8T6\r\n")} {
			t.Run(fmt.Sprintf("rxFirst=%t/%x", rxFirst, data), func(t *testing.T) {
				a := newApp()
				a.active = newSession(newStubPort())
				defer a.active.stop()
				at := time.Now()
				a.loopback.arm(data, "115200", at)
				rx := func() {
					for _, ch := range data {
						a.handleEvent(serialEvent{Direction: "RX", Data: []byte{ch}, Time: at.Add(time.Millisecond)})
					}
				}
				tx := func() { a.handleEvent(serialEvent{Direction: "TX", Data: data, Time: at}) }
				if rxFirst {
					rx()
					tx()
				} else {
					tx()
					rx()
				}
				if !a.loopback.detected || len(a.mcu.results()) != 0 || a.mcu.bytes != uint64(len(data)) {
					t.Fatalf("loopback failed or echoed chip misidentified: %+v, %+v", a.loopback, a.mcu.results())
				}
				if a.rx != uint64(len(data)) || a.tx != uint64(len(data)) {
					t.Fatal("loopback detection changed byte statistics")
				}
				a.clearLog()
				if !a.loopback.detected {
					t.Fatal("clear display lost loopback result")
				}
				a.noteFirstSend([]byte("second"), time.Now())
				if !bytes.Equal(a.loopback.payload, data) {
					t.Fatal("second send replaced first send")
				}
				a.disconnect(false)
				if a.loopback.attempted {
					t.Fatal("disconnect retained loopback result")
				}
			})
		}
	}
}

func TestLoopbackMismatchTimeoutAndExtraRX(t *testing.T) {
	at := time.Now()
	for _, tc := range []struct {
		payload, rx string
		late        bool
	}{
		{"STM32F103C8T6\r\n", "STM32F407VGT6\r\n", false},
		{"ABC", "A", true},
		{"ABC\r\n", "ABC\n", false},
		{"ABC", "XYZ", false},
	} {
		var d loopbackDetector
		d.arm([]byte(tc.payload), "115200", at)
		d.transmitted([]byte(tc.payload))
		when := at.Add(time.Millisecond)
		if tc.late {
			when = d.deadline.Add(time.Millisecond)
		}
		var got []byte
		for _, ch := range []byte(tc.rx) {
			got = append(got, d.receive([]byte{ch}, when)...)
		}
		if d.detected || !d.failed || string(got) != tc.rx {
			t.Fatalf("%+v: lost mismatch data=%q state=%+v", tc, got, d)
		}
	}
	var d loopbackDetector
	d.arm([]byte("ABC"), "115200", at)
	d.transmitted([]byte("ABC"))
	got := d.receive([]byte("ABCSTM32F103C8T6\r\n"), at.Add(time.Millisecond))
	if !d.detected || string(got) != "STM32F103C8T6\r\n" {
		t.Fatal("echo prefix swallowed real RX")
	}
	d = loopbackDetector{}
	d.arm([]byte("ABC"), "115200", at)
	if got := d.receive([]byte("ABC"), at.Add(-time.Millisecond)); string(got) != "ABC" || d.echoed != 0 {
		t.Fatal("pre-send RX used as evidence")
	}
	d.transmitted([]byte("AB"))
	d.receive([]byte("ABC"), at.Add(time.Millisecond))
	if d.detected {
		t.Fatal("partial failed write confirmed loopback")
	}
	d = loopbackDetector{}
	d.arm([]byte("STM32F103C8T6\r\n"), "115200", at)
	d.receive([]byte("STM32F103"), at.Add(time.Millisecond))
	if got := d.expire(d.deadline.Add(time.Millisecond)); string(got) != "STM32F103" || !d.failed {
		t.Fatalf("timeout lost buffered RX: %q", got)
	}
	if got := d.expire(d.deadline.Add(time.Second)); len(got) != 0 {
		t.Fatal("timeout replayed buffered bytes twice")
	}
}

func TestSendArmsLoopbackAfterValidation(t *testing.T) {
	a := newApp()
	a.sendText = "hi"
	a.send()
	if a.loopback.attempted {
		t.Fatal("disconnected send armed loopback")
	}
	a.active = newSession(newStubPort())
	defer a.active.stop()
	a.sendMode, a.sendText = "HEX", "AA 5"
	a.send()
	if a.loopback.attempted {
		t.Fatal("invalid send armed loopback")
	}
	a.sendText = "AA 55"
	a.send()
	if !bytes.Equal(a.loopback.payload, []byte{0xaa, 0x55, '\r', '\n'}) {
		t.Fatalf("wire payload not tracked: %x", a.loopback.payload)
	}
	a.loopback = loopbackDetector{}
	a.demo = true
	a.send()
	if a.loopback.attempted {
		t.Fatal("demo participated in physical-loopback detection")
	}
}

func TestNativeUSBMultiVendorHints(t *testing.T) {
	for _, tc := range []struct{ vid, pid, model string }{
		{"303a", "1001", "ESP32"}, {"0483", "5740", "STM32"}, {"0483", "572a", "STM32F401"},
		{"2e8a", "000a", "RP2040"}, {"2e8a", "0005", "RP"}, {"2e8a", "0009", "RP"},
		{"2341", "8036", "ATMEGA32U4"}, {"239a", "801e", "ATSAMD21E18A"},
		{"03eb", "6119", "SAM"}, {"04d8", "000b", "PIC18F2550"}, {"1fc9", "0003", "LPC1343"},
		{"1915", "ffff", "NRF"}, {"ffff", "ffff", ""},
	} {
		p := &usbPortInfo{VID: tc.vid, PID: tc.pid, CDC: true}
		hint, ok := p.mcuHint()
		if ok != (tc.model != "") || ok && (hint.Model != tc.model || !strings.HasPrefix(hint.Evidence, "USB ")) {
			t.Fatalf("%s:%s hint=%+v ok=%t", tc.vid, tc.pid, hint, ok)
		}
		p.CDC = false
		if _, ok := p.mcuHint(); ok {
			t.Fatal("non-CDC port provided MCU hint")
		}
	}
}

func TestUSBBridgesAreCompletelyIgnored(t *testing.T) {
	for id := range bridgeUSBIDs {
		parts := strings.Split(id, ":")
		p := &usbPortInfo{VID: parts[0], PID: parts[1], CDC: true}
		if p.eligible() {
			t.Fatalf("bridge %s was treated as native USB MCU", id)
		}
		if _, ok := p.mcuHint(); ok {
			t.Fatal("bridge produced USB MCU evidence")
		}
	}
	for _, name := range []string{"ESP USB Bridge", "ST-LINK VCP", "J-Link", "CP2102 UART", "DAPLink CMSIS-DAP", "CH340 Serial"} {
		p := &usbPortInfo{VID: "303A", PID: "9999", Product: name, CDC: true}
		if p.eligible() {
			t.Fatalf("bridge product not excluded: %s", name)
		}
	}
	a := newApp()
	a.usb = &usbPortInfo{VID: "10C4", PID: "EA60", CDC: true}
	a.mcu.feed([]byte("STM32F103C8T6\n"))
	if got := a.mcuResults(); len(got) != 1 || got[0].Model != "STM32F103C8T6" {
		t.Fatal("bridge interfered with normal RX inference")
	}
}

func TestUSBAndRXEvidenceReconcile(t *testing.T) {
	a := newApp()
	a.usb = &usbPortInfo{VID: "0483", PID: "5740", CDC: true}
	if got := a.mcuResults(); len(got) != 1 || got[0].Model != "STM32" {
		t.Fatalf("USB-only inference failed: %+v", got)
	}
	a.mcu.feed([]byte("STM32F103C8T6\n"))
	if got := a.mcuResults(); len(got) != 1 || got[0].Model != "STM32F103C8T6" {
		t.Fatalf("RX did not refine USB family: %+v", got)
	}
	a.usb = &usbPortInfo{VID: "2E8A", PID: "000A", CDC: true}
	if got := a.mcuResults(); len(got) != 2 {
		t.Fatalf("conflicting USB/RX evidence hidden: %+v", got)
	}
	a.usb = nil
	if got := a.mcuResults(); len(got) != 1 {
		t.Fatal("USB hints permanently contaminated RX state")
	}
}

func TestUSBLookupGenerationAndErrors(t *testing.T) {
	a := newApp()
	a.active = newSession(newStubPort())
	defer a.active.stop()
	queue := make(chan func(), 4)
	a.post = func(fn func()) { queue <- fn }
	a.lookupUSB = func(string) (*usbPortInfo, error) {
		return &usbPortInfo{VID: "0483", PID: "5740", CDC: true}, nil
	}
	a.loadUSBInfo("COM3", a.generation)
	(<-queue)()
	if a.usb == nil {
		t.Fatal("valid USB metadata not published")
	}
	a.usb = nil
	a.loadUSBInfo("COM3", a.generation)
	a.generation++
	(<-queue)()
	if a.usb != nil {
		t.Fatal("stale USB lookup updated new session")
	}
	a.lookupUSB = func(string) (*usbPortInfo, error) { return nil, errors.New("metadata unavailable") }
	a.loadUSBInfo("COM3", a.generation)
	(<-queue)()
	if a.usbError == "" || a.active == nil || a.lastError != "" {
		t.Fatal("metadata failure disrupted serial connection")
	}
	a.lookupUSB = func(string) (*usbPortInfo, error) {
		return &usbPortInfo{VID: "2341", PID: "0043", CDC: true}, nil
	}
	a.loadUSBInfo("COM3", a.generation)
	(<-queue)()
	if a.usb != nil {
		t.Fatal("CDC-based UART bridge metadata was shown")
	}
}

func TestLoopbackTimeoutCannotModifyNewConnection(t *testing.T) {
	a := newApp()
	a.active = newSession(newStubPort())
	queue := make(chan func(), 4)
	a.post = func(fn func()) { queue <- fn }
	a.noteFirstSend([]byte("STM32F103C8T6"), time.Now().Add(-time.Minute))
	var pending func()
	select {
	case pending = <-queue:
	case <-time.After(time.Second):
		t.Fatal("timeout callback did not post")
	}
	a.disconnect(false)
	a.active = newSession(newStubPort())
	defer a.active.stop()
	pending()
	if a.loopback.failed || a.mcu.bytes != 0 || len(a.mcu.results()) != 0 {
		t.Fatal("old timeout contaminated new connection")
	}
}

func TestUSBAndLoopbackPanelLifetimes(t *testing.T) {
	for _, size := range [][2]int{{1320, 900}, {960, 720}} {
		a := newApp()
		a.active = newSession(newStubPort())
		defer a.active.stop()
		a.usb = &usbPortInfo{VID: "2E8A", PID: "000A", CDC: true, Product: "Pico", Driver: "usbser", Serial: "0123456789"}
		tt := ui.NewTester(a.view, size[0], size[1])
		if !tt.HasText("USB 信息") || !tt.HasText("VID 2E8A / PID 000A") || !tt.HasText("USB 线索 · 待 RX 验证") {
			t.Fatalf("native USB panel absent: %q", tt.Texts())
		}
		at := time.Now()
		a.loopback.arm([]byte("hello"), "115200", at)
		a.handleEvent(serialEvent{Direction: "TX", Data: []byte("hello"), Time: at})
		a.handleEvent(serialEvent{Direction: "RX", Data: []byte("hello"), Time: at.Add(time.Millisecond)})
		tt.Frame()
		if !tt.HasText("疑似物理回环") {
			t.Fatal("physical loopback inference missing")
		}
		for i := 0; i < 3; i++ {
			a.usb = &usbPortInfo{VID: "10C4", PID: "EA60", CDC: true}
			tt.Frame()
			if tt.HasText("USB 信息") {
				t.Fatal("USB-UART bridge information was displayed")
			}
			a.usb = &usbPortInfo{VID: "0483", PID: "5740", CDC: true}
			tt.Frame()
		}
	}
}

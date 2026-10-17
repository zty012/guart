package main

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"go.bug.st/serial"
)

func TestEncodePayload(t *testing.T) {
	for _, tc := range []struct {
		name, text, mode, ending string
		want                     []byte
		fail                     bool
	}{
		{"text", "你好", "UTF-8", "CRLF", []byte("你好\r\n"), false},
		{"hex", "aa 00\nFf\t01", "HEX", "None", []byte{0xaa, 0, 0xff, 1}, false},
		{"hex-line-ending", "41", "HEX", "LF", []byte("A\n"), false},
		{"cr", "AT", "UTF-8", "CR", []byte("AT\r"), false},
		{"odd", "A 01", "HEX", "None", nil, true},
		{"invalid", "GG", "HEX", "None", nil, true},
		{"prefix-not-accepted", "0xAA", "HEX", "None", nil, true},
		{"empty", " \n", "HEX", "None", nil, true},
		{"empty-text", "", "UTF-8", "CRLF", nil, true},
		{"bad-ending", "AT", "UTF-8", "bad", nil, true},
		{"bad-mode", "AT", "bad", "None", nil, true},
		{"too-long", strings.Repeat("a", maxPayload), "UTF-8", "CRLF", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodePayload(tc.text, tc.mode, tc.ending)
			if (err != nil) != tc.fail || !bytes.Equal(got, tc.want) {
				t.Fatalf("got %x, %v; want %x, fail=%v", got, err, tc.want, tc.fail)
			}
		})
	}
}

func TestNewlineKind(t *testing.T) {
	for _, tc := range []struct{ data, want string }{
		{"plain", "无换行"}, {"", "无换行"},
		{"a\r\nb\r\n", "CRLF"}, {"a\nb\n", "LF"}, {"a\rb\r", "CR"},
		{"a\r\nb\n", "混合 CRLF/LF"}, {"a\r\nb\r", "混合 CRLF/CR"},
		{"a\nb\r", "混合 LF/CR"}, {"a\r\nb\nc\r", "混合 CRLF/LF/CR"},
		{"\r\r\n\n", "混合 CRLF/LF/CR"},
		{"\x1b[31mred\r\n", "CRLF"},
	} {
		if got := newlineKind([]byte(tc.data)); got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.data, got, tc.want)
		}
	}
}

func TestPortMode(t *testing.T) {
	cfg := defaultConfig()
	mode, err := cfg.mode()
	if err != nil || mode.BaudRate != 115200 || mode.DataBits != 8 || mode.Parity != serial.NoParity || mode.StopBits != serial.OneStopBit || !mode.InitialStatusBits.DTR {
		t.Fatalf("mode=%+v err=%v", mode, err)
	}
	for _, mutate := range []func(*portConfig){
		func(c *portConfig) { c.Baud = "0" }, func(c *portConfig) { c.Baud = "abc" },
		func(c *portConfig) { c.Bits = "9" }, func(c *portConfig) { c.Parity = "bad" }, func(c *portConfig) { c.Stop = "bad" },
	} {
		cfg := defaultConfig()
		mutate(&cfg)
		if _, err := cfg.mode(); err == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
}

func TestLogRetentionAndSelection(t *testing.T) {
	a := newApp()
	a.addPacket(packet{Direction: "RX", Data: []byte("first")})
	a.selectPacket(a.nextID)
	for i := 0; i < maxMessages; i++ {
		a.addPacket(packet{Direction: "RX", Data: []byte{byte(i)}})
	}
	if len(a.packets) != maxMessages || a.dropped != 1 || a.selected != 0 || a.storedBytes != maxMessages {
		t.Fatalf("retention: count=%d dropped=%d selected=%d bytes=%d", len(a.packets), a.dropped, a.selected, a.storedBytes)
	}
	a.clearLog()
	for i := 0; i < 600; i++ {
		a.addPacket(packet{Direction: "RX", Data: make([]byte, 4096)})
	}
	if a.storedBytes > maxLogBytes || len(a.packets) != maxLogBytes/4096 {
		t.Fatal("byte limit not enforced")
	}
}

func TestFilterAndPause(t *testing.T) {
	a := newApp()
	a.addPacket(packet{Direction: "RX", Data: []byte("OK\r\n")})
	a.paused, a.pauseID = true, a.nextID
	a.handleEvent(serialEvent{Direction: "TX", Data: []byte{0xaa, 0x55}})
	if len(a.visiblePackets()) != 1 || a.tx != 2 {
		t.Fatal("pause must freeze display, not capture")
	}
	a.paused, a.search = false, "AA 55"
	if got := a.visiblePackets(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("hex search: %v", got)
	}
	a.search, a.direction = "", "RX"
	if got := a.visiblePackets(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("direction filter: %v", got)
	}
	a.clearLog()
	if a.tx != 0 || a.rx != 0 || a.inspected() != nil || len(a.packets) != 0 {
		t.Fatal("clear did not reset")
	}
}

func TestCSVPreservesRawBytesAndNeutralizesFormulas(t *testing.T) {
	data, err := packetCSV([]packet{
		{ID: 1, Time: time.Unix(0, 0), Direction: "RX", Data: []byte{0, 0xff, 13, 10}},
		{ID: 2, Direction: "RX", Data: []byte("=SUM(1,1)"), Demo: true},
		{ID: 3, Direction: "SYS", Note: "@formula"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	rows, err := r.ReadAll()
	if err != nil || len(rows) != 4 || rows[1][4] != "00 FF 0D 0A" || rows[2][5] != "'=SUM(1,1)" || rows[2][6] != "true" || rows[3][5] != "'@formula" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

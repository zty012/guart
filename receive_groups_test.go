package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

func rxPacket(id uint64, at time.Time, data string) packet {
	return packet{ID: id, Direction: "RX", Time: at, Data: []byte(data)}
}

func TestReceiveScreenshotFragments(t *testing.T) {
	at := time.Unix(0, 728*time.Millisecond.Nanoseconds())
	raw := []packet{rxPacket(41, at, "h"), rxPacket(42, at.Add(100*time.Microsecond), "ello\r"), rxPacket(43, at.Add(200*time.Microsecond), "\n")}
	for _, mode := range []receiveMode{receiveIdle, receiveLine, receiveMillisecond} {
		t.Run(string(mode), func(t *testing.T) {
			got := groupReceived(raw, mode, 10*time.Millisecond)
			if len(got) != 1 || string(got[0].Data) != "hello\r\n" || len(got[0].Parts) != 3 {
				t.Fatalf("groups=%+v", got)
			}
			if got[0].Time != at || got[0].EndTime != raw[2].Time || got[0].Parts[1].Start != 1 || got[0].Parts[2].Start != 6 {
				t.Fatalf("lost timing or boundaries: %+v", got[0])
			}
		})
	}
	if got := groupReceived(raw, receiveRaw, 0); len(got) != 3 {
		t.Fatalf("raw groups=%d", len(got))
	}
	if string(raw[0].Data) != "h" || string(raw[1].Data) != "ello\r" || string(raw[2].Data) != "\n" {
		t.Fatal("grouping changed capture")
	}
}

func TestIdleCrossesMillisecondBoundaryButHonorsGap(t *testing.T) {
	at := time.Unix(0, 999900000)
	raw := []packet{rxPacket(1, at, "a"), rxPacket(2, at.Add(200*time.Microsecond), "b"), rxPacket(3, at.Add(10200*time.Microsecond), "c")}
	got := groupReceived(raw, receiveIdle, 10*time.Millisecond)
	if len(got) != 2 || string(got[0].Data) != "ab" || string(got[1].Data) != "c" {
		t.Fatalf("idle: %+v", got)
	}
	if got := groupReceived(raw, receiveMillisecond, 0); len(got) != 3 {
		t.Fatalf("millisecond grouping ignored bucket boundary: %+v", got)
	}
}

func TestReceiveGroupingBoundaries(t *testing.T) {
	at := time.Unix(10, 0)
	for _, mode := range []receiveMode{receiveIdle, receiveLine, receiveMillisecond} {
		for _, direction := range []string{"TX", "SYS", "ERR"} {
			raw := []packet{rxPacket(1, at, "a"), {ID: 2, Time: at, Direction: direction, Data: []byte("middle")}, rxPacket(3, at, "b")}
			if got := groupReceived(raw, mode, time.Second); len(got) != 3 {
				t.Fatalf("%s joined across %s", mode, direction)
			}
		}
		raw := []packet{rxPacket(1, at, "a"), rxPacket(2, at.Add(-time.Microsecond), "b")}
		if got := groupReceived(raw, mode, time.Second); len(got) != 2 {
			t.Fatalf("%s joined backwards timestamps", mode)
		}
		raw[1].Time, raw[1].Demo = at, true
		if got := groupReceived(raw, mode, time.Second); len(got) != 2 {
			t.Fatalf("%s joined different sessions", mode)
		}
	}
	var raw []packet
	for i := 0; i < 300; i++ {
		raw = append(raw, rxPacket(uint64(i+1), at.Add(time.Duration(i)*time.Millisecond), "a"))
	}
	got := groupReceived(raw, receiveIdle, 10*time.Millisecond)
	if len(got) != 2 || len(got[0].Data) != 250 || len(got[1].Data) != 50 {
		t.Fatal("continuous input not bounded by span")
	}
}

func TestLineGroupingSplitCRLFAndSeveralLinesPerRead(t *testing.T) {
	at := time.Unix(10, 0)
	raw := []packet{rxPacket(1, at, "a\r"), rxPacket(2, at.Add(time.Second), "\nb\nc\npartial")}
	got := groupReceived(raw, receiveLine, 0)
	want := []string{"a\r\n", "b\n", "c\n", "partial"}
	if len(got) != len(want) {
		t.Fatalf("lines=%+v", got)
	}
	seen := make(map[string]bool)
	for i, m := range got {
		if string(m.Data) != want[i] || m.Unterminated != (i == 3) {
			t.Fatalf("line %d: %+v", i, m)
		}
		if seen[m.key()] {
			t.Fatal("line fragments have duplicate identity")
		}
		seen[m.key()] = true
	}
	if got[1].Parts[0].Offset != 1 || got[2].Parts[0].Offset != 3 || got[3].Parts[0].Offset != 5 {
		t.Fatalf("incorrect Read offsets: %+v", got)
	}
	if len(got[0].Parts) != 2 {
		t.Fatal("CRLF split lost Read provenance")
	}
}

func TestReceiveGroupingByteLimitAndTrace(t *testing.T) {
	at := time.Unix(10, 0)
	for _, mode := range []receiveMode{receiveIdle, receiveLine, receiveMillisecond, receiveRaw} {
		t.Run(string(mode), func(t *testing.T) {
			var raw []packet
			for i := 0; i < 20; i++ {
				raw = append(raw, rxPacket(uint64(i+1), at, strings.Repeat(fmt.Sprintf("%02d", i), 2048)))
			}
			got := groupReceived(raw, mode, 10*time.Millisecond)
			var joined, expected []byte
			for _, p := range raw {
				expected = append(expected, p.Data...)
			}
			for _, m := range got {
				if len(m.Data) > maxReceiveGroupBytes {
					t.Fatal("unbounded group")
				}
				joined = append(joined, m.Data...)
				for _, part := range m.Parts {
					p := raw[part.ID-1]
					if part.Time != p.Time || !bytes.Equal(m.Data[part.Start:part.Start+part.Length], p.Data[part.Offset:part.Offset+part.Length]) {
						t.Fatal("trace no longer matches original Read")
					}
				}
			}
			if !bytes.Equal(joined, expected) {
				t.Fatal("bytes lost, duplicated or reordered")
			}
			if mode != receiveRaw && (len(got) != 2 || !got[0].Limited) {
				t.Fatal("byte limit did not split group")
			}
		})
	}
}

func TestReceiveReversibleFilteringPauseAndExport(t *testing.T) {
	a := newApp()
	at := time.Unix(10, 0)
	a.handleEvent(serialEvent{Direction: "RX", Time: at, Data: []byte("h")})
	a.handleEvent(serialEvent{Direction: "RX", Time: at, Data: []byte("ello\r")})
	a.search = "hello"
	if len(a.visiblePackets()) != 1 {
		t.Fatal("search must operate after grouping")
	}
	m := a.groupedMessages()[0]
	a.selectMessage(m)
	a.hex.Cursor, a.hex.Anchor = 5, 5
	a.paused, a.pauseID = true, a.nextID
	a.handleEvent(serialEvent{Direction: "RX", Time: at, Data: []byte("\n")})
	if p := a.inspected(); p == nil || string(p.Data) != "hello\r" {
		t.Fatal("paused message changed")
	}
	if a.rx != 7 {
		t.Fatal("pause stopped capture or corrupted byte count")
	}
	a.paused = false
	if p := a.inspected(); p == nil || string(p.Data) != "hello\r\n" || p.key() != m.key() {
		t.Fatal("resuming changed identity or lost data")
	}
	snapshot := append([]packet(nil), a.packets...)
	before, _ := packetCSV(snapshot)
	a.receiveMode = receiveRaw
	a.resetGrouping()
	if len(a.groupedMessages()) != 3 || a.selected != 0 || len(a.visiblePackets()) != 0 {
		t.Fatal("raw view not restored")
	}
	for _, mode := range []receiveMode{receiveLine, receiveMillisecond, receiveIdle} {
		a.receiveMode = mode
		if len(a.groupedMessages()) != 1 {
			t.Fatal("historical records not regrouped")
		}
	}
	a.addPacket(rxPacket(99, at, "another"))
	after, _ := packetCSV(snapshot)
	if !bytes.Equal(before, after) || string(snapshot[0].Data) != "h" {
		t.Fatal("display changed immutable export snapshot")
	}
	a.clearLog()
	if len(a.groupedMessages()) != 0 {
		t.Fatal("clear left cached groups")
	}
}

func TestReceiveEvictionAndUTF8Reassembly(t *testing.T) {
	a := newApp()
	at := time.Unix(10, 0)
	data := []byte("你好")
	for _, b := range data {
		a.handleEvent(serialEvent{Direction: "RX", Time: at, Data: []byte{b}})
	}
	if got := readable(a.groupedMessages()[0].Data); got != "你好" {
		t.Fatalf("split UTF-8 not reassembled: %s", got)
	}
	a.selectMessage(a.groupedMessages()[0])
	for i := 0; i < maxMessages; i++ {
		a.addPacket(rxPacket(0, at, "a"))
	}
	if a.inspected() != nil || a.selected != 0 || len(a.groupedMessages()[0].Data) != maxMessages {
		t.Fatal("eviction retained stale groups or selection")
	}
}

func TestSessionPreservesExplicitCaptureTimestamp(t *testing.T) {
	s := &session{events: make(chan serialEvent, 1), done: make(chan struct{})}
	at := time.Unix(10, 12345)
	s.emit(serialEvent{Time: at, Direction: "RX", Data: []byte("a")})
	if got := <-s.events; got.Time != at {
		t.Fatal("timestamp was replaced during queueing")
	}
}

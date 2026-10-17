package main

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/code"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/core"
	"github.com/egoist/mygo/ui"
)

func TestANSIAcrossBubblesAndDirections(t *testing.T) {
	a := newApp()
	a.receiveMode = receiveRaw
	for _, p := range []packet{
		{Direction: "RX", Data: []byte("\x1b[1;31mred")},
		{Direction: "TX", Data: []byte("\x1b[32mout")},
		{Direction: "ERR", Note: "an error"},
		{Direction: "RX", Data: []byte("still red")},
		{Direction: "RX", Data: []byte("\x1b[34mblue\x1b[22mnormal weight")},
		{Direction: "RX", Data: []byte("\x1b[39mdefault\x1b[0mreset")},
		{Direction: "RX", Data: []byte("plain")},
	} {
		a.addPacket(p)
	}
	m := a.groupedMessages()
	if m[3].ansi.style.prefix() != "\x1b[1;31m" || m[4].ansi.style.prefix() != "\x1b[1;31m" {
		t.Fatal("RX did not retain its own style across TX and errors")
	}
	if m[5].ansi.style.prefix() != "\x1b[34m" || m[6].ansi.style.prefix() != "" {
		t.Fatal("selective/full reset failed")
	}
	a.search = "still red"
	if got := a.visiblePackets(); len(got) != 1 || got[0] != 3 {
		t.Fatal("search changed stream chronology")
	}
	input, _ := m[3].ansiText(m[3].Data)
	if input != "\x1b[1;31mstill red" {
		t.Fatalf("input=%q", input)
	}
}

func TestANSISplitAtEveryByte(t *testing.T) {
	text := "\x1b[1;38;2;12;34;56m你好\x1b[48;5;123mOK\x1b[22;39mEND"
	var full ansiStream
	want, wantPlain := full.feed([]byte(text))
	for split := 0; split <= len(text); split++ {
		var s ansiStream
		a, ap := s.feed([]byte(text[:split]))
		b, bp := s.feed([]byte(text[split:]))
		if a+b != want || ap+bp != wantPlain || s != full {
			t.Fatalf("split %d: %q + %q, state=%+v", split, a, b, s)
		}
	}
	var s ansiStream
	for i := range text {
		s.feed([]byte{text[i]})
	}
	if s != full || s.style.prefix() != "\x1b[48;5;123m" {
		t.Fatalf("bytewise state=%+v", s)
	}
}

func TestANSIBoundedEscapesAndIgnoredOSC(t *testing.T) {
	var s ansiStream
	_, plain := s.feed([]byte("\x1b]0;title\aone\x1b]8;;https://example.test\x1b\\two\x1b]8;;\x1b\\"))
	if plain != "onetwo" {
		t.Fatalf("OSC leaked: %q", plain)
	}
	s.feed([]byte("\x1b[" + strings.Repeat("1;", 2000)))
	if len(s.pending) > maxANSIPending {
		t.Fatal("unbounded pending escape")
	}
	s.feed([]byte("\x1b[0mrecovered"))
	if s.style.prefix() != "" || s.pending != "" {
		t.Fatal("could not recover from malformed sequence")
	}
}

func TestANSISplitBubblesAndNewConnectionReset(t *testing.T) {
	a, queue := watchTestApp(t)
	a.receiveMode = receiveRaw
	for _, chunk := range []string{"\x1b[", "1;31mred", "\x1b[", "34mblue", "tail"} {
		a.addPacket(packet{Direction: "RX", Data: []byte(chunk)})
	}
	m := a.groupedMessages()
	input, plain := m[1].ansiText(m[1].Data)
	if input != "\x1b[1;31mred" || plain != "red" {
		t.Fatalf("split bubble: %q %q", input, plain)
	}
	if m[4].ansi.style.prefix() != "\x1b[1;34m" {
		t.Fatal("split style was not inherited")
	}
	a.config.Port = "FAKE"
	a.connect(false)
	runPostedUntil(t, queue, func() bool { return a.active != nil })
	a.addPacket(packet{Direction: "RX", Data: []byte("new session")})
	last := a.packets[len(a.packets)-1]
	if last.ansi.style.prefix() != "" || last.ansi.pending != "" {
		t.Fatal("new connection inherited stale ANSI state")
	}
}

func TestANSIGroupingPausePreviewAndRetention(t *testing.T) {
	a := newApp()
	at := time.Unix(10, 0)
	a.addPacket(rxPacket(0, at, "\x1b[31mred\nsecond\n\x1b[0mplain\n"))
	a.receiveMode = receiveLine
	m := a.groupedMessages()
	if len(m) != 3 || m[1].ansi.style.prefix() != "\x1b[31m" || m[2].ansi.style.prefix() != "\x1b[31m" {
		t.Fatal("line split lost initial style")
	}
	a.paused, a.pauseID = true, a.nextID
	a.addPacket(rxPacket(0, at.Add(time.Second), "\x1b[32mgreen"))
	if len(a.groupedMessages()) != 3 {
		t.Fatal("paused view advanced")
	}
	a.paused = false
	a.clearLog()
	a.receiveMode = receiveRaw
	a.addPacket(packet{Direction: "RX", Data: []byte("after clear")})
	if a.packets[0].ansi.style.prefix() != "\x1b[32m" {
		t.Fatal("clear lost live stream state")
	}
	for i := 0; i < maxMessages; i++ {
		a.addPacket(packet{Direction: "RX", Data: []byte("x")})
	}
	if a.packets[0].ansi.style.prefix() != "\x1b[32m" {
		t.Fatal("eviction lost inherited style")
	}
	a.clearLog()
	a.addPacket(packet{Direction: "RX", Data: []byte(strings.Repeat("x", 512) + "\x1b[34m")})
	a.addPacket(packet{Direction: "RX", Data: []byte("blue after a long message")})
	if a.packets[1].ansi.style.prefix() != "\x1b[34m" {
		t.Fatal("long message did not advance style")
	}
	before := append([]byte(nil), a.packets[0].Data...)
	a.groupedMessages()
	if !bytes.Equal(before, a.packets[0].Data) {
		t.Fatal("ANSI modified raw capture")
	}
}

func TestANSIPreviewInheritedStyleRendersIdentically(t *testing.T) {
	a := newApp()
	a.receiveMode = receiveRaw
	a.addPacket(packet{Direction: "RX", Data: []byte("\x1b[1;31;44mfirst")})
	a.addPacket(packet{Direction: "RX", Data: []byte("inherited")})
	p := a.groupedMessages()[1]
	input, _ := p.ansiText(p.Data)
	render := func(text string) *ui.Tester {
		var s code.TerminalState
		s.Resize(40, 3)
		s.Write(text)
		return ui.NewTester(func(c *ui.Context) {
			core.Use(c, core.Settings{})
			code.Terminal(c, &s, code.TerminalOptions{}).Size(240, 80)
		}, 240, 80)
	}
	got, want := render(input).Image(), render("\x1b[1;31;44minherited").Image()
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("inherited style differs from explicit style")
	}
	plain := render("inherited").Image()
	if bytes.Equal(got.Pix, plain.Pix) {
		t.Fatal("ANSI styling was not rendered")
	}
}

func TestErrorsOnlyInMessageListAndInspectorHidden(t *testing.T) {
	a := newApp()
	a.post = func(fn func()) { fn() }
	a.connect(false)
	if len(a.packets) != 1 || a.packets[0].Direction != "ERR" {
		t.Fatal("validation error missing from log")
	}
	a.handleEvent(serialEvent{Err: errors.New("unique driver error")})
	tt := ui.NewTester(a.view, 1320, 900)
	count := 0
	for _, text := range tt.Texts() {
		if text == "unique driver error" {
			count++
		}
	}
	if count != 1 || tt.HasText("关闭提示") || tt.HasText("HEX INSPECTOR") || a.inspector {
		t.Fatalf("duplicate error or visible inspector: %q", tt.Texts())
	}
	if err := tt.Click("字节检视器"); err != nil {
		t.Fatal(err)
	}
	if !a.inspector || !tt.HasText("HEX INSPECTOR") {
		t.Fatal("inspector toggle did not reveal it")
	}
}

func TestMultilineBubbleContainment(t *testing.T) {
	for _, mode := range []string{"UTF-8", "HEX"} {
		for _, w := range []int{960, 1320} {
			a := newApp()
			a.displayMode, a.receiveMode = mode, receiveRaw
			a.addPacket(packet{Direction: "RX", Data: []byte("\x1b[31m" + strings.Repeat("a line\n", 65))})
			p := a.groupedMessages()[0]
			tt := ui.NewTester(func(c *ui.Context) {
				core.Use(c, core.Settings{Locale: core.ZhCN})
				ui.Column(c).Fill().Children(func() {
					a.packetRow(c, p)
					ui.Text(c, "after bubble").Shrink(0)
				})
			}, w, 1800)
			for i := 0; i < 4; i++ {
				tt.Frame()
			}
			bubble, ok := tt.Find("助手")
			if !ok {
				t.Fatal("missing bubble")
			}
			after, _ := tt.Find("after bubble")
			if bubble.H < 145 || after.Y < bubble.Y+bubble.H-1 {
				t.Fatalf("%s/%d invalid bubble layout: bubble=%+v after=%+v", mode, w, bubble, after)
			}
			if dir := os.Getenv("GUART_SCREENSHOTS"); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("guart-multiline-%s-%d.png", mode, w)))
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
	}
}

func TestANSIMessageListRebuildsAndScreenshots(t *testing.T) {
	a := newApp()
	a.receiveMode = receiveRaw
	a.config.Port = "COM3"
	a.addPacket(packet{Direction: "RX", Data: []byte("\x1b[1;31mRED: first bubble")})
	a.addPacket(packet{Direction: "RX", Data: []byte("RED: inherited in second bubble")})
	a.addPacket(packet{Direction: "RX", Data: []byte("\x1b[32mGREEN: new foreground, still bold\n\x1b[22mregular weight\n" + strings.Repeat("multiline preview stays inside bubble\n", 18))})
	a.addPacket(packet{Direction: "RX", Data: []byte("\x1b[0mDefault again")})
	a.reportError("演示错误：只在消息列表显示一次")
	tt := ui.NewTester(a.view, 1320, 1000)
	for _, dark := range []bool{false, true} {
		a.themeMode = core.Light
		if dark {
			a.themeMode = core.Dark
		}
		for _, mode := range []string{"HEX", "UTF-8"} {
			a.displayMode = mode
			tt.Frame()
		}
		for _, receive := range []receiveMode{receiveLine, receiveRaw} {
			a.receiveMode = receive
			tt.Frame()
		}
		for i := 0; i < 4; i++ {
			tt.Frame()
		}
		if !tt.HasText("Default again") || tt.HasText("关闭提示") {
			t.Fatal("list rebuild lost ANSI content or restored alert")
		}
		if dir := os.Getenv("GUART_SCREENSHOTS"); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("guart-ansi-list-dark-%t.png", dark)))
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
	tt.SetSize(960, 720)
	tt.Frame()
	a.search = "inherited"
	tt.Frame()
	if !tt.HasText("RED: inherited in second bubble") {
		t.Fatal("filtered bubble disappeared")
	}
	a.search = ""
	tt.Frame()
}

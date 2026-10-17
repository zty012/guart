package main

import (
	"strings"
	"testing"

	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/code"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/core"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/theme"
	"github.com/egoist/mygo/ui"
)

func terminalText(s *code.TerminalState) string {
	var b strings.Builder
	for i := 0; i < s.Lines(); i++ {
		b.WriteString(s.LineText(i))
	}
	return b.String()
}

func TestFullMessageInUTF8AndHEX(t *testing.T) {
	for _, mode := range []string{"UTF-8", "HEX"} {
		a := newApp()
		a.receiveMode, a.displayMode = receiveRaw, mode
		data := []byte("HEAD" + strings.Repeat("x", maxReceiveGroupBytes-12) + "TAIL-END")
		a.addPacket(packet{Direction: "RX", Data: data})
		tt := ui.NewTester(a.view, 1320, 900)
		for i := 0; i < 4; i++ {
			tt.Frame()
		}
		if tt.HasText("预览仅显示前 512 字节") {
			t.Fatal("truncation hint remains")
		}
		want := "TAIL-END"
		if mode == "HEX" {
			want = hexText(data)
		}
		if !tt.HasText(want) {
			t.Fatalf("%s message tail was not built", mode)
		}
	}
}

func TestTerminalExpandsEntireMessageWithoutScrollback(t *testing.T) {
	a := newApp()
	data := "HEAD\n" + strings.Repeat("line\n", 1200) + "TAIL-END"
	p := message{packet: packet{Direction: "RX", Data: []byte(data)}}
	var preview *terminalPreview
	var bounds ui.Rect
	tt := ui.NewTester(func(c *ui.Context) {
		core.Use(c, core.Settings{})
		ui.Column(c).Fill().Children(func() { preview = a.terminalPreview(c, p) })
		bounds = preview.handle.Bounds(c)
	}, 960, 720)
	for i := 0; i < 4; i++ {
		tt.Frame()
	}
	if preview.lines != 1202 || preview.state.Lines() != 1202 || preview.state.LineText(0) != "HEAD" || preview.state.LineText(1201) != "TAIL-END" {
		t.Fatalf("lost scrollback: measured=%d retained=%d first=%q last=%q", preview.lines, preview.state.Lines(), preview.state.LineText(0), preview.state.LineText(preview.state.Lines()-1))
	}
	if preview.rows != 1202 || preview.state.Offset() != 0 {
		t.Fatal("message was not fully expanded")
	}
	label := "RX ANSI 预览: " + data
	r, ok := tt.Find(label)
	if !ok {
		t.Fatal("missing terminal")
	}
	tt.Scroll(r.X+10, r.Y+10, 0, 100000)
	tt.Frame()
	if preview.state.Offset() != 0 || bounds.H < 1202*13 {
		t.Fatalf("terminal has internal scrolling or insufficient height: offset=%d rect=%+v", preview.state.Offset(), bounds)
	}
	// Copying also sees the entire stream, not just visible rows.
	preview.state.Select(code.TerminalPos{Line: 0}, code.TerminalPos{Line: 1201, Col: 7})
	if preview.state.Selection() != data {
		t.Fatal("copy lost data outside the viewport")
	}
}

func TestTerminalWheelScrollsOuterList(t *testing.T) {
	a := newApp()
	a.receiveMode = receiveRaw
	a.addPacket(packet{Direction: "RX", Data: []byte(strings.Repeat("line\n", 80))})
	tt := ui.NewTester(a.view, 1320, 900)
	for i := 0; i < 5; i++ {
		tt.Frame()
	}
	if tt.HasText("多行预览可滚动") || tt.HasText("Ctrl+Shift+C 复制") {
		t.Fatal("removed hint remains")
	}
	label := "RX ANSI 预览: " + strings.Repeat("line\n", 80)
	before, ok := tt.Find(label)
	if !ok {
		t.Fatal("terminal missing")
	}
	if !a.messageList.AtEnd() {
		t.Fatal("list did not initially follow end")
	}
	// The end-following list initially shows the bottom of the tall bubble.
	tt.Scroll(450, 350, 0, -120)
	for i := 0; i < 3; i++ {
		tt.Frame()
	}
	after, ok := tt.Find(label)
	if !ok || a.messageList.AtEnd() {
		t.Fatalf("wheel trapped inside terminal: before=%+v after=%+v", before, after)
	}
}

func TestTerminalReflowsUsingActualLayoutWidth(t *testing.T) {
	a := newApp()
	data := strings.Repeat("W", 350)
	p := message{packet: packet{Direction: "RX", Data: []byte("\x1b[31m" + data)}}
	var preview *terminalPreview
	var cw float32
	tt := ui.NewTester(func(c *ui.Context) {
		core.Use(c, core.Settings{})
		cw = ui.Shape("M", ui.Font{Family: theme.MonoFont, Size: core.FontSize(c, code.CodeSize)})[0].Advance
		ui.Column(c).Fill().Children(func() { preview = a.terminalPreview(c, p) })
	}, 1320, 900)
	var wideHeight float32
	wideLines := 0
	for i, w := range []int{1320, 960, 1320} {
		tt.SetSize(w, 900)
		for j := 0; j < 5; j++ {
			tt.Frame()
		}
		r, ok := tt.Find("RX ANSI 预览: " + data)
		if !ok {
			t.Fatal("terminal missing")
		}
		cols := max(1, int((r.W-12)/cw))
		wantLines := (len(data) + cols - 1) / cols
		if preview.cols != cols || preview.lines != wantLines || preview.rows != wantLines {
			t.Fatalf("width=%d actual columns=%d preview=%+v want lines=%d", w, cols, preview, wantLines)
		}
		if terminalText(&preview.state) != data {
			t.Fatal("resize cropped or duplicated wrapped cells")
		}
		if i == 0 {
			wideHeight, wideLines = r.H, preview.lines
		}
		if i == 1 && (r.H <= wideHeight || preview.lines <= wideLines) {
			t.Fatalf("narrower width did not increase wrapped height: rect=%+v cols=%d lines=%d wideHeight=%f wideLines=%d", r, cols, preview.lines, wideHeight, wideLines)
		}
		if i == 2 && r.H != wideHeight {
			t.Fatal("widening did not restore height")
		}
	}
}

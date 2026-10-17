package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/core"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/theme"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestStatusBarBuildVersion(t *testing.T) {
	original := mygo.App.Version()
	t.Cleanup(func() { mygo.App.SetVersion(original) })
	for _, size := range [][2]int{{1320, 900}, {960, 720}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			a := newApp()
			tt := ui.NewTester(a.view, size[0], size[1])
			for _, tc := range []struct{ version, label string }{
				{"1.2.3", "GUART 1.2.3"},
				{"1.3.0-rc.1", "GUART 1.3.0-rc.1"},
				{"", "GUART dev"},
			} {
				mygo.App.SetVersion(tc.version)
				for i := 0; i < 3; i++ {
					tt.Frame()
					if !tt.HasText(tc.label) || tt.HasText("GUART 0.1") {
						t.Fatalf("version %q: status bar does not show %q", tc.version, tc.label)
					}
				}
			}
		})
	}
}

func TestHeaderCheckForUpdates(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
		bar  ui.TitleBar
	}{
		{"wide", 1320, 900, ui.TitleBar{Height: 40, Right: 138}},
		{"compact", 960, 720, ui.TitleBar{Height: 40, Right: 138}},
		{"compact-left-controls", 960, 720, ui.TitleBar{Height: 40, Left: 78}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newApp()
			if a.checkForUpdates == nil {
				t.Fatal("update action is not configured")
			}
			checks := 0
			a.checkForUpdates = func() { checks++ }
			tt := ui.NewTester(a.view, tc.w, tc.h)
			tt.SetTitleBar(tc.bar)
			for i := 0; i < 3; i++ {
				tt.Frame()
			}
			if checks != 0 {
				t.Fatal("rendering triggered a manual update check")
			}
			for _, label := range []string{"检查更新", "外观模式"} {
				r, ok := tt.Find(label)
				if !ok || r.W <= 0 || r.H <= 0 || r.X < tc.bar.Left || r.Y < 0 || r.X+r.W > float32(tc.w)-tc.bar.Right || r.Y+r.H > 90 {
					t.Fatalf("header control %q is outside the title bar: %+v", label, r)
				}
			}
			for i := 1; i <= 2; i++ {
				if err := tt.Click("检查更新"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if checks != i {
					t.Fatalf("click %d: got %d update checks", i, checks)
				}
			}
			if dir := os.Getenv("GUART_SCREENSHOTS"); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				f, err := os.Create(filepath.Join(dir, "guart-updater-"+tc.name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, tt.Image()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestAppearanceFollowsSystemAndManualOverrides(t *testing.T) {
	for _, size := range [][2]int{{1320, 900}, {960, 720}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			a := newApp()
			if a.themeMode != core.System {
				t.Fatal("new app should follow the system appearance")
			}
			var actual theme.Tokens
			tt := ui.NewTester(func(c *ui.Context) {
				a.view(c)
				actual = core.Tokens(c)
			}, size[0], size[1])
			check := func(dark bool) {
				t.Helper()
				want := theme.Light()
				if dark {
					want = theme.Dark()
				}
				for i := 0; i < 3; i++ {
					tt.Frame()
					if actual.Dark != dark || actual.Background != want.Background || actual.Text != want.Text {
						t.Fatalf("mode %v: got dark=%t, background=%v, text=%v; want dark=%t, background=%v, text=%v",
							a.themeMode, actual.Dark, actual.Background, actual.Text, dark, want.Background, want.Text)
					}
				}
			}
			choose := func(label string, mode core.Mode) {
				t.Helper()
				if err := tt.Click("外观模式"); err != nil {
					t.Fatal(err)
				}
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				if a.themeMode != mode {
					t.Fatalf("selected %q: got mode %v, want %v", label, a.themeMode, mode)
				}
			}
			check(false)
			tt.SetDark(true)
			check(true)
			tt.SetDark(false)
			check(false)
			choose("深色外观", core.Dark)
			check(true)
			tt.SetDark(true)
			tt.SetDark(false)
			check(true)
			choose("跟随系统", core.System)
			check(false)
			tt.SetDark(true)
			check(true)
			choose("浅色外观", core.Light)
			check(false)
			tt.SetDark(false)
			tt.SetDark(true)
			check(false)
			choose("跟随系统", core.System)
			check(true)
			tt.SetDark(false)
			check(false)
		})
	}
}

func TestViewInteractionsAndLifetimes(t *testing.T) {
	a := newApp()
	tt := ui.NewTester(a.view, 1320, 900)
	if !tt.HasText("GUART") || !tt.HasText("等待第一个字节") {
		t.Fatalf("initial view: %q", tt.Texts())
	}
	if err := tt.Click("AT"); err != nil {
		t.Fatal(err)
	}
	if a.sendText != "AT" {
		t.Fatal("preset did not populate composer")
	}
	a.addPacket(packet{Direction: "RX", Data: []byte("hello\r\n")})
	a.addPacket(packet{Direction: "TX", Data: []byte("AT\r\n")})
	tt.Frame()
	if err := tt.Click("检视字节"); err != nil {
		t.Fatal(err)
	}
	if a.selected == 0 {
		t.Fatal("packet not selected")
	}
	if err := tt.Click("复制 HEX"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() == "" {
		t.Fatal("copy did not use window services")
	}
	a.hex.Cursor, a.hex.Anchor = 3, 3
	a.selectPacket(1)
	tt.Frame()
	if a.hex.Cursor != 0 {
		t.Fatal("new packet did not reset cursor")
	}
	for i := 0; i < 3; i++ {
		if err := tt.Click("字节检视器"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if err := tt.Click("暂停显示"); err != nil {
		t.Fatal(err)
	}
	if !a.paused {
		t.Fatal("pause did not apply")
	}
	if err := tt.Click("继续显示"); err != nil {
		t.Fatal(err)
	}
	if a.paused {
		t.Fatal("resume did not apply")
	}
	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.packets) != 0 || a.selected != 0 {
		t.Fatal("clear did not reset view")
	}
	if err := tt.Click("外观模式"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("深色外观"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.themeMode != core.Dark {
		t.Fatal("theme did not change")
	}
	tt.SetSize(960, 720)
	tt.Frame()
	if r, ok := tt.Find("发送"); !ok || r.Y+r.H > 720 || r.X+r.W > 960 {
		t.Fatalf("small layout lost send button: %+v", r)
	}
}

func TestViewOmitsExplanatoryCopy(t *testing.T) {
	for _, size := range [][2]int{{1320, 900}, {960, 720}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			a := newApp()
			a.autoConnect = true
			a.portWatchError = "test port error"
			tt := ui.NewTester(a.view, size[0], size[1])
			check := func() {
				t.Helper()
				for _, text := range []string{
					"SERIAL MONITOR", "CONNECTION /", "PASSIVE /",
					"可手动输入后按 Enter", "每秒检测", "DTR / RTS 在打开端口时设置",
					"没有设备？", "接收分组仅影响显示", "相邻读取间隔小于此值时合并",
					"保留每次 Read 的边界", "LF 结束一条", "不代表线路帧边界",
					"连接左侧串口", "尝试修改搜索条件", "连接串口后自动分析",
					"普通业务数据可能无法识别", "不能替代硬件确认", "支持 ESP、",
					"首次发送任意数据后", "不发送 USB 探测请求",
				} {
					if tt.HasText(text) {
						t.Errorf("explanatory copy still visible: %q", text)
					}
				}
				for _, label := range []string{"GUART", "S E R I A L   S T U D I O", "每一个字节，都有迹可循。", "串口", "波特率", "发送", "MCU 识别", "检测失败: test port error"} {
					if !tt.HasText(label) {
						t.Errorf("essential label or error missing: %q", label)
					}
				}
			}
			check()
			a.active = newSession(newStubPort())
			defer a.active.stop()
			a.usb = &usbPortInfo{VID: "2E8A", PID: "000A", CDC: true}
			a.addPacket(packet{Direction: "RX", Data: []byte("hello\n")})
			a.search = "no match"
			for _, mode := range []receiveMode{receiveIdle, receiveLine, receiveMillisecond, receiveRaw} {
				a.receiveMode = mode
				tt.Frame()
				check()
				if !tt.HasText("没有匹配的记录") || !tt.HasText("USB 信息") {
					t.Fatal("empty state or USB data missing")
				}
			}
		})
	}
}

func TestMessageHeaderNewlinesBesideByteSize(t *testing.T) {
	for _, data := range []string{"plain", "a\r\n", "a\n", "a\r", "a\r\nb\nc\r"} {
		a := newApp()
		a.receiveMode = receiveRaw
		a.addPacket(packet{Direction: "RX", Data: []byte(data)})
		p := a.groupedMessages()[0]
		tt := ui.NewTester(func(c *ui.Context) {
			core.Use(c, core.Settings{Locale: core.ZhCN})
			ui.Column(c).Fill().Children(func() { a.packetRow(c, p) })
		}, 960, 720)
		for i := 0; i < 4; i++ {
			tt.Frame()
		}
		size, ok := tt.Find(fmt.Sprintf("#%04d · %d B", p.ID, len(p.Data)))
		if !ok {
			t.Fatal("byte size missing")
		}
		lineEnd, ok := tt.Find(newlineKind(p.Data))
		if !ok || lineEnd.X < size.X+size.W || lineEnd.Y < size.Y-2 || lineEnd.Y > size.Y+size.H {
			t.Fatalf("newline metadata misplaced: size=%+v newline=%+v", size, lineEnd)
		}
	}
}

func TestViewEditingSearchAndHexValidation(t *testing.T) {
	a := newApp()
	a.addPacket(packet{Direction: "RX", Data: []byte("needle")})
	a.addPacket(packet{Direction: "RX", Data: []byte("other")})
	tt := ui.NewTester(a.view, 1320, 900)
	if err := tt.Click("搜索消息"); err != nil {
		t.Fatal(err)
	}
	tt.Type("needle")
	if a.search != "needle" || len(a.visiblePackets()) != 1 {
		t.Fatal("search input did not bind")
	}
	if err := tt.Click("发送格式"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("HEX"); err != nil {
		t.Fatal(err)
	}
	if a.sendMode != "HEX" {
		t.Fatalf("select did not apply: %s", a.sendMode)
	}
	if err := tt.Click("发送内容"); err != nil {
		t.Fatal(err)
	}
	tt.Type("AA 5")
	// Attach a session so validation, not the connection guard, handles submit.
	a.active = newSession(newStubPort())
	defer a.active.stop()
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyEnter)
	if a.sendError == "" {
		t.Fatal("invalid HEX submitted without an error")
	}
	select {
	case e := <-a.active.events:
		t.Fatalf("invalid HEX was sent: %+v", e)
	default:
	}
	a.clearLog()
	tt.Frame()
}

func TestReceiveGroupingViewAndInspectorLifetimes(t *testing.T) {
	a := newApp()
	at := time.Unix(10, 0)
	for _, fragment := range []string{"h", "ello\r", "\n"} {
		a.handleEvent(serialEvent{Direction: "RX", Time: at, Data: []byte(fragment)})
	}
	tt := ui.NewTester(a.view, 1320, 900)
	if !tt.HasText("hello\n") {
		t.Fatalf("missing grouped text: %q", tt.Texts())
	}
	if err := tt.Click("检视字节"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("复制 HEX"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "68 65 6C 6C 6F 0D 0A" {
		t.Fatal("inspector copied an incomplete group")
	}
	if err := tt.Click("原始分块"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Read #0001") || !tt.HasText("Read #0003") {
		t.Fatalf("missing Read provenance: %q", tt.Texts())
	}
	if err := tt.Click("HEX 视图"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"原始读取", "同一毫秒", "按行 LF / CRLF", "空闲间隔"} {
		if err := tt.Click("接收分组"); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click(mode); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		want := 1
		if mode == "原始读取" {
			want = 3
		}
		if len(a.groupedMessages()) != want || a.selected != 0 {
			t.Fatalf("mode %s did not regroup and reset selection", mode)
		}
	}
	if err := tt.Click("接收空闲间隔"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("20"); err != nil {
		t.Fatal(err)
	}
	if a.receiveGap != "20" {
		t.Fatal("gap was not bound to persistent state")
	}
	tt.SetSize(960, 720)
	tt.Frame()
	for _, label := range []string{"发送", "接收分组", "接收空闲间隔"} {
		if r, ok := tt.Find(label); !ok || r.X+r.W > 960 || r.Y+r.H > 720 {
			t.Fatalf("compact view clipped %s: %+v", label, r)
		}
	}
}

func TestReceiveLiveGroupKeepsSelectionAndHexCursor(t *testing.T) {
	a := newApp()
	at := time.Unix(10, 0)
	a.handleEvent(serialEvent{Direction: "RX", Time: at, Data: []byte("h")})
	tt := ui.NewTester(a.view, 1320, 900)
	if err := tt.Click("检视字节"); err != nil {
		t.Fatal(err)
	}
	key := a.inspected().key()
	a.handleEvent(serialEvent{Direction: "RX", Time: at.Add(time.Millisecond), Data: []byte("ello\r")})
	tt.Frame()
	if a.inspected().key() != key || !tt.HasText("hello") {
		t.Fatal("growing group lost identity or failed to render")
	}
	a.hex.Cursor, a.hex.Anchor = 5, 5
	a.handleEvent(serialEvent{Direction: "RX", Time: at.Add(2 * time.Millisecond), Data: []byte("\n")})
	tt.Frame()
	if a.hex.Cursor != 5 || len(a.inspected().Data) != 7 {
		t.Fatal("appending data reset selection")
	}
	if err := tt.Click("复制 HEX"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "68 65 6C 6C 6F 0D 0A" {
		t.Fatal("growing message copied stale data")
	}
	// Freezing at an earlier raw record must also shrink a selected live group safely.
	a.hex.Cursor, a.hex.Anchor = 6, 6
	a.paused, a.pauseID = true, 1
	tt.Frame()
	if a.hex.Cursor != 0 || a.hex.Anchor != 0 || len(a.inspected().Data) != 1 {
		t.Fatal("shorter paused snapshot left an out-of-range cursor")
	}
}

func TestRenderReceiveGroupingScreenshots(t *testing.T) {
	dir := os.Getenv("GUART_SCREENSHOTS")
	if dir == "" {
		t.Skip("GUART_SCREENSHOTS not set")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.themeMode = core.Dark
	a.config.Port, a.ports = "COM3", []string{"COM3"}
	a.notice = "DEMO · 复现碎片接收，仅用于界面验证"
	a.sendText = "hello"
	at := time.Date(2026, 10, 17, 20, 47, 22, 728000000, time.Local)
	a.addPacket(packet{Direction: "SYS", Time: at, Demo: true, Note: "DEMO · 三次 Read 的 h / ello\\r / \\n 合并展示"})
	a.addPacket(packet{Direction: "TX", Time: at.Add(-time.Millisecond), Demo: true, Data: []byte("hello\r\n")})
	a.demo = true
	for i, fragment := range []string{"h", "ello\r", "\n"} {
		a.handleEvent(serialEvent{Direction: "RX", Time: at.Add(time.Duration(i) * 100 * time.Microsecond), Data: []byte(fragment)})
	}
	a.tx = 7
	for _, tc := range []struct {
		name  string
		mode  receiveMode
		parts bool
		w, h  int
	}{
		{"guart-receive-grouped", receiveIdle, false, 1320, 900},
		{"guart-receive-parts", receiveIdle, true, 1320, 900},
		{"guart-receive-raw", receiveRaw, false, 1320, 900},
		{"guart-receive-compact", receiveIdle, true, 960, 720},
	} {
		a.receiveMode, a.showParts = tc.mode, tc.parts
		a.selectMessage(a.groupedMessages()[2])
		tt := ui.NewTester(a.view, tc.w, tc.h)
		tt.Frame()
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

// Set GUART_SCREENSHOTS to a workspace directory to save reproducible visual QA.
func TestRenderScreenshots(t *testing.T) {
	dir := os.Getenv("GUART_SCREENSHOTS")
	if dir == "" {
		t.Skip("GUART_SCREENSHOTS not set")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.config.Port, a.ports = "COM3", []string{"COM3", "COM7"}
	a.sendText = "AT+INFO?"
	a.notice = "DEMO · 界面预览数据，不代表真实串口连接"
	for i, p := range []packet{
		{Direction: "SYS", Note: "DEMO · 以下为演示消息，不代表真实设备数据", Demo: true},
		{Direction: "RX", Data: []byte("GUART demo device ready\r\n"), Demo: true},
		{Direction: "TX", Data: []byte("AT+INFO?\r\n"), Demo: true},
		{Direction: "RX", Data: []byte("+INFO: GUART, firmware 1.0\r\nOK\r\n"), Demo: true},
		{Direction: "TX", Data: []byte{0xaa, 0x55, 0x01, 0, 0xff, 0x0d, 0x0a}, Demo: true},
		{Direction: "RX", Data: []byte{0xaa, 0x55, 0x81, 0, 0x24, 0x7e, 0x0d, 0x0a}, Demo: true},
	} {
		p.Time = time.Date(2026, 10, 17, 20, 30, i, 125000000, time.Local)
		a.addPacket(p)
		if p.Direction == "RX" {
			a.rx += uint64(len(p.Data))
		} else if p.Direction == "TX" {
			a.tx += uint64(len(p.Data))
		}
	}
	a.selectPacket(a.nextID)
	for _, tc := range []struct {
		name string
		w, h int
		mode core.Mode
	}{
		{"guart-light", 1320, 900, core.Light}, {"guart-dark", 1320, 900, core.Dark}, {"guart-compact", 960, 720, core.Light},
	} {
		a.themeMode = tc.mode
		tt := ui.NewTester(a.view, tc.w, tc.h)
		tt.Frame()
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

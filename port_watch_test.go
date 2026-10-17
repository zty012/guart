package main

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/core"
	"github.com/egoist/mygo/ui"
)

func watchTestApp(t *testing.T) (*app, chan func()) {
	t.Helper()
	a := newApp()
	a.lookupUSB = nil
	queue := make(chan func(), 32)
	a.post = func(fn func()) { queue <- fn }
	a.openPort = func(portConfig) (transport, error) { return newStubPort(), nil }
	t.Cleanup(a.shutdown)
	return a, queue
}

func scanTestPorts(t *testing.T, a *app, queue chan func(), ports []string, err error) {
	t.Helper()
	if a.refreshing {
		t.Fatal("previous scan still active")
	}
	a.listPorts = func() ([]string, error) { return ports, err }
	a.scanPorts(false)
	runPostedUntil(t, queue, func() bool { return !a.refreshing })
}

func TestAutoConnectDisabledAndInitialBaseline(t *testing.T) {
	a, queue := watchTestApp(t)
	scanTestPorts(t, a, queue, []string{"COM3"}, nil)
	scanTestPorts(t, a, queue, []string{"COM3", "COM7"}, nil)
	if a.autoConnect || a.opening || a.active != nil {
		t.Fatal("default must not auto-connect")
	}
	a.autoConnect = true
	scanTestPorts(t, a, queue, []string{"COM7", "COM3"}, nil)
	if !a.portBaseline || a.opening || a.active != nil {
		t.Fatal("first successful scan must only establish a baseline")
	}
	scanTestPorts(t, a, queue, []string{"COM3", "COM7", "COM7", " "}, nil)
	if a.opening || a.active != nil {
		t.Fatal("reordering or duplicates created a hotplug event")
	}
	if !reflect.DeepEqual(a.ports, []string{"COM3", "COM7"}) {
		t.Fatalf("ports=%v", a.ports)
	}
}

func TestAutoConnectNewPortUsesCurrentParameters(t *testing.T) {
	a, queue := watchTestApp(t)
	a.autoConnect = true
	a.config.Baud, a.config.Parity, a.config.DTR = "9600", "Even", false
	opened := make(chan portConfig, 1)
	a.openPort = func(cfg portConfig) (transport, error) { opened <- cfg; return newStubPort(), nil }
	scanTestPorts(t, a, queue, []string{"COM3"}, nil)
	scanTestPorts(t, a, queue, []string{"COM3", "COM7"}, nil)
	if !a.opening || !a.openingAuto || a.config.Port != "COM7" {
		t.Fatal("new port was not scheduled")
	}
	runPostedUntil(t, queue, func() bool { return a.active != nil })
	cfg := <-opened
	if cfg.Port != "COM7" || cfg.Baud != "9600" || cfg.Parity != "Even" || cfg.DTR {
		t.Fatalf("parameters not reused: %+v", cfg)
	}
	if a.openingAuto {
		t.Fatal("established connection still marked as pending")
	}
	a.setAutoConnect(false)
	if a.active == nil {
		t.Fatal("disabling closed an established connection")
	}
}

func TestAutoConnectDoesNotStealOrQueuePorts(t *testing.T) {
	for _, state := range []string{"connected", "demo", "opening"} {
		t.Run(state, func(t *testing.T) {
			a, queue := watchTestApp(t)
			a.autoConnect = true
			a.config.Port = "COM3"
			scanTestPorts(t, a, queue, []string{"COM3"}, nil)
			if state == "opening" {
				a.opening = true
			} else {
				a.active = newSession(newStubPort())
				a.demo = state == "demo"
			}
			scanTestPorts(t, a, queue, []string{"COM3", "COM7"}, nil)
			if a.config.Port != "COM3" || a.openingAuto {
				t.Fatal("hotplug stole the current port")
			}
			a.disconnect(false)
			scanTestPorts(t, a, queue, []string{"COM3", "COM7"}, nil)
			if a.opening || a.active != nil {
				t.Fatal("ignored hotplug was unexpectedly queued for later")
			}
		})
	}
}

func TestAutoConnectFailureOnlyRetriesAfterReplug(t *testing.T) {
	a, queue := watchTestApp(t)
	a.autoConnect = true
	attempts := make(chan string, 4)
	a.openPort = func(cfg portConfig) (transport, error) { attempts <- cfg.Port; return nil, errors.New("port busy") }
	scanTestPorts(t, a, queue, nil, nil)
	scanTestPorts(t, a, queue, []string{"COM7"}, nil)
	runPostedUntil(t, queue, func() bool { return !a.opening })
	if a.lastError == "" || len(attempts) != 1 {
		t.Fatal("failure not reported")
	}
	for i := 0; i < 3; i++ {
		scanTestPorts(t, a, queue, []string{"COM7"}, nil)
	}
	if a.opening || len(attempts) != 1 {
		t.Fatal("failed open retried without a new presence event")
	}
	scanTestPorts(t, a, queue, nil, nil)
	scanTestPorts(t, a, queue, []string{"COM7"}, nil)
	runPostedUntil(t, queue, func() bool { return !a.opening })
	if len(attempts) != 2 {
		t.Fatal("replug did not retry")
	}
}

func TestAutoConnectRemovalReleasesSession(t *testing.T) {
	a, queue := watchTestApp(t)
	a.autoConnect = true
	a.config.Port = "COM7"
	scanTestPorts(t, a, queue, []string{"COM7"}, nil)
	a.active = newSession(newStubPort())
	s := a.active
	a.sendText = "AT"
	a.toggleRepeat()
	scanTestPorts(t, a, queue, nil, nil)
	if a.active != nil || a.repeatCancel != nil || a.lastError == "" {
		t.Fatal("removed port or repeat task was not released")
	}
	select {
	case <-s.done:
	default:
		t.Fatal("removed session not stopped")
	}
	scanTestPorts(t, a, queue, []string{"COM7"}, nil)
	runPostedUntil(t, queue, func() bool { return a.active != nil })
}

func TestAutoConnectScanErrorIsNotRemoval(t *testing.T) {
	a, queue := watchTestApp(t)
	a.autoConnect = true
	a.config.Port, a.notice = "COM3", "keep notice"
	scanTestPorts(t, a, queue, []string{"COM3"}, nil)
	a.active = newSession(newStubPort())
	scanTestPorts(t, a, queue, nil, errors.New("enumeration failed"))
	if a.active == nil || !a.knownPorts["COM3"] || a.portWatchError == "" || a.notice != "keep notice" {
		t.Fatal("failed scan changed presence or connection status")
	}
	scanTestPorts(t, a, queue, []string{"COM3"}, nil)
	if a.portWatchError != "" || a.active == nil {
		t.Fatal("successful scan did not recover")
	}
}

func TestAutoConnectSimultaneousPortsPreferSelected(t *testing.T) {
	for _, preferred := range []string{"COM7", "not-present"} {
		a, queue := watchTestApp(t)
		a.autoConnect, a.config.Port = true, preferred
		scanTestPorts(t, a, queue, nil, nil)
		scanTestPorts(t, a, queue, []string{"COM7", "COM3"}, nil)
		want := "COM3"
		if preferred == "COM7" {
			want = "COM7"
		}
		if a.config.Port != want || !a.openingAuto {
			t.Fatalf("selected %s, want %s", a.config.Port, want)
		}
		runPostedUntil(t, queue, func() bool { return a.active != nil })
	}
}

func TestAutoConnectCancelPendingOpen(t *testing.T) {
	for _, action := range []string{"disable", "removed", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			a, queue := watchTestApp(t)
			a.autoConnect = true
			gate := make(chan struct{})
			p := newStubPort()
			a.openPort = func(portConfig) (transport, error) { <-gate; return p, nil }
			scanTestPorts(t, a, queue, nil, nil)
			scanTestPorts(t, a, queue, []string{"COM7"}, nil)
			if !a.openingAuto {
				t.Fatal("not automatically opening")
			}
			switch action {
			case "disable":
				a.setAutoConnect(false)
			case "removed":
				scanTestPorts(t, a, queue, nil, nil)
			case "shutdown":
				a.shutdown()
			}
			close(gate)
			runPostedUntil(t, queue, func() bool {
				select {
				case <-p.done:
					return true
				default:
					return false
				}
			})
			if a.active != nil || a.opening || a.openingAuto {
				t.Fatal("canceled automatic connection resurrected")
			}
		})
	}
}

func TestAutoConnectIgnoresOldScanAcrossReenable(t *testing.T) {
	a, queue := watchTestApp(t)
	gate := make(chan struct{})
	a.listPorts = func() ([]string, error) { <-gate; return []string{"COM7"}, nil }
	a.setAutoConnect(true)
	a.setAutoConnect(false)
	a.setAutoConnect(true)
	close(gate)
	runPostedUntil(t, queue, func() bool { return !a.refreshing })
	if a.portBaseline || a.opening {
		t.Fatal("old scan was used as current presence")
	}
	scanTestPorts(t, a, queue, []string{"COM7"}, nil)
	if !a.portBaseline || a.opening {
		t.Fatal("new baseline auto-connected an existing port")
	}
}

func TestAutoConnectDisablePreservesManualOpen(t *testing.T) {
	a, queue := watchTestApp(t)
	a.autoConnect, a.config.Port = true, "COM3"
	gate := make(chan struct{})
	a.openPort = func(portConfig) (transport, error) { <-gate; return newStubPort(), nil }
	a.connect(false)
	a.setAutoConnect(false)
	if !a.opening || a.openingAuto {
		t.Fatal("disabling detection canceled a manual open")
	}
	close(gate)
	runPostedUntil(t, queue, func() bool { return a.active != nil })
}

func TestAutoConnectFailedBaselineDoesNotOpenExistingPorts(t *testing.T) {
	a, queue := watchTestApp(t)
	a.autoConnect = true
	scanTestPorts(t, a, queue, nil, errors.New("enumeration failed"))
	if a.portBaseline {
		t.Fatal("failed scan established a baseline")
	}
	scanTestPorts(t, a, queue, []string{"COM3"}, nil)
	if !a.portBaseline || a.opening || a.active != nil || a.portWatchError != "" {
		t.Fatal("recovery opened an existing port instead of establishing a baseline")
	}
}

func TestPortWatcherPollsAndStops(t *testing.T) {
	a, queue := watchTestApp(t)
	a.listPorts = func() ([]string, error) { return nil, nil }
	a.setAutoConnect(true)
	done := a.portWatchDone
	runPostedUntil(t, queue, func() bool { return a.portScan >= 2 && !a.refreshing })
	a.setAutoConnect(false)
	select {
	case <-done:
	default:
		t.Fatal("watcher not canceled")
	}
	before := a.portScan
	for len(queue) > 0 {
		(<-queue)()
	}
	if a.portScan != before || a.portWatchDone != nil {
		t.Fatal("disabled watcher still scans")
	}
	a.shutdown()
	a.setAutoConnect(true)
	if a.autoConnect || a.portWatchDone != nil {
		t.Fatal("closed app restarted the watcher")
	}
}

func TestAutoConnectSwitchView(t *testing.T) {
	a, queue := watchTestApp(t)
	a.listPorts = func() ([]string, error) { return []string{"COM3"}, nil }
	tt := ui.NewTester(a.view, 1320, 900)
	if a.autoConnect {
		t.Fatal("switch defaulted on")
	}
	if err := tt.Click("自动连接新出现的串口"); err != nil {
		t.Fatal(err)
	}
	runPostedUntil(t, queue, func() bool { return !a.refreshing })
	tt.Frame()
	if !a.autoConnect || a.portWatchDone == nil || !a.portBaseline || a.active != nil {
		t.Fatal("switch did not enable detection safely")
	}
	tt.SetSize(960, 720)
	tt.Frame()
	if r, ok := tt.Find("自动连接新出现的串口"); !ok || r.X+r.W > 256 || r.Y+r.H > 720 {
		t.Fatalf("switch clipped: %+v", r)
	}
	if err := tt.Click("自动连接新出现的串口"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.autoConnect || a.portWatchDone != nil {
		t.Fatal("switch did not stop detection")
	}
	// Verify callbacks queued before closing cannot restart polling.
	a.shutdown()
	for len(queue) > 0 {
		(<-queue)()
	}
	tt.Advance(time.Millisecond)
}

func TestRenderAutoConnectScreenshots(t *testing.T) {
	dir := os.Getenv("GUART_SCREENSHOTS")
	if dir == "" {
		t.Skip("GUART_SCREENSHOTS not set")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		w, h    int
		mode    core.Mode
		failure string
	}{
		{"guart-auto-connect-light", 1320, 900, core.Light, ""},
		{"guart-auto-connect-dark", 1320, 900, core.Dark, ""},
		{"guart-auto-connect-compact", 960, 720, core.Light, ""},
		{"guart-auto-connect-error", 960, 720, core.Dark, "DEMO: 设备列表暂时不可用"},
	} {
		a := newApp()
		// Render enabled state without starting a watcher or touching hardware.
		a.autoConnect, a.themeMode, a.portWatchError = true, tc.mode, tc.failure
		a.config.Port, a.ports = "COM3", []string{"COM3"}
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

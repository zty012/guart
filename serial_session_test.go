package main

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

type stubPort struct {
	done      chan struct{}
	once      sync.Once
	mu        sync.Mutex
	written   []byte
	maxWrite  int
	writeErr  error
	zeroWrite bool
}

func newStubPort() *stubPort                 { return &stubPort{done: make(chan struct{})} }
func (p *stubPort) Read([]byte) (int, error) { <-p.done; return 0, io.EOF }
func (p *stubPort) Close() error             { p.once.Do(func() { close(p.done) }); return nil }
func (p *stubPort) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.zeroWrite {
		return 0, nil
	}
	n := len(data)
	if p.maxWrite > 0 {
		n = min(n, p.maxWrite)
	}
	p.written = append(p.written, data[:n]...)
	return n, p.writeErr
}

func nextEvent(t *testing.T, s *session) serialEvent {
	t.Helper()
	select {
	case e := <-s.events:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for serial event")
		return serialEvent{}
	}
}

func TestSessionShortWriteAndPayloadOwnership(t *testing.T) {
	p := newStubPort()
	p.maxWrite = 2
	s := newSession(p)
	defer s.stop()
	data := []byte("abcdef")
	if err := s.send(data); err != nil {
		t.Fatal(err)
	}
	data[0] = '!'
	e := nextEvent(t, s)
	if e.Direction != "TX" || string(e.Data) != "abcdef" {
		t.Fatalf("event=%+v", e)
	}
	p.mu.Lock()
	got := string(p.written)
	p.mu.Unlock()
	if got != "abcdef" {
		t.Fatalf("short writes lost data: %s", got)
	}
	s.stop()
	s.stop()
	if s.send([]byte("closed")) == nil {
		t.Fatal("send after close succeeded")
	}
}

func TestSessionPartialFailureAndZeroWrite(t *testing.T) {
	for _, zero := range []bool{false, true} {
		p := newStubPort()
		p.maxWrite = 2
		p.writeErr = errors.New("driver failure")
		p.zeroWrite = zero
		s := newSession(p)
		if err := s.send([]byte("abcdef")); err != nil {
			t.Fatal(err)
		}
		e := nextEvent(t, s)
		if !zero {
			if e.Direction != "TX" || string(e.Data) != "ab" {
				t.Fatalf("partial TX: %+v", e)
			}
			e = nextEvent(t, s)
		}
		if e.Err == nil || !e.Fatal {
			t.Fatalf("missing fatal error: %+v", e)
		}
		s.stop()
	}
}

func TestDemoUsesRealSessionPipeline(t *testing.T) {
	s := newSession(newDemoPort())
	defer s.stop()
	if e := nextEvent(t, s); e.Direction != "RX" || !bytes.Contains(e.Data, []byte("ready")) {
		t.Fatalf("banner: %+v", e)
	}
	if err := s.send([]byte("AT\r\n")); err != nil {
		t.Fatal(err)
	}
	foundRX, foundTX := false, false
	for i := 0; i < 2; i++ {
		e := nextEvent(t, s)
		if e.Direction == "TX" && string(e.Data) == "AT\r\n" {
			foundTX = true
		}
		if e.Direction == "RX" && string(e.Data) == "OK\r\n" {
			foundRX = true
		}
	}
	if !foundTX || !foundRX {
		t.Fatal("demo did not round trip")
	}
}

func TestBoundedSendQueue(t *testing.T) {
	p := newStubPort()
	s := &session{port: p, writes: make(chan []byte, 1), done: make(chan struct{})}
	defer s.stop()
	if err := s.send([]byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := s.send([]byte("second")); err == nil {
		t.Fatal("full queue accepted a write")
	}
}

func TestFatalReadDisconnectsAndStopsRepeat(t *testing.T) {
	a := newApp()
	a.active = newSession(newStubPort())
	a.sendText = "AT"
	a.toggleRepeat()
	if a.repeatCancel == nil {
		t.Fatal("repeat did not start")
	}
	a.handleEvent(serialEvent{Err: io.EOF, Fatal: true})
	if a.active != nil || a.repeatCancel != nil || a.lastError == "" || a.packets[len(a.packets)-1].Direction != "ERR" {
		t.Fatal("fatal error not applied")
	}
}

func runPostedUntil(t *testing.T, queue <-chan func(), done func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for !done() {
		select {
		case fn := <-queue:
			fn()
		case <-poll.C:
		case <-deadline:
			t.Fatal("timed out waiting for UI update")
		}
	}
}

func TestConnectCancellationClosesLatePort(t *testing.T) {
	a := newApp()
	a.lookupUSB = nil
	queue := make(chan func(), 8)
	a.post = func(fn func()) { queue <- fn }
	p := newStubPort()
	gate := make(chan struct{})
	a.config.Port = "FAKE"
	a.openPort = func(portConfig) (transport, error) { <-gate; return p, nil }
	a.connect(false)
	if !a.opening {
		t.Fatal("not opening")
	}
	a.disconnect(false)
	close(gate)
	runPostedUntil(t, queue, func() bool {
		select {
		case <-p.done:
			return true
		default:
			return false
		}
	})
	if a.active != nil || a.opening {
		t.Fatal("canceled connection resurrected")
	}
}

func TestAppDemoConnectRepeatAndDisconnect(t *testing.T) {
	a := newApp()
	queue := make(chan func(), 8)
	a.post = func(fn func()) { queue <- fn }
	a.connect(true)
	runPostedUntil(t, queue, func() bool { return a.rx > 0 })
	if !a.demo || a.active == nil {
		t.Fatal("demo did not connect")
	}
	a.sendText = "AT"
	a.interval = "1"
	a.toggleRepeat()
	if a.repeatCancel != nil || a.sendError == "" {
		t.Fatal("invalid repeat interval accepted")
	}
	a.interval = "50"
	a.toggleRepeat()
	runPostedUntil(t, queue, func() bool { return a.tx >= 8 })
	a.disconnect(true)
	if a.repeatCancel != nil || a.active != nil {
		t.Fatal("disconnect did not stop repeat")
	}
	a.shutdown()
	for len(queue) > 0 {
		(<-queue)()
	}
}

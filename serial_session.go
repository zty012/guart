package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

type transport interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}

type serialEvent struct {
	Direction string
	Data      []byte
	Err       error
	Time      time.Time
	Fatal     bool
}

// A session owns one reader and one serialized, bounded writer. Its event
// queue applies backpressure instead of growing with incoming serial traffic.
type session struct {
	port   transport
	events chan serialEvent
	writes chan []byte
	done   chan struct{}
	once   sync.Once
}

func newSession(port transport) *session {
	s := &session{port: port, events: make(chan serialEvent, 128), writes: make(chan []byte, 16), done: make(chan struct{})}
	go s.readLoop()
	go s.writeLoop()
	return s
}

func (s *session) emit(e serialEvent) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	select {
	case s.events <- e:
	case <-s.done:
	}
}

func (s *session) send(data []byte) error {
	select {
	case <-s.done:
		return errors.New("连接已关闭")
	default:
	}
	copyData := append([]byte(nil), data...)
	select {
	case <-s.done:
		return errors.New("连接已关闭")
	case s.writes <- copyData:
		return nil
	default:
		return errors.New("发送队列已满，请稍后重试")
	}
}

func (s *session) stop() {
	s.once.Do(func() {
		close(s.done)
		// Closing a driver must never block the UI thread.
		go s.port.Close()
	})
}

func (s *session) readLoop() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-s.done:
			return
		default:
		}
		n, err := s.port.Read(buf)
		at := time.Now()
		if n > 0 {
			s.emit(serialEvent{Direction: "RX", Time: at, Data: append([]byte(nil), buf[:n]...)})
		}
		if err != nil {
			s.emit(serialEvent{Err: fmt.Errorf("读取串口: %w", err), Fatal: true})
			return
		}
	}
}

func (s *session) writeLoop() {
	for {
		select {
		case <-s.done:
			return
		case data := <-s.writes:
			select {
			case <-s.done:
				return
			default:
			}
			written := 0
			var err error
			for written < len(data) {
				select {
				case <-s.done:
					return
				default:
				}
				var n int
				n, err = s.port.Write(data[written:])
				written += n
				if err != nil {
					break
				}
				if n == 0 {
					err = io.ErrShortWrite
					break
				}
			}
			if written > 0 {
				s.emit(serialEvent{Direction: "TX", Data: data[:written]})
			}
			if err != nil {
				s.emit(serialEvent{Err: fmt.Errorf("写入串口（%d/%d 字节）: %w", written, len(data), err), Fatal: true})
				return
			}
		}
	}
}

func openSerial(cfg portConfig) (transport, error) {
	mode, err := cfg.mode()
	if err != nil {
		return nil, err
	}
	p, err := serial.Open(cfg.Port, mode)
	if err != nil {
		return nil, err
	}
	if err = p.SetReadTimeout(200 * time.Millisecond); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

// Demo follows the same I/O path as a real port, but never touches hardware.
type demoPort struct {
	responses chan []byte
	done      chan struct{}
	once      sync.Once
	pending   []byte
}

func newDemoPort() *demoPort {
	p := &demoPort{responses: make(chan []byte, 32), done: make(chan struct{})}
	p.responses <- []byte("GUART demo device ready\r\nType AT, HELP or send a HEX payload.\r\n")
	return p
}

func (p *demoPort) Read(buf []byte) (int, error) {
	if len(p.pending) == 0 {
		select {
		case <-p.done:
			return 0, io.EOF
		case p.pending = <-p.responses:
		}
	}
	n := copy(buf, p.pending)
	p.pending = p.pending[n:]
	return n, nil
}

func (p *demoPort) Write(data []byte) (int, error) {
	var response []byte
	switch strings.ToUpper(strings.TrimSpace(string(data))) {
	case "AT":
		response = []byte("OK\r\n")
	case "HELP":
		response = []byte("COMMANDS: AT / HELP / INFO\r\nBinary payloads are echoed unchanged.\r\n")
	case "INFO":
		response = []byte("GUART / virtual loopback / firmware 1.0\r\n")
	default:
		response = append([]byte(nil), data...)
	}
	select {
	case <-p.done:
		return 0, io.ErrClosedPipe
	case p.responses <- response:
		return len(data), nil
	}
}

func (p *demoPort) Close() error { p.once.Do(func() { close(p.done) }); return nil }

func (s *session) repeat(ctx context.Context, payload []byte, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case <-t.C:
			if err := s.send(payload); err != nil {
				s.emit(serialEvent{Err: err})
			}
		}
	}
}

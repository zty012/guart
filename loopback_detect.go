package main

import (
	"bytes"
	"strconv"
	"time"
)

// Compare only the first queued user payload, including its configured ending.
// Withhold matching RX bytes from MCU inference so an echoed chip name is not
// treated as evidence. TX and RX notifications may arrive in either order.
type loopbackDetector struct {
	payload                     []byte
	at, deadline                time.Time
	echoed, written             int
	attempted, failed, detected bool
}

func (d *loopbackDetector) arm(data []byte, baud string, at time.Time) {
	if d.attempted || len(data) == 0 || len(data) > maxPayload {
		return
	}
	rate, err := strconv.Atoi(baud)
	if err != nil || rate <= 0 {
		rate = 9600
	}
	*d = loopbackDetector{payload: append([]byte(nil), data...), at: at, attempted: true,
		deadline: at.Add(3*time.Second + time.Duration(len(data))*24*time.Second/time.Duration(rate))}
}

func (d *loopbackDetector) receive(data []byte, at time.Time) []byte {
	if !d.attempted || d.failed || d.detected || !at.IsZero() && at.Before(d.at) {
		return data
	}
	if at.IsZero() {
		at = time.Now()
	}
	n := min(len(data), len(d.payload)-d.echoed)
	if at.After(d.deadline) || !bytes.Equal(data[:n], d.payload[d.echoed:d.echoed+n]) {
		d.failed = true
		return append(append([]byte(nil), d.payload[:d.echoed]...), data...)
	}
	d.echoed += n
	d.detected = d.echoed == len(d.payload) && d.written == len(d.payload)
	return data[n:]
}

func (d *loopbackDetector) transmitted(data []byte) []byte {
	if !d.attempted || d.failed || d.detected || d.written == len(d.payload) {
		return nil
	}
	if len(data) > len(d.payload)-d.written || !bytes.Equal(data, d.payload[d.written:d.written+len(data)]) {
		d.failed = true
		return append([]byte(nil), d.payload[:d.echoed]...)
	}
	d.written += len(data)
	d.detected = d.echoed == len(d.payload) && d.written == len(d.payload)
	return nil
}

func (d *loopbackDetector) expire(at time.Time) []byte {
	if !d.attempted || d.failed || d.detected || !at.After(d.deadline) {
		return nil
	}
	d.failed = true
	return append([]byte(nil), d.payload[:d.echoed]...)
}

package main

import (
	"bytes"
	"fmt"
	"strconv"
	"time"
)

type receiveMode string

const (
	receiveIdle          receiveMode = "idle"
	receiveLine          receiveMode = "line"
	receiveMillisecond   receiveMode = "millisecond"
	receiveRaw           receiveMode = "raw"
	maxReceiveGroupBytes             = 64 * 1024
	maxReceiveGroupSpan              = 250 * time.Millisecond
)

// Parts describe the original Read boundaries, including when one Read spans
// several lines. Start is relative to the displayed message; Offset to the Read.
type receivePart struct {
	ID                    uint64
	Time                  time.Time
	Offset, Start, Length int
}

type message struct {
	packet
	Offset       int
	EndTime      time.Time
	Parts        []receivePart
	Unterminated bool
	Limited      bool
}

func (m message) key() string { return fmt.Sprintf("%d:%d", m.ID, m.Offset) }

// Grouping is a reversible presentation of immutable packets. It never changes
// capture, export, counters, or chronology, and never joins across TX/SYS/ERR.
func groupReceived(raw []packet, mode receiveMode, gap time.Duration) []message {
	groups := make([]message, 0, len(raw))
	var current *message
	for _, p := range raw {
		if p.Direction != "RX" || p.Note != "" || len(p.Data) == 0 || mode == receiveRaw {
			m := message{packet: p, EndTime: p.Time}
			if len(p.Data) > 0 {
				m.Parts = []receivePart{{ID: p.ID, Time: p.Time, Length: len(p.Data)}}
			}
			groups = append(groups, m)
			current = nil
			continue
		}
		if current != nil {
			delta := p.Time.Sub(current.EndTime)
			join := p.Demo == current.Demo && delta >= 0
			switch mode {
			case receiveIdle:
				join = join && delta < gap && p.Time.Sub(current.Time) < maxReceiveGroupSpan
			case receiveMillisecond:
				join = join && p.Time.Truncate(time.Millisecond).Equal(current.Time.Truncate(time.Millisecond))
			case receiveLine:
			default:
				join = false
			}
			if !join {
				current = nil
			}
		}
		stream := p.ansi
		for offset := 0; offset < len(p.Data); {
			if current == nil {
				m := message{packet: p, Offset: offset, EndTime: p.Time}
				m.ansi = stream
				m.Data = nil
				groups = append(groups, m)
				current = &groups[len(groups)-1]
			}
			n := min(len(p.Data)-offset, maxReceiveGroupBytes-len(current.Data))
			if mode == receiveLine {
				if end := bytes.IndexByte(p.Data[offset:offset+n], '\n'); end >= 0 {
					n = end + 1
				}
			}
			current.Parts = append(current.Parts, receivePart{ID: p.ID, Time: p.Time, Offset: offset, Start: len(current.Data), Length: n})
			current.Data = append(current.Data, p.Data[offset:offset+n]...)
			stream.feed(p.Data[offset : offset+n])
			current.EndTime = p.Time
			offset += n
			current.Unterminated = mode == receiveLine && current.Data[len(current.Data)-1] != '\n'
			current.Limited = len(current.Data) == maxReceiveGroupBytes
			if current.Limited || (mode == receiveLine && !current.Unterminated) {
				current = nil
			}
		}
	}
	return groups
}

type receiveCache struct {
	valid    bool
	revision uint64
	mode     receiveMode
	gap      string
	paused   bool
	pauseID  uint64
	messages []message
}

func (a *app) groupedMessages() []message {
	k := &a.receiveCache
	if k.valid && k.revision == a.logRevision && k.mode == a.receiveMode && k.gap == a.receiveGap && k.paused == a.paused && k.pauseID == a.pauseID {
		return k.messages
	}
	raw := a.packets
	if a.paused {
		end := len(raw)
		for end > 0 && raw[end-1].ID > a.pauseID {
			end--
		}
		raw = raw[:end]
	}
	ms, err := strconv.Atoi(a.receiveGap)
	if err != nil || ms < 1 || ms > 100 {
		ms = 10
	}
	messages := groupReceived(raw, a.receiveMode, time.Duration(ms)*time.Millisecond)
	*k = receiveCache{valid: true, revision: a.logRevision, mode: a.receiveMode, gap: a.receiveGap, paused: a.paused, pauseID: a.pauseID, messages: messages}
	return messages
}

func (a *app) resetGrouping() {
	a.selectPacket(0)
	a.messageList.ScrollToEnd()
}

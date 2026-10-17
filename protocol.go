package main

import (
	"bytes"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.bug.st/serial"
)

const (
	maxPayload  = 64 * 1024
	maxMessages = 2000
	maxLogBytes = 2 * 1024 * 1024
)

type portConfig struct {
	Port, Baud, Bits, Parity, Stop string
	DTR, RTS                       bool
}

func defaultConfig() portConfig {
	return portConfig{Baud: "115200", Bits: "8", Parity: "None", Stop: "1", DTR: true, RTS: true}
}

func (p portConfig) mode() (*serial.Mode, error) {
	baud, err := strconv.Atoi(p.Baud)
	if err != nil || baud < 1 || baud > 4000000 {
		return nil, errors.New("波特率应为 1–4000000 的整数")
	}
	bits, err := strconv.Atoi(p.Bits)
	if err != nil || bits < 5 || bits > 8 {
		return nil, errors.New("数据位应为 5–8")
	}
	parities := map[string]serial.Parity{"None": serial.NoParity, "Odd": serial.OddParity, "Even": serial.EvenParity, "Mark": serial.MarkParity, "Space": serial.SpaceParity}
	parity, ok := parities[p.Parity]
	if !ok {
		return nil, errors.New("无效校验位")
	}
	stops := map[string]serial.StopBits{"1": serial.OneStopBit, "1.5": serial.OnePointFiveStopBits, "2": serial.TwoStopBits}
	stop, ok := stops[p.Stop]
	if !ok {
		return nil, errors.New("无效停止位")
	}
	return &serial.Mode{BaudRate: baud, DataBits: bits, Parity: parity, StopBits: stop,
		InitialStatusBits: &serial.ModemOutputBits{DTR: p.DTR, RTS: p.RTS}}, nil
}

func encodePayload(text, format, ending string) ([]byte, error) {
	if len(text) > maxPayload*3 {
		return nil, errors.New("发送内容过长（最多 64 KiB）")
	}
	var data []byte
	if format == "HEX" {
		compact := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, text)
		if len(compact)%2 != 0 {
			return nil, errors.New("HEX 需要完整字节，例如 AA 01 0D 0A")
		}
		var err error
		data, err = hex.DecodeString(compact)
		if err != nil {
			return nil, errors.New("HEX 只能包含 0–9、A–F 和空白分隔符")
		}
	} else if format == "UTF-8" {
		data = []byte(text)
	} else {
		return nil, errors.New("未知发送格式")
	}
	if len(data) == 0 {
		return nil, errors.New("请先输入发送内容")
	}
	switch ending {
	case "None":
	case "LF":
		data = append(data, '\n')
	case "CR":
		data = append(data, '\r')
	case "CRLF":
		data = append(data, '\r', '\n')
	default:
		return nil, errors.New("未知行尾格式")
	}
	if len(data) > maxPayload {
		return nil, errors.New("发送内容过长（含行尾最多 64 KiB）")
	}
	return data, nil
}

type packet struct {
	ID        uint64
	Time      time.Time
	Direction string
	Data      []byte
	Note      string
	Demo      bool
	ansi      ansiStream
}

func hexText(data []byte) string { return strings.ToUpper(fmt.Sprintf("% X", data)) }

func newlineKind(data []byte) string {
	var crlf, lf, cr bool
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\r':
			if i+1 < len(data) && data[i+1] == '\n' {
				crlf = true
				i++
			} else {
				cr = true
			}
		case '\n':
			lf = true
		}
	}
	var kinds []string
	if crlf {
		kinds = append(kinds, "CRLF")
	}
	if lf {
		kinds = append(kinds, "LF")
	}
	if cr {
		kinds = append(kinds, "CR")
	}
	switch len(kinds) {
	case 0:
		return "无换行"
	case 1:
		return kinds[0]
	default:
		return "混合 " + strings.Join(kinds, "/")
	}
}

func readable(data []byte) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(string(data), "�") {
		switch {
		case r == '\r':
			b.WriteString("\\r")
		case r == '\n':
			b.WriteString("\\n\n")
		case r == '\t':
			b.WriteString("\\t")
		case unicode.IsControl(r):
			fmt.Fprintf(&b, "\\x%02X", r)
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func packetCSV(packets []packet) ([]byte, error) {
	var b bytes.Buffer
	// A BOM lets Excel recognize Chinese UTF-8 without an import wizard.
	b.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"id", "time", "direction", "bytes", "hex", "text_or_note", "demo"})
	for _, p := range packets {
		text := readable(p.Data)
		if p.Note != "" {
			text = p.Note
		}
		// Prevent device-controlled text from becoming a spreadsheet formula.
		trimmed := strings.TrimLeft(text, " \t\r\n")
		if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) {
			text = "'" + text
		}
		_ = w.Write([]string{strconv.FormatUint(p.ID, 10), p.Time.Format(time.RFC3339Nano), p.Direction,
			strconv.Itoa(len(p.Data)), hexText(p.Data), text, strconv.FormatBool(p.Demo)})
	}
	w.Flush()
	return b.Bytes(), w.Error()
}

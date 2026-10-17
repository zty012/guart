package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/chat"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/code"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/core"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/layout"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"go.bug.st/serial"
)

type app struct {
	config                               portConfig
	ports                                []string
	refreshing, opening, closed          bool
	portScan, generation                 uint64
	autoConnect, openingAuto             bool
	portWatchEpoch                       uint64
	portWatchDone                        chan struct{}
	portBaseline                         bool
	knownPorts                           map[string]bool
	portWatchError                       string
	active                               *session
	demo                                 bool
	connectedAt                          time.Time
	post                                 func(func())
	window                               *mygo.Window
	openPort                             func(portConfig) (transport, error)
	listPorts                            func() ([]string, error)
	lookupUSB                            func(string) (*usbPortInfo, error)
	usb                                  *usbPortInfo
	usbError                             string
	packets                              []packet
	ansiRX, ansiTX                       ansiStream
	mcu                                  mcuDetector
	loopback                             loopbackDetector
	loopbackTimer                        *time.Timer
	nextID, selected                     uint64
	selectedOffset                       int
	logRevision                          uint64
	receiveMode                          receiveMode
	receiveGap                           string
	receiveCache                         receiveCache
	showParts                            bool
	partsList                            ui.ListState
	storedBytes                          int
	rx, tx, dropped                      uint64
	lastError, notice                    string
	search, direction, displayMode       string
	paused                               bool
	pauseID                              uint64
	messageList                          chat.MessageListState
	hex                                  code.HexViewerState
	sidebar                              layout.SplitPaneState
	inspector                            bool
	themeMode                            core.Mode
	sendText, sendMode, ending, interval string
	sendError                            string
	repeatCancel                         context.CancelFunc
	exporting                            bool
}

func newApp() *app {
	return &app{config: defaultConfig(), openPort: openSerial, listPorts: serial.GetPortsList, lookupUSB: lookupNativeUSB,
		receiveMode: receiveIdle, receiveGap: "10",
		direction: "ALL", displayMode: "UTF-8", sendMode: "UTF-8", ending: "CRLF", interval: "1000",
		sidebar: layout.SplitPaneState{Size: 256}, notice: "就绪"}
}

func (a *app) refreshPorts() {
	a.scanPorts(true)
}

func (a *app) scanPorts(manual bool) {
	if a.post == nil || a.refreshing || a.closed {
		return
	}
	a.refreshing = true
	a.portScan++
	id, epoch, post, list := a.portScan, a.portWatchEpoch, a.post, a.listPorts
	go func() {
		ports, err := list()
		post(func() {
			if a.closed || id != a.portScan {
				return
			}
			a.refreshing = false
			if err != nil {
				if manual {
					a.reportError("枚举串口失败: " + err.Error())
				}
				if a.autoConnect && epoch == a.portWatchEpoch {
					a.portWatchError = err.Error()
				}
				return
			}
			a.ports = normalizePorts(ports)
			if a.config.Port == "" && len(a.ports) > 0 && a.active == nil && !a.opening {
				a.config.Port = a.ports[0]
			}
			if manual {
				if len(a.ports) == 0 {
					a.notice = "未发现串口"
				} else {
					a.notice = fmt.Sprintf("发现 %d 个串口", len(a.ports))
				}
			}
			if epoch == a.portWatchEpoch {
				a.portWatchError = ""
			}
			a.observePorts(a.ports, epoch)
		})
	}()
}

func (a *app) connect(demo bool) {
	if a.active != nil || a.opening || a.closed || a.post == nil {
		return
	}
	if !demo {
		a.config.Port = strings.TrimSpace(a.config.Port)
		if a.config.Port == "" {
			a.reportError("请选择或输入串口，例如 COM3")
			return
		}
		if _, err := a.config.mode(); err != nil {
			a.reportError(err.Error())
			return
		}
	}
	a.generation++
	id, cfg, post, open := a.generation, a.config, a.post, a.openPort
	a.opening, a.openingAuto, a.lastError = true, false, ""
	go func() {
		var p transport
		var err error
		if demo {
			p = newDemoPort()
		} else {
			p, err = open(cfg)
		}
		post(func() {
			if a.closed || id != a.generation {
				if p != nil {
					go p.Close()
				}
				return
			}
			a.opening, a.openingAuto = false, false
			if err != nil {
				a.reportError("连接失败: " + err.Error())
				return
			}
			a.active, a.demo, a.connectedAt = newSession(p), demo, time.Now()
			a.ansiRX, a.ansiTX = ansiStream{}, ansiStream{}
			a.mcu = mcuDetector{}
			a.loopback, a.usb, a.usbError = loopbackDetector{}, nil, ""
			name := cfg.Port
			if demo {
				name = "DEMO · 虚拟回环（不连接真实设备）"
			}
			a.notice = "已连接 " + name
			a.addPacket(packet{Direction: "SYS", Note: a.notice, Demo: demo})
			a.pump(a.active, id, post)
			if !demo {
				a.loadUSBInfo(cfg.Port, id)
			}
		})
	}()
}

// Coalesce driver events into at most one pending UI update. Waiting for its
// acknowledgement keeps a busy port from flooding MyGo's main-thread queue.
func (a *app) pump(s *session, id uint64, post func(func())) {
	go func() {
		t := time.NewTicker(33 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-t.C:
			}
			batch := make([]serialEvent, 0, 128)
			for len(batch) < 128 {
				select {
				case e := <-s.events:
					batch = append(batch, e)
				default:
					goto drained
				}
			}
		drained:
			if len(batch) == 0 {
				continue
			}
			ack := make(chan struct{})
			post(func() {
				defer close(ack)
				if a.closed || id != a.generation || a.active != s {
					return
				}
				for _, e := range batch {
					a.handleEvent(e)
					if e.Fatal {
						break
					}
				}
			})
			select {
			case <-s.done:
				return
			case <-ack:
			}
		}
	}()
}

func (a *app) handleEvent(e serialEvent) {
	if len(e.Data) > 0 {
		if e.Direction == "RX" {
			a.rx += uint64(len(e.Data))
			if !a.demo {
				a.mcu.bytes += uint64(len(e.Data))
				a.mcu.scan(a.loopback.receive(e.Data, e.Time))
			}
		} else if e.Direction == "TX" {
			a.tx += uint64(len(e.Data))
			if !a.demo {
				a.mcu.scan(a.loopback.transmitted(e.Data))
			}
		}
		a.addPacket(packet{Direction: e.Direction, Time: e.Time, Data: e.Data, Demo: a.demo})
	}
	if e.Err != nil {
		a.reportError(e.Err.Error())
		if e.Fatal {
			a.disconnect(false)
		}
	}
}

func (a *app) reportError(text string) {
	a.lastError = text
	a.addPacket(packet{Direction: "ERR", Note: text, Demo: a.demo})
}

func (a *app) stopRepeat() {
	if a.repeatCancel != nil {
		a.repeatCancel()
		a.repeatCancel = nil
	}
}

func (a *app) disconnect(log bool) {
	a.generation++
	if a.loopbackTimer != nil {
		a.loopbackTimer.Stop()
		a.loopbackTimer = nil
	}
	a.mcu = mcuDetector{}
	a.loopback, a.usb, a.usbError = loopbackDetector{}, nil, ""
	a.stopRepeat()
	a.opening, a.openingAuto = false, false
	if a.active != nil {
		a.active.stop()
		a.active = nil
		if log {
			a.addPacket(packet{Direction: "SYS", Note: "连接已断开", Demo: a.demo})
		}
	}
	a.notice = "连接已断开"
}

func (a *app) shutdown() {
	a.closed = true
	a.stopPortWatch()
	a.disconnect(false)
}

func (a *app) addPacket(p packet) {
	a.logRevision++
	a.nextID++
	p.ID = a.nextID
	if p.Time.IsZero() {
		p.Time = time.Now()
	}
	p.Data = append([]byte(nil), p.Data...)
	if p.Direction == "RX" || p.Direction == "TX" {
		s := &a.ansiRX
		if p.Direction == "TX" {
			s = &a.ansiTX
		}
		p.ansi = *s
		s.feed(p.Data)
	}
	a.packets = append(a.packets, p)
	a.storedBytes += len(p.Data)
	for len(a.packets) > maxMessages || a.storedBytes > maxLogBytes {
		old := a.packets[0]
		a.storedBytes -= len(old.Data)
		a.packets[0] = packet{}
		a.packets = a.packets[1:]
		a.dropped++
		if a.selected == old.ID {
			a.selectPacket(0)
		}
	}
}

func (a *app) selectPacket(id uint64) {
	a.selected, a.selectedOffset = id, 0
	a.hex = code.HexViewerState{}
	a.partsList = ui.ListState{}
}

func (a *app) selectMessage(m message) {
	a.selectPacket(m.ID)
	a.selectedOffset = m.Offset
}

func (a *app) inspected() *message {
	messages := a.groupedMessages()
	for i := range messages {
		if messages[i].ID == a.selected && messages[i].Offset == a.selectedOffset {
			return &messages[i]
		}
	}
	return nil
}

func (a *app) visiblePackets() []int {
	query := strings.ToLower(strings.TrimSpace(a.search))
	messages := a.groupedMessages()
	indices := make([]int, 0, len(messages))
	for i, p := range messages {
		if a.direction != "ALL" && a.direction != p.Direction {
			continue
		}
		if query != "" {
			_, plain := p.ansiText(p.Data)
			if !strings.Contains(strings.ToLower(p.Note+" "+plain+" "+readable(p.Data)+" "+hexText(p.Data)), query) {
				continue
			}
		}
		indices = append(indices, i)
	}
	return indices
}

func (a *app) clearLog() {
	a.logRevision++
	a.packets = nil
	a.storedBytes, a.rx, a.tx, a.dropped = 0, 0, 0, 0
	a.selectPacket(0)
	a.messageList = chat.MessageListState{}
	a.pauseID = a.nextID
	a.notice = "显示日志与计数已清空"
}

func (a *app) send() {
	if a.active == nil {
		a.sendError = "请先连接串口或启动演示"
		return
	}
	data, err := encodePayload(a.sendText, a.sendMode, a.ending)
	queuedAt := time.Now()
	if err == nil {
		err = a.active.send(data)
	}
	if err != nil {
		a.sendError = err.Error()
		return
	}
	a.sendError = ""
	a.noteFirstSend(data, queuedAt)
	a.notice = fmt.Sprintf("%d 字节已加入发送队列", len(data))
}

func (a *app) toggleRepeat() {
	if a.repeatCancel != nil {
		a.stopRepeat()
		return
	}
	if a.active == nil {
		a.sendError = "请先连接串口或启动演示"
		return
	}
	ms, err := strconv.Atoi(a.interval)
	if err != nil || ms < 50 || ms > 3600000 {
		a.sendError = "循环间隔应为 50–3600000 毫秒"
		return
	}
	data, err := encodePayload(a.sendText, a.sendMode, a.ending)
	if err != nil {
		a.sendError = err.Error()
		return
	}
	queuedAt := time.Now()
	if err = a.active.send(data); err != nil {
		a.sendError = err.Error()
		return
	}
	a.sendError = ""
	a.noteFirstSend(data, queuedAt)
	ctx, cancel := context.WithCancel(context.Background())
	a.repeatCancel = cancel
	go a.active.repeat(ctx, data, time.Duration(ms)*time.Millisecond)
}

func (a *app) noteFirstSend(data []byte, queuedAt time.Time) {
	if !a.demo && !a.loopback.attempted {
		a.loopback.arm(data, a.config.Baud, queuedAt)
		if a.post != nil && a.loopback.attempted && !a.loopback.failed && !a.loopback.detected {
			id, deadline, post := a.generation, a.loopback.deadline, a.post
			a.loopbackTimer = time.AfterFunc(time.Until(deadline)+time.Millisecond, func() {
				post(func() {
					if !a.closed && a.active != nil && a.generation == id {
						a.loopbackTimer = nil
						a.mcu.scan(a.loopback.expire(time.Now()))
					}
				})
			})
		}
	}
}

func (a *app) exportLog() {
	if a.window == nil || a.post == nil || a.exporting {
		return
	}
	a.exporting = true
	// Packets are immutable once appended; copy the collection before leaving UI.
	snapshot := append([]packet(nil), a.packets...)
	w, post := a.window, a.post
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: w, Title: "导出 GUART 日志",
			DefaultPath: "guart-" + time.Now().Format("20060102-150405") + ".csv",
			Filters:     []mygo.FileFilter{{Name: "CSV 日志", Extensions: []string{"csv"}}}})
		if err == nil && path != "" {
			var data []byte
			data, err = packetCSV(snapshot)
			if err == nil {
				err = os.WriteFile(path, data, 0600)
			}
		}
		post(func() {
			if a.closed {
				return
			}
			a.exporting = false
			if err != nil {
				a.reportError("导出失败: " + err.Error())
			} else if path != "" {
				a.notice = "日志已导出: " + path
			}
		})
	}()
}

package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

const portWatchInterval = time.Second

func (a *app) stopPortWatch() {
	a.portWatchEpoch++
	if a.portWatchDone != nil {
		close(a.portWatchDone)
		a.portWatchDone = nil
	}
}

func (a *app) setAutoConnect(enabled bool) {
	a.stopPortWatch()
	a.autoConnect = enabled && !a.closed
	a.portBaseline = false
	a.knownPorts = nil
	a.portWatchError = ""
	if !a.autoConnect {
		// Disabling cancels a pending automatic open, but not a manual open or
		// an already established connection. Late driver results are closed.
		if a.openingAuto {
			a.disconnect(false)
		}
		return
	}
	if a.post == nil {
		return
	}
	done := make(chan struct{})
	a.portWatchDone = done
	epoch, post := a.portWatchEpoch, a.post
	a.scanPorts(false)
	go func() {
		ticker := time.NewTicker(portWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			ack := make(chan struct{})
			post(func() {
				defer close(ack)
				if a.closed || !a.autoConnect || epoch != a.portWatchEpoch {
					return
				}
				a.scanPorts(false)
			})
			select {
			case <-done:
				return
			case <-ack:
			}
		}
	}()
}

func normalizePorts(ports []string) []string {
	clean := make([]string, 0, len(ports))
	for _, p := range ports {
		if p = strings.TrimSpace(p); p != "" {
			clean = append(clean, p)
		}
	}
	slices.Sort(clean)
	return slices.Compact(clean)
}

// Only successful scans from this enable/disable epoch may change presence.
// Updating the baseline even while busy consumes ignored hotplug events;
// disconnecting later must not unexpectedly open an already present device.
func (a *app) observePorts(ports []string, epoch uint64) {
	if a.closed || !a.autoConnect || epoch != a.portWatchEpoch {
		return
	}
	present := make(map[string]bool, len(ports))
	var added []string
	for _, p := range ports {
		present[p] = true
		if a.portBaseline && !a.knownPorts[p] {
			added = append(added, p)
		}
	}
	previous := a.knownPorts
	a.knownPorts, a.portBaseline = present, true
	if a.openingAuto && previous[a.config.Port] && !present[a.config.Port] {
		a.disconnect(false)
		a.notice = "自动连接已取消：串口已移除"
	}
	if a.active != nil && !a.demo && previous[a.config.Port] && !present[a.config.Port] {
		a.handleEvent(serialEvent{Err: fmt.Errorf("串口已从设备列表移除: %s", a.config.Port), Fatal: true})
	}
	if a.active != nil || a.opening || len(added) == 0 {
		return
	}
	slices.Sort(added)
	port := added[0]
	if slices.Contains(added, a.config.Port) {
		port = a.config.Port
	}
	a.config.Port = port
	a.notice = "发现新串口，自动连接: " + port
	a.connect(false)
	a.openingAuto = a.opening
}

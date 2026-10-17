package main

import (
	"fmt"
	"strings"
	"time"

	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/core"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/display"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/theme"
	"github.com/egoist/mygo/ui"
)

func mcuPanelWidth(c *ui.Context) float32 {
	if c.Root().Bounds().W < 1200 {
		return 216
	}
	return 248
}

func (a *app) workspaceWidth(c *ui.Context) float32 {
	return c.Root().Bounds().W - a.sidebar.Size - 1 - mcuPanelWidth(c)
}

func (a *app) mcuPanel(c *ui.Context) {
	k := core.Tokens(c)
	ui.Scroll(c.Key("mcu-panel")).Width(mcuPanelWidth(c)).Shrink(0).FillHeight().Background(k.Surface).
		Border(1, k.Border).BorderWidth(0, 0, 0, 1).Children(func() {
		ui.Column(c).FillWidth().Padding(22, 16).Gap(14).Children(func() {
			section(c, "03", "MCU 识别")
			rule(c)
			results := a.mcuResults()
			switch {
			case a.demo:
				ui.Text(c, "演示设备").Bold().FontSize(18)
			case a.active == nil:
				ui.Text(c, "等待连接").Bold().FontSize(18)
			case len(results) == 0:
				ui.Text(c, "尚未识别").Bold().FontSize(18)
			default:
				if len(results) > 1 || a.mcu.limited {
					display.Badge(c, display.BadgeOptions{Text: "线索冲突 · 无法唯一确定", Tone: display.BadgeWarning}, nil)
				}
				for _, candidate := range results {
					ui.Column(c.Key(candidate.Model)).FillWidth().Padding(10).Gap(8).
						Background(k.Background).Border(1, k.Border).Radius(6).Children(func() {
						ui.Text(c, candidate.Model).Font(theme.MonoFont).FontSize(17).Bold().Selectable()
						level := "较强线索 · 型号标记"
						if strings.HasPrefix(candidate.Evidence, "USB ") {
							level = "USB 线索 · 待 RX 验证"
						} else if candidate.Strength == 2 {
							level = "系列线索 · 型号待定"
						} else if candidate.Strength == 1 {
							level = "弱线索 · 启动特征"
						}
						ui.Text(c, level).FontSize(11).TextColor(k.AccentText)
						caption(c, "厂商 / "+candidate.Vendor)
						caption(c, "系列 / "+candidate.Family)
						caption(c, "内核 / "+candidate.Core)
						rule(c)
						ui.Text(c, candidate.Evidence).FontSize(11).Selectable()
					})
				}
			}
			if a.active != nil && !a.demo {
				caption(c, fmt.Sprintf("本次连接已分析 %d B RX", a.mcu.bytes))
				a.loopbackInfo(c)
				if a.usbError != "" {
					caption(c, a.usbError)
				}
			}
			if a.mcu.limited {
				caption(c, "已达 16 项候选上限")
			}
			if a.active != nil && !a.demo && a.usb.eligible() {
				a.usbPanel(c)
			}
		})
	})
}

func (a *app) loopbackInfo(c *ui.Context) {
	d := &a.loopback
	switch {
	case d.detected:
		display.Badge(c, display.BadgeOptions{Text: "疑似物理回环", Tone: display.BadgeWarning}, nil)
		caption(c, fmt.Sprintf("TX / RX 匹配 %d B · 可能为固件回显", len(d.payload)))
	case !d.attempted:
		return
	case d.failed || time.Now().After(d.deadline):
		caption(c, "未发现回环线索")
	default:
		caption(c, "等待回传…")
	}
}

func (a *app) usbPanel(c *ui.Context) {
	p := a.usb
	entry := usbMCURules[strings.ToUpper(p.VID+":"+p.PID)]
	manufacturer := p.Manufacturer
	if manufacturer == "" {
		manufacturer = entry.Vendor
	}
	if manufacturer == "" {
		manufacturer = "系统未提供"
	}
	ruleLine := "VID/PID 无已知 MCU 映射"
	if hint, ok := p.mcuHint(); ok {
		ruleLine = hint.Evidence
	}
	rule(c)
	section(c, "04", "USB 信息")
	ui.Column(c.Key("usb-info")).FillWidth().Padding(10).Gap(8).
		Background(core.Tokens(c).Background).Border(1, core.Tokens(c).Border).Radius(6).Children(func() {
		ui.Text(c, "VID "+p.VID+" / PID "+p.PID).Font(theme.MonoFont).FontSize(12).Selectable()
		caption(c, "厂商 / "+manufacturer)
		if p.Product != "" {
			ui.Text(c, "产品 / "+p.Product).FontSize(11).Selectable()
		}
		if p.Serial != "" {
			ui.Text(c, "序列号 / "+p.Serial).FontSize(11).Selectable()
		}
		if p.Driver != "" {
			caption(c, "驱动 / "+p.Driver)
		}
		caption(c, ruleLine)
	})
}

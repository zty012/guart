package main

import (
	"fmt"
	"strconv"
	"time"

	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/chat"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/code"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/core"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/display"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/feedback"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/icons"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/input"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/layout"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/theme"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) view(c *ui.Context) {
	core.Use(c, core.Settings{Mode: a.themeMode, Density: theme.Compact, Locale: core.ZhCN})
	k := core.Tokens(c)
	ui.Column(c).Fill().Background(k.Background).Children(func() {
		a.header(c)
		layout.SplitPane(c.Key("workspace"), &a.sidebar, layout.SplitPaneOptions{MinFirst: 232, MinSecond: mcuPanelWidth(c) + 444},
			func() { a.connectionPanel(c) }, func() {
				ui.Row(c).Fill().Children(func() {
					ui.Box(c).Grow(1).MinWidth(0).FillHeight().Children(func() { a.workspace(c) })
					a.mcuPanel(c)
				})
			}).Element.Grow(1).MinHeight(0)
		a.statusBar(c)
	})
}

func (a *app) header(c *ui.Context) {
	k := core.Tokens(c)
	// The native window buttons are overlaid on this row when the window uses
	// a hidden title bar. Reserve their platform-specific inset and make the
	// rest of the header a draggable title-bar surface.
	bar := c.TitleBar()
	leftInset, rightInset := max(float32(24), bar.Left+24), max(float32(24), bar.Right+24)
	ui.Row(c).Padding(14, rightInset, 14, leftInset).Gap(16).AlignItems(ui.Center).Shrink(0).Background(k.Surface).
		DragWindow().
		Border(1, k.Border).BorderWidth(0, 0, 1, 0).Children(func() {
		ui.Image(c, appLogo).Size(40, 40).Label("GUART")
		ui.Column(c).Gap(0).Children(func() {
			ui.Text(c, "GUART").Font(theme.SerifFont).FontSize(28).Bold().LineHeight(1.1)
			ui.Text(c, "S E R I A L   S T U D I O").FontSize(9).TextColor(k.TextMuted)
		})
		ui.Box(c).Height(28).Width(1).Background(k.Border).Margin(0, 6)
		ui.Text(c, "每一个字节，都有迹可循。").Font(theme.SerifFont).FontSize(15).TextColor(k.TextMuted)
		ui.Spacer(c)
		status, tone := "未连接", display.BadgeNeutral
		if a.opening {
			status, tone = "连接中", display.BadgeWarning
		}
		if a.active != nil {
			status, tone = "串口已连接", display.BadgeSuccess
			if a.demo {
				status, tone = "DEMO · 演示连接", display.BadgeWarning
			}
		}
		display.Badge(c, display.BadgeOptions{Text: status, Tone: tone}, nil)
		input.Button(c.Key("check-for-updates"), "检查更新", input.ButtonOptions{Variant: input.Ghost, Icon: icons.Must("download")}).
			OnClick(a.checkForUpdates)
		input.Select(c.Key("theme-mode"), &a.themeMode, []input.SelectOption[core.Mode]{
			{Value: core.System, Label: "跟随系统"},
			{Value: core.Light, Label: "浅色外观"},
			{Value: core.Dark, Label: "深色外观"},
		}, input.SelectOptions{Label: "外观模式"}).Element.Width(126)
	})
}

func section(c *ui.Context, number, label string) {
	k := core.Tokens(c)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, number).Font(theme.MonoFont).FontSize(10).TextColor(k.Ornament)
		ui.Text(c, label).FontSize(14).Bold()
	})
}

func caption(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(11).TextColor(core.Tokens(c).TextMuted)
}

func rule(c *ui.Context) { ui.Box(c).FillWidth().Height(1).Background(core.Tokens(c).Border).Shrink(0) }

func choices(values ...string) []input.SelectOption[string] {
	options := make([]input.SelectOption[string], len(values))
	for i, value := range values {
		options[i] = input.SelectOption[string]{Value: value, Label: value}
	}
	return options
}

func (a *app) connectionPanel(c *ui.Context) {
	k := core.Tokens(c)
	locked := a.active != nil || a.opening
	ui.Scroll(c.Key("connection-scroll")).Fill().Background(k.Surface).Children(func() {
		ui.Column(c).Padding(22, 20).Gap(14).FillWidth().Children(func() {
			section(c, "01", "连接设备")
			rule(c)
			ui.Column(c).Gap(6).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					caption(c, "串口")
					ui.Spacer(c)
					input.Button(c, "刷新", input.ButtonOptions{Variant: input.Ghost, Icon: icons.Must("refresh-cw"), Loading: a.refreshing}).OnClick(a.refreshPorts)
				})
				input.Combobox(c.Key("port"), &a.config.Port, a.ports, input.ComboboxOptions{
					AllowCustom: true, Placeholder: "选择或输入 COM3", Label: "串口", Disabled: locked,
				}).Element.MinWidth(0).FillWidth()
				input.Switch(c.Key("auto-connect"), &a.autoConnect, "自动连接新出现的串口", input.SwitchOptions{}).
					OnChange(func() { a.setAutoConnect(a.autoConnect) })
				if a.autoConnect {
					if a.portWatchError != "" {
						ui.Text(c, "检测失败: "+a.portWatchError).FontSize(11).TextColor(k.Warning).MaxHeight(44).Clip()
					}
				}
			})
			ui.Column(c).Gap(6).Children(func() {
				caption(c, "波特率")
				input.Combobox(c.Key("baud"), &a.config.Baud, []string{"9600", "19200", "38400", "57600", "115200", "230400", "460800", "921600"},
					input.ComboboxOptions{AllowCustom: true, Label: "波特率", Disabled: locked}).Element.MinWidth(0).FillWidth()
			})
			ui.Row(c).Gap(10).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
					caption(c, "数据位")
					input.Select(c.Key("bits"), &a.config.Bits, choices("5", "6", "7", "8"), input.SelectOptions{Label: "数据位", Disabled: locked}).Element.MinWidth(0).Width((a.sidebar.Size - 50) / 2)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
					caption(c, "停止位")
					input.Select(c.Key("stop"), &a.config.Stop, choices("1", "1.5", "2"), input.SelectOptions{Label: "停止位", Disabled: locked}).Element.MinWidth(0).Width((a.sidebar.Size - 50) / 2)
				})
			})
			ui.Column(c).Gap(6).Children(func() {
				caption(c, "校验位")
				input.Select(c.Key("parity"), &a.config.Parity, choices("None", "Odd", "Even", "Mark", "Space"), input.SelectOptions{Label: "校验位", Disabled: locked}).Element.MinWidth(0).Width(a.sidebar.Size - 40)
			})
			ui.Row(c).Gap(22).Children(func() {
				input.Checkbox(c.Key("dtr"), &a.config.DTR, "DTR", input.CheckboxOptions{Disabled: locked})
				input.Checkbox(c.Key("rts"), &a.config.RTS, "RTS", input.CheckboxOptions{Disabled: locked})
			})
			if a.active != nil || a.opening {
				label := "断开连接"
				if a.opening {
					label = "取消连接"
				}
				input.Button(c, label, input.ButtonOptions{Variant: input.Danger, Icon: icons.Must("x")}).FillWidth().OnClick(func() { a.disconnect(true) })
			} else {
				input.Button(c, "连接串口", input.ButtonOptions{Icon: icons.Must("arrow-left-right")}).FillWidth().OnClick(func() { a.connect(false) })
			}
			input.Button(c, "启动演示", input.ButtonOptions{Variant: input.Outline, Icon: icons.Must("play"), Disabled: locked}).FillWidth().OnClick(func() { a.connect(true) })
			rule(c)
			section(c, "02", "流量统计")
			ui.Row(c).Gap(10).Children(func() {
				a.counter(c, "RX / 接收", a.rx, k.Success)
				a.counter(c, "TX / 发送", a.tx, k.AccentText)
			})
			if a.dropped > 0 {
				caption(c, fmt.Sprintf("已淘汰 %d 条旧记录", a.dropped))
			}
		})
	})
}

func (a *app) counter(c *ui.Context, label string, value uint64, color ui.Color) {
	k := core.Tokens(c)
	ui.Column(c).Grow(1).MinWidth(0).Padding(10).Gap(6).Background(k.Background).Border(1, k.Border).Radius(6).Children(func() {
		ui.Text(c, label).FontSize(10).TextColor(color).Bold()
		ui.Text(c, strconv.FormatUint(value, 10)).Font(theme.MonoFont).FontSize(22).Bold()
		caption(c, "bytes")
	})
}

func (a *app) workspace(c *ui.Context) {
	k := core.Tokens(c)
	compact := a.workspaceWidth(c) < 660
	ui.Column(c).Fill().Padding(18, 22).Gap(10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Shrink(0).Children(func() {
			display.Heading(c, "串口工作台", display.HeadingOptions{Level: 2})
			ui.Spacer(c)
			input.Button(c, "导出日志", input.ButtonOptions{Variant: input.Ghost, Icon: icons.Must("download"), Loading: a.exporting, Disabled: len(a.packets) == 0}).OnClick(a.exportLog)
			input.Button(c, "清空", input.ButtonOptions{Variant: input.Ghost, Icon: icons.Must("trash-2"), Disabled: len(a.packets) == 0}).OnClick(a.clearLog)
		})
		tools := ui.Row(c.Key("monitor-tools")).Gap(8).Shrink(0)
		if compact {
			tools.Column()
		} else {
			tools.AlignItems(ui.Center)
		}
		tools.Children(func() {
			search := input.TextInput(c.Key("search"), &a.search, input.TextInputOptions{Label: "搜索消息", Placeholder: "搜索文本或 HEX 字节…", Clearable: true,
				Leading: func() { ui.Icon(c, icons.Must("search")).Size(14, 14).TextColor(k.TextMuted) }})
			if compact {
				search.Element.FillWidth()
			} else {
				search.Element.Grow(1).MinWidth(100)
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				input.Select(c.Key("filter"), &a.direction, []input.SelectOption[string]{{Value: "ALL", Label: "全部方向"}, {Value: "RX", Label: "RX 接收"}, {Value: "TX", Label: "TX 发送"}, {Value: "ERR", Label: "错误"}}, input.SelectOptions{Label: "方向筛选"}).Element.MinWidth(0).Width(116)
				input.Select(c.Key("display-mode"), &a.displayMode, choices("UTF-8", "HEX"), input.SelectOptions{Label: "显示格式"}).Element.MinWidth(0).Width(90)
				label, icon := "暂停显示", "pause"
				if a.paused {
					label, icon = "继续显示", "play"
				}
				input.Button(c, label, input.ButtonOptions{Variant: input.Outline, Icon: icons.Must(icon)}).OnClick(func() {
					a.paused = !a.paused
					a.pauseID = a.nextID
					if !a.paused {
						a.messageList.ScrollToEnd()
					}
				})
			})
		})
		a.receiveControls(c)
		a.monitor(c)
		ui.Row(c).AlignItems(ui.Center).Shrink(0).Children(func() {
			caption(c, fmt.Sprintf("%d 条显示 / %d 条原始 · %d bytes", len(a.visiblePackets()), len(a.packets), a.storedBytes))
			ui.Spacer(c)
			input.Button(c, "字节检视器", input.ButtonOptions{Variant: input.Ghost, Icon: icons.Must("file-code")}).OnClick(func() { a.inspector = !a.inspector })
		})
		if a.inspector {
			a.hexPanel(c)
		}
		a.composer(c)
	})
}

func (a *app) receiveControls(c *ui.Context) {
	ui.Row(c.Key("receive-tools")).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
		caption(c, "RX 分组")
		r := input.Select(c.Key("receive-mode"), &a.receiveMode, []input.SelectOption[receiveMode]{
			{Value: receiveIdle, Label: "空闲间隔"},
			{Value: receiveLine, Label: "按行 LF / CRLF"},
			{Value: receiveMillisecond, Label: "同一毫秒"},
			{Value: receiveRaw, Label: "原始读取"},
		}, input.SelectOptions{Label: "接收分组"})
		r.Element.MinWidth(0).Width(184)
		if r.Changed() {
			a.resetGrouping()
		}
		if a.receiveMode == receiveIdle {
			r := input.Select(c.Key("receive-gap"), &a.receiveGap, choices("1", "5", "10", "20", "50", "100"), input.SelectOptions{Label: "接收空闲间隔"})
			r.Element.MinWidth(0).Width(64)
			if r.Changed() {
				a.resetGrouping()
			}
			caption(c, "ms")
		}
	})
}

func (a *app) monitor(c *ui.Context) {
	k := core.Tokens(c)
	messages := a.groupedMessages()
	indices := a.visiblePackets()
	ui.Column(c.Key("monitor")).Grow(1).MinHeight(60).Clip().Background(k.Background).Border(1, k.Border).Radius(8).Children(func() {
		if len(indices) == 0 {
			ui.Box(c).Fill().Center().Children(func() {
				title := "等待第一个字节"
				kind := feedback.EmptyNoData
				if len(a.packets) > 0 {
					kind, title = feedback.EmptyNoResults, "没有匹配的记录"
				}
				feedback.EmptyState(c, feedback.EmptyStateOptions{Kind: kind, Title: title, Icon: icons.Must("terminal")})
			})
			return
		}
		chat.MessageList(c.Key("messages"), &a.messageList, len(indices), chat.MessageListOptions{
			ID: func(i int) string { return messages[indices[i]].key() },
			Label: func(i int) string {
				p := messages[indices[i]]
				_, text := p.ansiText(p.Data)
				return p.Direction + " " + p.Note + " " + text
			},
		}, func(i int) { a.packetRow(c, messages[indices[i]]) }).Element.FillWidth()
	})
}

func (a *app) packetRow(c *ui.Context, p message) {
	k := core.Tokens(c)
	role := chat.MessageAssistant
	if p.Direction == "TX" {
		role = chat.MessageUser
	}
	if p.Direction == "SYS" {
		role = chat.MessageSystem
	}
	if p.Direction == "ERR" {
		role = chat.MessageError
	}
	chat.MessageBubble(c, role, chat.MessageBubbleOptions{MaxWidth: 760}, func() {
		if p.Note != "" {
			ui.Text(c, p.Note).FontSize(12).Shrink(0)
			return
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			chat.MessageHeader(c, chat.MessageHeaderOptions{Model: p.Direction})
			ui.Text(c, p.Time.Format("15:04:05.000")).Font(theme.MonoFont).FontSize(10).TextColor(k.TextMuted)
			caption(c, fmt.Sprintf("#%04d · %d B", p.ID, len(p.Data)))
			caption(c, newlineKind(p.Data))
			if p.Demo {
				display.Badge(c, display.BadgeOptions{Text: "DEMO", Tone: display.BadgeWarning}, nil)
			}
			ui.Spacer(c)
			label := "检视字节"
			if a.selected == p.ID && a.selectedOffset == p.Offset {
				label = "正在检视"
			}
			input.Button(c, label, input.ButtonOptions{Variant: input.Ghost, Icon: icons.Must("file-code")}).OnClick(func() { a.selectMessage(p); a.inspector = true })
		})
		if len(p.Parts) > 1 {
			caption(c, fmt.Sprintf("%d 次读取 · 跨度 %.3f ms · 末次 %s", len(p.Parts), float64(p.EndTime.Sub(p.Time))/float64(time.Millisecond), p.EndTime.Format("15:04:05.000")))
		}
		if p.Offset > 0 {
			caption(c, fmt.Sprintf("原始偏移 %d", p.Offset))
		}
		if p.Unterminated {
			caption(c, "未结束行")
		}
		if p.Limited {
			caption(c, "已达 64 KiB 分组上限")
		}
		if a.displayMode == "HEX" {
			// The viewport, not the text's measured height, is capped. Keeping
			// the full text inside a scroll container prevents flex overflow.
			ui.Scroll(c.Key("hex-preview")).Width(a.messageWidth(c)).Height(120).Shrink(0).Clip().Children(func() {
				ui.Text(c, hexText(p.Data)).Font(theme.MonoFont).FontSize(13).Selectable().Shrink(0)
			})
		} else {
			a.terminalPreview(c, p)
		}
	})
}

func (a *app) hexPanel(c *ui.Context) {
	k := core.Tokens(c)
	p := a.inspected()
	var data []byte
	label := "未选择消息"
	if p != nil {
		data = p.Data
		label = fmt.Sprintf("#%04d / %s / %d bytes / %d 段", p.ID, p.Direction, len(data), len(p.Parts))
	}
	// A changed grouping or a paused snapshot may shorten the selected message.
	a.hex.Cursor = max(0, min(a.hex.Cursor, len(data)-1))
	a.hex.Anchor = max(0, min(a.hex.Anchor, len(data)-1))
	services := c.Services()
	height := float32(166)
	if c.Root().Bounds().H < 800 {
		height = 130
	}
	ui.Column(c.Key("hex-panel")).Height(height).Shrink(0).Gap(6).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Text(c, "HEX INSPECTOR").Font(theme.MonoFont).FontSize(11).Bold().TextColor(k.AccentText)
			caption(c, label)
			ui.Spacer(c)
			toggle := "原始分块"
			if a.showParts {
				toggle = "HEX 视图"
			}
			input.Button(c, toggle, input.ButtonOptions{Variant: input.Ghost, Disabled: len(data) == 0}).OnClick(func() { a.showParts = !a.showParts })
			input.Button(c, "复制 HEX", input.ButtonOptions{Variant: input.Ghost, Icon: icons.Must("copy"), Disabled: len(data) == 0}).OnClick(func() {
				services.WriteClipboard(hexText(data))
				a.notice = "完整 HEX 数据已复制"
			})
		})
		if a.showParts && p != nil {
			a.partsList.Key = func(i int) any { part := p.Parts[i]; return fmt.Sprintf("%d:%d", part.ID, part.Offset) }
			ui.List(c.Key("read-parts"), &a.partsList, len(p.Parts), func(i int) {
				part := p.Parts[i]
				ui.Column(c).Padding(5, 8).Gap(2).Children(func() {
					caption(c, fmt.Sprintf("Read #%04d · %s · 原始偏移 %d · 消息偏移 %d · %d B", part.ID, part.Time.Format("15:04:05.000000"), part.Offset, part.Start, part.Length))
					ui.Text(c, hexText(p.Data[part.Start:part.Start+min(part.Length, 64)])).Font(theme.MonoFont).FontSize(11).SingleLine()
				})
			}).Grow(1).MinHeight(0)
			return
		}
		ui.ScrollHorizontal(c.Key("hex-scroll")).Grow(1).MinHeight(0).Children(func() {
			width := max(float32(740), a.workspaceWidth(c)-44)
			code.HexViewer(c.Key("hex-viewer"), data, &a.hex, code.HexViewerOptions{}).Element.Width(width).FillHeight()
		})
	})
}

func (a *app) composer(c *ui.Context) {
	k := core.Tokens(c)
	repeating := a.repeatCancel != nil
	compact := a.workspaceWidth(c) < 660
	ui.Column(c.Key("composer")).Shrink(0).Padding(12, 14).Gap(8).Background(k.Surface).Border(1, k.Border).Radius(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			if !compact {
				ui.Text(c, "发送数据").Bold().FontSize(13)
			}
			input.Select(c.Key("send-mode"), &a.sendMode, choices("UTF-8", "HEX"), input.SelectOptions{Label: "发送格式", Disabled: repeating}).Element.MinWidth(0).Width(90)
			input.Select(c.Key("ending"), &a.ending, []input.SelectOption[string]{{Value: "None", Label: "无行尾"}, {Value: "LF", Label: "+ LF"}, {Value: "CR", Label: "+ CR"}, {Value: "CRLF", Label: "+ CRLF"}}, input.SelectOptions{Label: "发送行尾", Disabled: repeating}).Element.MinWidth(0).Width(100)
			ui.Spacer(c)
			a.sendPresets(c, repeating)
		})
		placeholder := "输入文本…"
		if a.sendMode == "HEX" {
			placeholder = "AA 55 01 00 FF"
		}
		r := input.TextArea(c.Key("send-text"), &a.sendText, input.TextAreaOptions{Label: "发送内容", Placeholder: placeholder, MinHeight: 60, MaxHeight: 60, ReadOnly: repeating, Error: a.sendError})
		if r.Submitted() && !repeating {
			a.send()
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			caption(c, "间隔")
			input.TextInput(c.Key("interval"), &a.interval, input.TextInputOptions{Label: "循环间隔", Disabled: repeating}).Element.MinWidth(0).Width(72)
			caption(c, "ms")
			label, icon, variant := "循环发送", "refresh-cw", input.Outline
			if repeating {
				label, icon, variant = "停止循环", "square", input.Danger
			}
			input.Button(c, label, input.ButtonOptions{Variant: variant, Icon: icons.Must(icon), Disabled: a.active == nil}).OnClick(a.toggleRepeat)
			ui.Spacer(c)
			byteCount := "待发送"
			if data, err := encodePayload(a.sendText, a.sendMode, a.ending); err == nil {
				byteCount = fmt.Sprintf("%d B", len(data))
			}
			if !compact {
				caption(c, byteCount)
			}
			input.Button(c, "发送", input.ButtonOptions{Icon: icons.Must("arrow-up"), Disabled: a.active == nil || repeating}).Width(100).OnClick(a.send)
		})
	})
}

func (a *app) sendPresets(c *ui.Context, disabled bool) {
	for _, command := range []string{"AT", "HELP", "INFO"} {
		input.Button(c.Key("preset-"+command), command, input.ButtonOptions{Variant: input.Ghost, Disabled: disabled}).OnClick(func() { a.sendText, a.sendMode = command, "UTF-8"; a.sendError = "" })
	}
}

func (a *app) statusBar(c *ui.Context) {
	version := mygo.App.Version()
	if version == "" {
		version = "dev"
	}
	left := []layout.StatusItem{{ID: "notice", Text: a.notice, Secondary: true}}
	connection := "OFFLINE"
	if a.active != nil {
		connection = a.config.Port + " · " + a.config.Baud + " · " + a.config.Bits + " / " + a.config.Parity + " / " + a.config.Stop
		if a.demo {
			connection = "DEMO / VIRTUAL LOOPBACK"
		}
		elapsed := time.Since(a.connectedAt).Truncate(time.Second)
		left = append(left, layout.StatusItem{ID: "elapsed", Text: elapsed.String(), Secondary: true})
		c.After(time.Second)
	}
	if a.paused {
		left = append(left, layout.StatusItem{ID: "pause", Text: "显示已暂停 · 接收继续", Tone: layout.StatusItemWarning})
	}
	if a.autoConnect {
		tone, text := layout.StatusItemInfo, "自动连接已开启"
		if a.portWatchError != "" {
			tone, text = layout.StatusItemWarning, "自动连接 · 端口检测失败"
		}
		left = append(left, layout.StatusItem{ID: "auto-connect", Text: text, Tone: tone, Secondary: true})
	}
	layout.StatusBar(c, layout.StatusBarOptions{Left: left, Right: []layout.StatusItem{
		{ID: "connection", Text: connection},
		{ID: "version", Text: "GUART " + version, Secondary: true},
	}})
}

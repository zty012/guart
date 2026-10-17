package main

import (
	"math"
	"runtime"

	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/code"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/core"
	"git.zacharyzhang.com/ZacharyZhang-NY/MujicaUI/theme"
	"github.com/egoist/mygo/ui"
)

type terminalPreview struct {
	input             string
	cols, rows, lines int
	state             code.TerminalState
	handle            ui.Handle
	anchor            code.TerminalPos
	dragging          bool
}

func (a *app) messageWidth(c *ui.Context) float32 {
	// Workspace padding, MessageList gutters, and MessageBubble padding.
	return max(180, min(732, a.workspaceWidth(c)-44-32-24-4))
}

func (p *terminalPreview) reflow(input string, cols int) {
	// Use Terminal's own parser and wrapping at its committed column count.
	// Reset the existing state rather than replacing it: Terminal configured
	// its scrollback capacity before we got here.
	const reset = "\x1b[0m\x1b[3J\x1b[H"
	p.state.Resize(cols, 1)
	p.state.Write(reset)
	p.state.Write(input)
	lines := p.state.Lines()
	rows := lines
	p.state.Resize(cols, rows)
	p.state.Write(reset)
	p.state.Write(input)
	p.input, p.cols, p.rows, p.lines = input, cols, rows, lines
}

func (a *app) terminalPreview(c *ui.Context, p message) *terminalPreview {
	input, plain := p.ansiText(p.Data)
	font := ui.Font{Family: theme.MonoFont, Size: core.FontSize(c, code.CodeSize)}
	cw := font.Size * 0.6
	if g := ui.Shape("M", font); len(g) > 0 {
		cw = g[0].Advance
	}
	m := font.Metrics()
	ch := float32(math.Ceil(float64((m.Ascent + m.Descent + m.LineGap) * 1.15)))
	width := a.messageWidth(c)
	box := ui.Box(c.Key("terminal-preview")).Width(width).Shrink(0).Clip()
	preview := ui.Local(box, "preview", func() terminalPreview { return terminalPreview{} })
	height := float32(max(1, preview.rows))*ch + 12 + 0.5
	box.Height(height).Children(func() {
		terminal := code.Terminal(c.Key("terminal"), &preview.state, code.TerminalOptions{
			Label: p.Direction + " ANSI 预览: " + plain,
			// The component otherwise silently drops lines beyond its default
			// 1000-line scrollback. One input byte can add at most one line.
			Scrollback: max(1000, len(input)+1),
		}).MinHeight(0).FillWidth().Height(height).Shrink(0)
		// Terminal's built-in wheel handler consumes scrolling even with no
		// scrollback. This read-only adapter keeps selection/copy but lets the
		// event bubble to MessageList instead of trapping the mouse wheel.
		terminal.Bind(&preview.handle).HandleInput(preview.readOnlyInput(c.Services(), cw, ch))
		// Terminal has now derived its grid from the actual laid-out bounds.
		// Replaying at those columns is necessary: Resize alone crops cells
		// instead of rewrapping the original message when the window narrows.
		cols, _ := preview.state.Size()
		if preview.input != input || preview.cols != cols || preview.rows == 0 {
			preview.reflow(input, cols)
			height = float32(preview.rows)*ch + 12 + 0.5
			terminal.Height(height)
			box.Height(height)
			c.Invalidate()
		}
	})
	return preview
}

func (p *terminalPreview) readOnlyInput(svc ui.Services, cw, ch float32) func(ui.InputEvent) bool {
	position := func(x, y float32) code.TerminalPos {
		return code.TerminalPos{
			Line: max(0, min(int((y-6)/ch), p.rows-1)),
			Col:  max(0, min(int((x-6)/cw), p.cols-1)),
		}
	}
	return func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerDown:
			if ev.Button != 0 {
				return false
			}
			p.dragging, p.anchor = true, position(ev.X, ev.Y)
			p.handle.Focus()
			p.state.Select(p.anchor, p.anchor)
			return true
		case ui.InputPointerMove:
			if p.dragging {
				p.state.Select(p.anchor, position(ev.X, ev.Y))
				return true
			}
		case ui.InputPointerUp:
			if p.dragging {
				p.dragging = false
				return true
			}
		case ui.InputKeyDown:
			copyMod := ui.Ctrl | ui.Shift
			if runtime.GOOS == "darwin" {
				copyMod = ui.Super
			}
			if ev.Key == ui.KeyC && ev.Mods == copyMod {
				if text := p.state.Selection(); text != "" {
					svc.WriteClipboard(text)
				}
				return true
			}
		}
		return false
	}
}

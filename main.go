package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

func main() {
	mygo.Use(native.New(updater.Options{Icon: appIconPNG, Language: "zh-Hans"}))
	a := newApp()
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{
			Title: "GUART · Serial Studio", Width: 1320, Height: 900,
			MinWidth: 960, MinHeight: 720, StateKey: "guart-main",
			// Keep the native minimize, maximize and close buttons, but let the
			// view draw its header underneath them (see c.TitleBar in view.go).
			TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 40,
			Content: ui.View(a.view),
		})
		if err := w.SetIcon(appIconPNG); err != nil {
			log.Printf("set window icon: %v", err)
		}
		a.window, a.post = w, w.Update
		w.OnClosed(a.shutdown)
		a.refreshPorts()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

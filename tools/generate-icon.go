//go:build ignore

// Generate the native packaging icon from the same SVG used in the header.
package main

import (
	"image/png"
	"log"
	"os"

	"github.com/egoist/mygo/ui"
)

func main() {
	src, err := os.ReadFile("resources/icon.svg")
	if err != nil {
		log.Fatal(err)
	}
	logo := ui.MustParseSVG(src)
	theme := *ui.LightTheme()
	theme.Background = ui.Color{}
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(&theme)
		ui.Image(c, logo).Size(1024, 1024)
	}, 1024, 1024)
	f, err := os.Create("resources/icon.png")
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(f, tt.Image()); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

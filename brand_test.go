package main

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAppIconMatchesSVG(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(appIconPNG))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 1024, 1024) {
		t.Fatalf("packaging icon must be 1024 square, got %v", img.Bounds())
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Fatal("icon corners must remain transparent")
	}
	theme := *ui.LightTheme()
	theme.Background = ui.Color{}
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(&theme)
		ui.Image(c, appLogo).Size(1024, 1024)
	}, 1024, 1024)
	var rendered bytes.Buffer
	if err := png.Encode(&rendered, tt.Image()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(appIconPNG, rendered.Bytes()) {
		t.Fatal("native icon differs from SVG; run go generate .")
	}
}

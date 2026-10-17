package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

//go:generate go run ./tools/generate-icon.go

//go:embed resources/icon.svg
var appIconSVG []byte

//go:embed resources/icon.png
var appIconPNG []byte

var appLogo = ui.MustParseSVG(appIconSVG)

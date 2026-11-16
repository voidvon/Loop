package desktop

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed assets/app-icon.png
var appIconPNG []byte

var appIcon = fyne.NewStaticResource("loop.png", appIconPNG)

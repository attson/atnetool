package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	linuxoptions "github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/linux/icon.png
var linuxAppIcon []byte

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:            "ATNetool - AT 网络助手",
		Width:            1120,
		Height:           780,
		MinWidth:         820,
		MinHeight:        650,
		BackgroundColour: &options.RGBA{R: 10, G: 13, B: 14, A: 1},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Linux: &linuxoptions.Options{
			Icon:             linuxAppIcon,
			ProgramName:      "ATNetool",
			WebviewGpuPolicy: linuxoptions.WebviewGpuPolicyNever,
		},
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

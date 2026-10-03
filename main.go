package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"time"

	"netpulsar/internal/locale"
	"netpulsar/internal/metrics"
	"netpulsar/internal/settings"
	"netpulsar/internal/skin"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsopts "github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend
var frontendFS embed.FS

//go:embed all:skins
var skinsFS embed.FS

//go:embed all:locales
var localesFS embed.FS

//go:embed assets/logo.png
var logoPNG []byte

const windowTitle = "NetPulsar"

func main() {
	if hasFlag("--check") {
		attachParentConsole()
		runCheck()
		return
	}

	store, err := settings.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "settings:", err)
		os.Exit(1)
	}
	if hasFlag("--reset") {
		store.ResetPlacement()
	}
	embedded, err := fs.Sub(skinsFS, "skins")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	catalog, err := skin.Open(skin.ResolveDir(), embedded)
	if err != nil {
		fmt.Fprintln(os.Stderr, "skins:", err)
		os.Exit(1)
	}
	localeFS, err := fs.Sub(localesFS, "locales")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	locales, err := locale.Open(locale.ResolveDir(), localeFS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "languages:", err)
		os.Exit(1)
	}
	front, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	app := newApp(store, catalog, locales)
	app.logo = logoPNG
	meta := catalog.Resolve(store.Get().Skin)
	if hasFlag("--reset") {
		app.forceCenter.Store(true)
	}

	err = wails.Run(&options.App{
		Title:            windowTitle,
		Width:            meta.Width,
		Height:           meta.Height,
		MinWidth:         180,
		MinHeight:        48,
		MaxWidth:         1200,
		MaxHeight:        1000,
		DisableResize:    true,
		Frameless:        true,
		StartHidden:      true,
		AlwaysOnTop:      store.Get().AlwaysOnTop,
		BackgroundColour: options.NewRGBA(0, 0, 0, 0),
		AssetServer: &assetserver.Options{
			Handler: assetHandler{
				frontend: front,
				skins:    catalog,
				current:  func() string { return store.Get().Skin },
			},
		},
		LogLevel:           logger.ERROR,
		LogLevelProduction: logger.ERROR,
		OnStartup:          app.startup,
		OnShutdown:         app.shutdown,
		Bind:               []any{app},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.netpulsar.widget",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				app.onSecond(data.Args)
			},
		},
		DragAndDrop: &options.DragAndDrop{DisableWebViewDrop: true},
		Windows: &wailsopts.Options{
			WebviewIsTransparent:              true,
			WindowIsTranslucent:               true,
			DisableFramelessWindowDecorations: true,
			BackdropType:                      wailsopts.None,
			DisablePinchZoom:                  true,
			Theme:                             wailsopts.Dark,
		},
		Mac: &mac.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			TitleBar:             mac.TitleBarHiddenInset(),
		},
		Linux: &linux.Options{
			WindowIsTranslucent: true,
			ProgramName:         "netpulsar",
			WebviewGpuPolicy:    linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func hasFlag(flag string) bool {
	for _, arg := range os.Args[1:] {
		if arg == flag {
			return true
		}
	}
	return false
}

func runCheck() {
	col := metrics.New()
	_ = col.Sample("cpu")
	time.Sleep(400 * time.Millisecond)
	snap := col.Sample("cpu")
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snap); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

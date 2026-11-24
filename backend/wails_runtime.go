package main

import (
	"context"
	"io/fs"
	"net/http"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"modhub/internal/desktop"
)

type wailsRuntimeAdapter struct{}

func (wailsRuntimeAdapter) Emit(ctx context.Context, eventName string, data any) {
	if ctx != nil {
		wailsrt.EventsEmit(ctx, eventName, data)
	}
}

func (wailsRuntimeAdapter) OpenDirectoryDialog(ctx context.Context, title string) (string, error) {
	return wailsrt.OpenDirectoryDialog(ctx, wailsrt.OpenDialogOptions{
		Title: title,
	})
}

func (wailsRuntimeAdapter) OpenJavaFileDialog(ctx context.Context, title string) (string, error) {
	return wailsrt.OpenFileDialog(ctx, wailsrt.OpenDialogOptions{
		Title: title,
		Filters: []wailsrt.FileFilter{
			{DisplayName: "Java Executable (java.exe; java)", Pattern: "java.exe;javaw.exe;java"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
}

func (wailsRuntimeAdapter) OpenModpackFileDialog(ctx context.Context, title string) (string, error) {
	return wailsrt.OpenFileDialog(ctx, wailsrt.OpenDialogOptions{
		Title: title,
		Filters: []wailsrt.FileFilter{
			{DisplayName: "Modpacks (*.mrpack; *.zip; *.modhub)", Pattern: "*.mrpack;*.zip;*.modhub"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
}

func (wailsRuntimeAdapter) SaveModpackDialog(ctx context.Context, title, defaultFilename, extPattern, extLabel string) (string, error) {
	return wailsrt.SaveFileDialog(ctx, wailsrt.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultFilename,
		Filters:         []wailsrt.FileFilter{{DisplayName: extLabel, Pattern: extPattern}},
	})
}

func (wailsRuntimeAdapter) SaveFileDialog(ctx context.Context, title, defaultFilename string) (string, error) {
	return wailsrt.SaveFileDialog(ctx, wailsrt.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultFilename,
		Filters: []wailsrt.FileFilter{
			{DisplayName: "ZIP Archive (*.zip)", Pattern: "*.zip"},
		},
	})
}

func (wailsRuntimeAdapter) BrowserOpenURL(ctx context.Context, rawURL string) {
	if ctx != nil {
		wailsrt.BrowserOpenURL(ctx, rawURL)
	}
}

func (wailsRuntimeAdapter) WindowMinimise(ctx context.Context) {
	if ctx != nil {
		wailsrt.WindowMinimise(ctx)
	}
}

func (wailsRuntimeAdapter) WindowHide(ctx context.Context) {
	if ctx != nil {
		wailsrt.WindowHide(ctx)
	}
}

func (wailsRuntimeAdapter) WindowShow(ctx context.Context) {
	if ctx != nil {
		wailsrt.WindowShow(ctx)
	}
}

func (wailsRuntimeAdapter) WindowToggleMaximise(ctx context.Context) {
	if ctx != nil {
		wailsrt.WindowToggleMaximise(ctx)
	}
}

func (wailsRuntimeAdapter) Quit(ctx context.Context) {
	if ctx != nil {
		wailsrt.Quit(ctx)
	}
}

func runWailsDesktop(
	bridge *desktop.Bridge,
	assetsFS fs.FS,
	apiHandler http.Handler,
	onShutdown func(),
) error {
	return wails.Run(&options.App{
		Title:            "ModHub",
		Width:            1360,
		Height:           860,
		MinWidth:         960,
		MinHeight:        640,
		Frameless:        true,
		DisableResize:    false,
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
			CSSDropProperty:    "--wails-drop-target",
			CSSDropValue:       "drop",
		},
		BackgroundColour: &options.RGBA{R: 17, G: 18, B: 20, A: 0},
		AssetServer: &assetserver.Options{
			Assets:  assetsFS,
			Handler: apiHandler,
		},
		OnStartup:     bridge.Startup,
		OnBeforeClose: bridge.BeforeClose,
		OnShutdown: func(ctx context.Context) {
			bridge.Shutdown(ctx)
			if onShutdown != nil {
				onShutdown()
			}
		},
		Bind: []interface{}{
			bridge,
		},
		Windows: &windows.Options{
			WebviewIsTransparent:              true,
			WindowIsTranslucent:               true,
			BackdropType:                      windows.Mica,
			DisableWindowIcon:                 false,
			Theme:                             windows.Dark,
			ResizeDebounceMS:                  16,
			DisableFramelessWindowDecorations: false,
		},
	})
}

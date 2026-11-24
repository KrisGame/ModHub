package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"modhub/internal/app"
	"modhub/internal/desktop"
	"modhub/internal/httpapi"
	"modhub/internal/storage"
)

func openBrowser(rawURL string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	case "darwin":
		cmd = exec.Command("open", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	_ = cmd.Start()
}

func resolveAppBaseDir() (string, string) {
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	baseDir := wd
	if exeDir != "" {
		if _, err := os.Stat(filepath.Join(exeDir, "frontend")); err == nil {
			baseDir = exeDir
		} else if _, ok := getEmbeddedDistFS(); ok {
			baseDir = exeDir
		}
	}
	if _, err := os.Stat(filepath.Join(baseDir, "frontend")); err != nil {
		parent := filepath.Dir(wd)
		if _, err := os.Stat(filepath.Join(parent, "frontend")); err == nil {
			baseDir = parent
		}
	}
	return baseDir, exeDir
}

func isWebModeRequested() bool {
	if strings.TrimSpace(os.Getenv("MODHUB_WEB")) == "1" {
		return true
	}
	for _, arg := range os.Args[1:] {
		if arg == "--web" || arg == "-web" {
			return true
		}
	}
	return false
}

func isAlreadyRunning(appURL string) bool {
	client := &http.Client{Timeout: 600 * time.Millisecond}
	resp, err := client.Get(appURL + "/api/health")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func listenWithRetry(addr string, isRestart bool) (net.Listener, error) {
	attempts := 1
	if isRestart {
		attempts = 20
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, nil
		}
		lastErr = err
		if i+1 < attempts {
			time.Sleep(150 * time.Millisecond)
		}
	}
	return nil, lastErr
}

func main() {
	baseDir, exeDir := resolveAppBaseDir()
	wd, _ := os.Getwd()

	dataInfo, err := desktop.ResolveDataDir(exeDir, wd)
	if err != nil {
		log.Fatalf("[ModHub] Failed to resolve data directory: %v", err)
	}
	if dataInfo.MigratedFrom != "" {
		log.Printf("[ModHub] Migrated legacy user data from %s -> %s (backup: %s)", dataInfo.MigratedFrom, dataInfo.Dir, dataInfo.BackupPath)
	} else {
		log.Printf("[ModHub] Using data directory (%s): %s", dataInfo.Mode, dataInfo.Dir)
	}

	diskDist := filepath.Join(baseDir, "backend", "webdist")
	if _, err := os.Stat(filepath.Join(diskDist, "index.html")); err != nil {
		diskDist = filepath.Join(baseDir, "frontend", "dist")
	}

	db, err := storage.OpenDatabase(filepath.Join(dataInfo.Dir, "modhub.db"))
	if err != nil {
		log.Fatalf("[ModHub] SQLite init failed: %v", err)
	}

	container := app.NewContainer(dataInfo.Dir, db)
	var embeddedFS fs.FS
	if efs, ok := getEmbeddedDistFS(); ok {
		embeddedFS = efs
	}
	srv := httpapi.NewServer(container, diskDist, embeddedFS)

	if cat, _ := db.Stats(); cat < 40 {
		container.TriggerBackgroundSync(false)
	}

	var shutdownOnce sync.Once
	shutdownCore := func() {
		shutdownOnce.Do(func() {
			container.Close()
			_ = db.Close()
		})
	}

	// Legacy / dev browser mode: `modhub.exe --web` or `MODHUB_WEB=1`
	if isWebModeRequested() {
		runWebMode(container, srv, shutdownCore)
		return
	}

	// Default Desktop Mode: native Wails v2 window (no localhost HTTP listener required).
	// Static frontend assets come from embeddedFS (or diskDist fallback), and /api/* requests
	// are served in-memory by Wails AssetServer.Handler.
	assetsFS := embeddedFS
	if assetsFS == nil {
		if _, statErr := os.Stat(filepath.Join(diskDist, "index.html")); statErr == nil {
			assetsFS = os.DirFS(diskDist)
		}
	}
	bridge := desktop.NewBridge(container, dataInfo, wailsRuntimeAdapter{})
	if err := runWailsDesktop(bridge, assetsFS, srv.APIHandler(), shutdownCore); err != nil {
		shutdownCore()
		log.Fatalf("[ModHub] Desktop window error: %v", err)
	}
	shutdownCore()
}

func runWebMode(container *app.Container, srv *httpapi.Server, shutdownCore func()) {
	cfg := container.GetConfig()
	port := cfg.Port
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); p > 0 && err == nil {
			port = p
		}
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	appURL := "http://" + addr
	isRestart := os.Getenv("MODHUB_RESTART") == "1"

	ln, err := listenWithRetry(addr, isRestart)
	if err != nil {
		shutdownCore()
		if isAlreadyRunning(appURL) {
			openBrowser(appURL)
			return
		}
		log.Fatalf("[ModHub] Failed to bind %s: %v", addr, err)
	}

	httpServer := &http.Server{
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		log.Printf("[ModHub] Web mode listening on %s", appURL)
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[ModHub] HTTP server error: %v", err)
		}
	}()

	if os.Getenv("NO_BROWSER") != "1" {
		go func() {
			time.Sleep(450 * time.Millisecond)
			openBrowser(appURL)
		}()
	}

	shutdownWeb := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		shutdownCore()
	}

	if os.Getenv("NO_TRAY") == "1" {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		shutdownWeb()
		return
	}

	RunSystemTray("ModHub — "+addr, TrayCallbacks{
		OnOpen: func() { openBrowser(appURL) },
		OnRestart: func() {
			exe, err := os.Executable()
			shutdownWeb()
			if err == nil && exe != "" {
				cmd := exec.Command(exe, "--web")
				cmd.Env = append(os.Environ(), "MODHUB_RESTART=1", "NO_BROWSER=1")
				_ = cmd.Start()
			}
			os.Exit(0)
		},
		OnExit: func() {
			shutdownWeb()
			os.Exit(0)
		},
	})
	shutdownWeb()
}

package httpapi

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"modhub/internal/app"
	"modhub/internal/domain"
	"modhub/internal/downloads"
	"modhub/internal/instances"
	"modhub/internal/launcher"
	"modhub/internal/modloader"
	"modhub/internal/modpacks"
	"modhub/internal/netx"
	"modhub/internal/packio"
	javart "modhub/internal/runtime"
)

var (
	reUnsafeFile   = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	reSafeFilename = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
)

// Server is the thin HTTP transport adapter over app.Container.
type Server struct {
	container  *app.Container
	publicDir  string
	embeddedFS fs.FS
	exports    exportRegistry
}

func NewServer(container *app.Container, publicDir string, embeddedFS fs.FS) *Server {
	return &Server{
		container:  container,
		publicDir:  publicDir,
		embeddedFS: embeddedFS,
	}
}

func (s *Server) mountAPIRoutes(r chi.Router) {
	r.Route("/api", func(api chi.Router) {
		api.Use(localAPISecurityMiddleware)
		api.Get("/media/icon", s.handleMediaIcon)
		api.Get("/download-file", s.handleDownloadFile)
		api.Get("/modpacks/export-file", s.handleDownloadExportFile)
		api.Get("/instances/worlds/export-file", s.handleDownloadWorldFile)
		api.Post("/download-pack", s.handleDownloadPack)

		api.Group(func(gz chi.Router) {
			gz.Use(middleware.Compress(5))
			gz.Get("/health", s.handleHealth)
			gz.Get("/config", s.handleGetConfig)
			gz.Post("/config", s.handleUpdateConfig)
			gz.Post("/config/test-cf", s.handleTestCF)
			gz.Get("/network/check", s.handleNetworkCheck)

			gz.Get("/index/status", s.handleIndexStatus)
			gz.Get("/index/snapshot", s.handleIndexSnapshot)
			gz.Post("/index/sync", s.handleIndexSync)
			gz.Post("/cache/clear", s.handleCacheClear)

			gz.Get("/mc-versions", s.handleMCVersions)
			gz.Get("/filters", s.handleFilters)
			gz.Get("/search", s.handleSearch)

			gz.Get("/overrides", s.handleGetOverrides)
			gz.Post("/overrides/merge", s.handleOverridesMerge)
			gz.Post("/overrides/split", s.handleOverridesSplit)
			gz.Post("/overrides/delete", s.handleOverridesDelete)
			gz.Post("/overrides/reset", s.handleOverridesReset)

			gz.Get("/packs", s.handleGetPacks)
			gz.Post("/packs/save", s.handleSavePack)
			gz.Post("/packs/delete", s.handleDeletePack)

			gz.Post("/mods/check-installed", s.handleCheckInstalled)

			gz.Post("/mod/details", s.handleModDetails)
			gz.Post("/mod/versions", s.handleModVersions)
			gz.Post("/mod/quick-file", s.handleQuickFile)
			gz.Post("/mod/dependents", s.handleDependents)
			gz.Post("/mod/file-changelog", s.handleFileChangelog)
			gz.Post("/mod/dependency-tree", s.handleDependencyTree)
			gz.Post("/resolve-url", s.handleResolveURL)
			gz.Post("/resolve-dependencies", s.handleResolveDependencies)

			// Centralized Download Manager endpoints (web/dev fallback)
			gz.Get("/downloads", s.handleGetDownloads)
			gz.Post("/downloads/add", s.handleAddDownload)
			gz.Post("/downloads/pause", s.handlePauseDownload)
			gz.Post("/downloads/resume", s.handleResumeDownload)
			gz.Post("/downloads/cancel", s.handleCancelDownload)
			gz.Post("/downloads/retry", s.handleRetryDownload)
			gz.Post("/downloads/remove", s.handleRemoveDownload)
			gz.Post("/downloads/clear-finished", s.handleClearFinishedDownloads)

			// Java Runtime Manager endpoints (web/dev fallback)
			gz.Get("/java", s.handleGetJavaRuntimes)
			gz.Post("/java/scan", s.handleScanJavaRuntimes)
			gz.Post("/java/add-custom", s.handleAddCustomJava)
			gz.Post("/java/verify", s.handleVerifyJava)
			gz.Post("/java/install", s.handleInstallJava)
			gz.Post("/java/cancel-install", s.handleCancelJavaInstall)
			gz.Post("/java/reinstall", s.handleReinstallJava)
			gz.Post("/java/remove", s.handleRemoveJava)
			gz.Get("/java/recommend", s.handleRecommendJava)

			// Instance Manager endpoints (web/dev fallback)
			gz.Get("/instances", s.handleListInstances)
			gz.Get("/instances/{id}/mods", s.handleListInstanceMods)
			gz.Post("/instances/mods/toggle", s.handleSetInstanceModEnabled)
			gz.Post("/instances/mods/toggle-all", s.handleSetAllInstanceModsEnabled)
			gz.Post("/instances/mods/delete", s.handleDeleteInstanceMod)
			gz.Post("/instances/mods/delete-batch", s.handleDeleteMultipleInstanceMods)
			gz.Get("/instances/{id}/mods/updates", s.handleCheckInstanceModUpdates)
			gz.Post("/instances/mods/update", s.handleApplyInstanceModUpdate)
			gz.Post("/instances/mods/install", s.handleInstallInstanceMod)
			gz.Get("/instances/{id}/resourcepacks", s.handleListInstanceResourcePacks)
			gz.Post("/instances/resourcepacks/toggle", s.handleSetInstanceResourcePackEnabled)
			gz.Post("/instances/resourcepacks/delete", s.handleDeleteInstanceResourcePack)
			gz.Get("/instances/{id}/shaders", s.handleListInstanceShaderPacks)
			gz.Post("/instances/shaders/toggle", s.handleSetInstanceShaderPackEnabled)
			gz.Post("/instances/shaders/delete", s.handleDeleteInstanceShaderPack)
			gz.Get("/instances/{id}/worlds", s.handleListInstanceWorlds)
			gz.Post("/instances/worlds/export", s.handleExportInstanceWorld)
			gz.Get("/instances/{id}", s.handleGetInstance)
			gz.Post("/instances/create", s.handleCreateInstance)
			gz.Post("/instances/update", s.handleUpdateInstance)
			gz.Post("/instances/duplicate", s.handleDuplicateInstance)
			gz.Post("/instances/delete", s.handleDeleteInstance)
			gz.Post("/instances/verify", s.handleVerifyInstance)
			gz.Post("/instances/repair", s.handleRepairInstance)
			gz.Get("/instances/{id}/files", s.handleGetInstanceFiles)

			gz.Get("/minecraft/versions", s.handleGetMinecraftManifest)
			gz.Get("/modloaders/versions", s.handleGetModloaderVersions)
			gz.Post("/instances/install-version", s.handleInstallProfileVersion)
			gz.Post("/instances/cancel-install", s.handleCancelProfileInstall)
			gz.Get("/instances/{id}/active-install", s.handleGetActiveProfileInstall)
			gz.Get("/instances/{id}/installed-versions", s.handleListProfileInstalledVersions)
			gz.Post("/instances/activate-version", s.handleActivateProfileVersion)
			gz.Post("/instances/verify-version", s.handleVerifyProfileVersion)
			gz.Post("/instances/delete-version", s.handleDeleteProfileVersion)

			// Microsoft & Offline Account Manager endpoints (web/dev fallback, token-free)
			gz.Get("/accounts", s.handleGetAccounts)
			gz.Post("/accounts/offline", s.handleAddOfflineAccount)
			gz.Post("/accounts/login/start", s.handleStartMicrosoftLogin)
			gz.Post("/accounts/login/cancel", s.handleCancelMicrosoftLogin)
			gz.Post("/accounts/active", s.handleSetActiveAccount)
			gz.Post("/accounts/refresh", s.handleRefreshAccount)
			gz.Post("/accounts/check-license", s.handleCheckAccountLicense)
			gz.Post("/accounts/signout", s.handleSignOutAccount)
			gz.Post("/accounts/delete", s.handleDeleteAccount)
			gz.Post("/accounts/{id}/skin", s.handleUpdateAccountSkin)

			// Game Launcher & Process Monitoring endpoints (web/dev fallback)
			gz.Get("/launcher/snapshot", s.handleLauncherSnapshot)
			gz.Post("/launcher/preflight", s.handleLauncherPreflight)
			gz.Post("/launcher/launch", s.handleLauncherLaunch)
			gz.Post("/launcher/stop", s.handleLauncherStop)
			gz.Get("/launcher/instances/{id}/session", s.handleLauncherInstanceSession)
			gz.Get("/launcher/instances/{id}/history", s.handleLauncherSessionHistory)
			gz.Get("/launcher/instances/{id}/logs", s.handleLauncherLogs)
			gz.Get("/launcher/instances/{id}/crashes", s.handleLauncherCrashReports)
			gz.Get("/launcher/instances/{id}/crash-diagnosis", s.handleLauncherCrashDiagnosis)

			gz.Get("/modpacks", s.handleListModpacks)
			gz.Get("/modpacks/progress", s.handleGetModpackProgress)
			gz.Post("/modpacks/preview-import", s.handlePreviewModpackImport)
			gz.Post("/modpacks/upload-import", s.handleUploadModpackImport)
			gz.Post("/modpacks/execute-import", s.handleExecuteModpackImport)
			gz.Post("/modpacks/export", s.handleExportModpack)
			gz.Post("/modpacks/preview-update", s.handlePreviewModpackUpdate)
			gz.Post("/modpacks/apply-update", s.handleApplyModpackUpdate)
			gz.Post("/modpacks/delete", s.handleDeleteModpack)
		})
	})

}

// APIHandler serves only /api/* routes directly in-memory for Wails AssetServer.Handler
// without starting any localhost TCP listener.
func (s *Server) APIHandler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowOriginFunc: func(_ *http.Request, origin string) bool {
			return isAllowedLocalOrigin(origin)
		},
		AllowedMethods: []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Content-Type"},
	}))
	s.mountAPIRoutes(r)
	return r
}

// Router serves both /api/* and embedded/disk static frontend assets (for --web and dev modes).
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowOriginFunc: func(_ *http.Request, origin string) bool {
			return isAllowedLocalOrigin(origin)
		},
		AllowedMethods: []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Content-Type"},
	}))
	s.mountAPIRoutes(r)
	r.Get("/*", s.serveFrontend)
	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"runtime":        "go-chi-sqlite",
		"schema_version": s.container.DB.SchemaVersion(),
	})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.container.PublicConfigPayload())
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurseForgeAPIKey       *string `json:"curseforge_api_key"`
		MergeThreshold         *int    `json:"merge_threshold"`
		ProxyURL               *string `json:"proxy_url"`
		AutoInstallMediumDeps  *bool   `json:"auto_install_medium_deps"`
		MaxConcurrentDownloads *int    `json:"max_concurrent_downloads"`
		MicrosoftClientID      *string `json:"microsoft_client_id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	payload, err := s.container.UpdateConfig(app.ConfigUpdatePatch{
		CurseForgeAPIKey:       body.CurseForgeAPIKey,
		MergeThreshold:         body.MergeThreshold,
		ProxyURL:               body.ProxyURL,
		AutoInstallMediumDeps:  body.AutoInstallMediumDeps,
		MaxConcurrentDownloads: body.MaxConcurrentDownloads,
		MicrosoftClientID:      body.MicrosoftClientID,
	})
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleNetworkCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.container.CheckNetwork())
}

func (s *Server) handleTestCF(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurseForgeAPIKey *string `json:"curseforge_api_key"`
		APIKey           *string `json:"api_key"`
	}
	if !decodeJSONBody(w, r, &body, true) {
		return
	}
	key := ""
	if body.CurseForgeAPIKey != nil {
		key = *body.CurseForgeAPIKey
	} else if body.APIKey != nil {
		key = *body.APIKey
	} else {
		key = s.container.GetConfig().CurseForgeAPIKey
	}
	if err := s.container.TestCurseForgeKey(key); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Ключ CurseForge действителен"})
}

func (s *Server) handleIndexStatus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("auto") == "1" {
		if cat, _ := s.container.DB.Stats(); cat < 80 {
			s.container.TriggerBackgroundSync(false)
		}
	}
	writeJSON(w, http.StatusOK, s.container.IndexStatusPayload())
}

func (s *Server) handleIndexSnapshot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.container.IndexSnapshot())
}

func (s *Server) handleIndexSync(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Full bool `json:"full"`
	}
	if !decodeJSONBody(w, r, &body, true) {
		return
	}
	started := s.container.TriggerBackgroundSync(body.Full)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"started": started,
		"status":  s.container.IndexStatusPayload(),
	})
}

func (s *Server) handleCacheClear(w http.ResponseWriter, _ *http.Request) {
	s.container.ClearAllCaches()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMediaIcon(w http.ResponseWriter, r *http.Request) {
	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	data, ctype, etag, err := s.container.Icons.GetCachedIcon(r.Context(), rawURL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (s *Server) handleMCVersions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.container.GetMCVersions())
}

func (s *Server) handleFilters(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.container.GetFilters(strings.TrimSpace(r.URL.Query().Get("type"))))
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if err := s.container.HTTP.ValidateDLURL(rawURL); err != nil {
		writeErr(w, http.StatusBadRequest, "Недопустимый источник загрузки: "+err.Error())
		return
	}
	fn := filepath.Base(strings.TrimSpace(r.URL.Query().Get("filename")))
	fn = reUnsafeFile.ReplaceAllString(fn, "_")
	if fn == "" || fn == "." || fn == "/" || fn == ".." {
		fn = "mod.jar"
	}
	resp, err := s.container.HTTP.OpenDownloadStream(r.Context(), rawURL, netx.MaxDownloadFileBytes)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Ошибка загрузки файла: "+err.Error())
		return
	}
	defer resp.Body.Close()

	asciiFn := strings.Map(func(r rune) rune {
		if r >= 0x20 && r <= 0x7e && r != '"' && r != '\\' {
			return r
		}
		return '_'
	}, fn)
	if strings.Trim(asciiFn, "_") == "" {
		asciiFn = "mod.jar"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiFn, url.PathEscape(fn)))
	w.Header().Set("Cache-Control", "no-store")
	if resp.ContentLength > 0 && resp.ContentLength <= netx.MaxDownloadFileBytes {
		w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = netx.CopyWithLimit(r.Context(), w, resp.Body, netx.MaxDownloadFileBytes)
	}
}

func (s *Server) handleGetOverrides(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "overrides": s.container.GetOverrides()})
}

func (s *Server) handleOverridesMerge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourceKeys []string `json:"source_keys"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	ov, err := s.container.AddMergeOverride(body.SourceKeys)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "overrides": ov})
}

func (s *Server) handleOverridesSplit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourceKeys []string `json:"source_keys"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	ov, err := s.container.AddSplitOverride(body.SourceKeys)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "overrides": ov})
}

func (s *Server) handleOverridesDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind  string `json:"kind"`
		Index int    `json:"index"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	ov, err := s.container.DeleteOverride(body.Kind, body.Index)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "overrides": ov})
}

func (s *Server) handleOverridesReset(w http.ResponseWriter, _ *http.Request) {
	ov, err := s.container.ResetOverrides()
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "overrides": ov})
}

func (s *Server) handleGetPacks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "packs": s.container.GetPacks()})
}

func (s *Server) handleSavePack(w http.ResponseWriter, r *http.Request) {
	var pack domain.SavedPack
	if !decodeJSONBody(w, r, &pack, false) {
		return
	}
	packs, err := s.container.SavePack(pack)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "packs": packs})
}

func (s *Server) handleDeletePack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	packs, err := s.container.DeletePack(body.ID)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "packs": packs})
}

func (s *Server) handleCheckInstalled(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hashes    []string `json:"hashes"`
		MCVersion string   `json:"mc_version"`
		Loader    string   `json:"loader"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	payload, err := s.container.CheckInstalledHashes(r.Context(), body.Hashes, body.MCVersion, body.Loader)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	page, _ := strconv.Atoi(qs.Get("page"))
	limit, _ := strconv.Atoi(qs.Get("limit"))
	mrOff, _ := strconv.Atoi(qs.Get("mr_offset"))
	cfOff, _ := strconv.Atoi(qs.Get("cf_offset"))
	updWithin, _ := strconv.Atoi(qs.Get("updated_within"))

	loadersIn := splitCSV(qs.Get("loaders_in"))
	if len(loadersIn) == 0 && strings.TrimSpace(qs.Get("loader")) != "" {
		loadersIn = []string{strings.TrimSpace(qs.Get("loader"))}
	}
	params := domain.SearchParams{
		Query:         strings.TrimSpace(qs.Get("q")),
		ContentType:   qs.Get("type"),
		Version:       strings.TrimSpace(qs.Get("version")),
		LoadersIn:     loadersIn,
		LoadersOut:    splitCSV(qs.Get("loaders_out")),
		CategoriesIn:  splitCSV(qs.Get("categories")),
		CategoriesOut: splitCSV(qs.Get("categories_out")),
		CatMode:       qs.Get("cat_mode"),
		Env:           strings.TrimSpace(qs.Get("env")),
		UpdatedWithin: updWithin,
		HideAbandoned: qs.Get("hide_abandoned") == "1",
		Sort:          qs.Get("sort"),
		Sources:       splitCSV(qs.Get("sources")),
		AutoMerge:     qs.Get("merge") != "0",
		Page:          page,
		Limit:         limit,
		MROffset:      mrOff,
		CFOffset:      cfOff,
	}

	raw, hit, err := s.container.Search(r.Context(), qs.Encode(), params)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeServiceErr(w, err)
		return
	}
	writeRawJSON(w, raw, hit)
}

func (s *Server) handleResolveURL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	item, err := s.container.ResolveURL(r.Context(), body.URL)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item})
}

func (s *Server) handleModDetails(w http.ResponseWriter, r *http.Request) {
	var body app.ModDetailsInput
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	raw, hit, err := s.container.GetModDetails(r.Context(), body)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeServiceErr(w, err)
		return
	}
	writeRawJSON(w, raw, hit)
}

func (s *Server) handleModVersions(w http.ResponseWriter, r *http.Request) {
	var body app.ModVersionsInput
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	writeJSON(w, http.StatusOK, s.container.GetModVersions(r.Context(), body))
}

func (s *Server) handleQuickFile(w http.ResponseWriter, r *http.Request) {
	var req domain.QuickFileRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	payload, err := s.container.GetQuickFile(r.Context(), req)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleDependents(w http.ResponseWriter, r *http.Request) {
	var req domain.DependentsRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	raw, hit, err := s.container.GetDependents(r.Context(), req)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeServiceErr(w, err)
		return
	}
	writeRawJSON(w, raw, hit)
}

func (s *Server) handleFileChangelog(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Platform string `json:"platform"`
		ModID    string `json:"mod_id"`
		FileID   string `json:"file_id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	payload, err := s.container.GetFileChangelog(r.Context(), body.Platform, body.ModID, body.FileID)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleDependencyTree(w http.ResponseWriter, r *http.Request) {
	var req domain.DepTreeRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	raw, hit, err := s.container.GetDependencyTree(r.Context(), req)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeServiceErr(w, err)
		return
	}
	writeRawJSON(w, raw, hit)
}

func (s *Server) handleResolveDependencies(w http.ResponseWriter, r *http.Request) {
	var req domain.DepResolveRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	payload, err := s.container.ResolveDependencies(r.Context(), req)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleDownloadPack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PackName string            `json:"pack_name"`
		Items    []domain.CartItem `json:"items"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	if err := packio.ValidatePackItems(body.Items, s.container.HTTP.ValidateDLURL); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.Trim(reSafeFilename.ReplaceAllString(strings.TrimSpace(body.PackName), "-"), "-")
	if name == "" {
		name = "modhub-pack"
	}
	tmp, err := os.CreateTemp("", "modhub-pack-*.zip")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Не удалось подготовить ZIP-сборку")
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	defer tmp.Close()
	summary, err := packio.StreamPackZIPWithReport(r.Context(), tmp, s.container.HTTP.WithContext(r.Context()), body.Items)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Не удалось собрать ZIP: "+err.Error())
		return
	}
	info, err := tmp.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Не удалось проверить готовую ZIP-сборку")
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		writeErr(w, http.StatusInternalServerError, "Не удалось прочитать готовую ZIP-сборку")
		return
	}
	packStatus := "complete"
	if len(summary.FailedFiles) > 0 {
		packStatus = "partial"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("X-ModHub-Pack-Downloaded", strconv.Itoa(summary.WrittenCount))
	w.Header().Set("X-ModHub-Pack-Total", strconv.Itoa(summary.TotalRequested))
	w.Header().Set("X-ModHub-Pack-Failed", strconv.Itoa(len(summary.FailedFiles)))
	w.Header().Set("X-ModHub-Pack-Status", packStatus)
	if len(summary.FailedFiles) > 0 {
		errSummary := strings.Join(summary.FailedFiles, " | ")
		if len(errSummary) > 800 {
			errSummary = errSummary[:800]
		}
		w.Header().Set("X-ModHub-Pack-Errors", url.QueryEscape(errSummary))
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, tmp)
}

func (s *Server) handleGetDownloads(w http.ResponseWriter, _ *http.Request) {
	snap := s.container.Downloads.GetSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tasks": snap.Tasks, "summary": snap.Summary})
}

func (s *Server) handleAddDownload(w http.ResponseWriter, r *http.Request) {
	var req downloads.DownloadRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	v, err := s.container.Downloads.Enqueue(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": v})
}

func (s *Server) handlePauseDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	v, err := s.container.Downloads.Pause(body.ID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": v})
}

func (s *Server) handleResumeDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	v, err := s.container.Downloads.Resume(body.ID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": v})
}

func (s *Server) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	v, err := s.container.Downloads.Cancel(body.ID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": v})
}

func (s *Server) handleRetryDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	v, err := s.container.Downloads.Retry(body.ID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": v})
}

func (s *Server) handleRemoveDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	if err := s.container.Downloads.Remove(body.ID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleClearFinishedDownloads(w http.ResponseWriter, _ *http.Request) {
	removed := s.container.Downloads.ClearFinished()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": removed})
}

func (s *Server) handleGetJavaRuntimes(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	snap := s.container.Java.GetSnapshot(r.Context(), refresh)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                    true,
		"runtimes":              snap.Runtimes,
		"available":             snap.Available,
		"installing_components": snap.InstallingComponents,
		"runtimes_dir":          snap.RuntimesDir,
		"platform_key":          snap.PlatformKey,
		"metadata_error":        snap.MetadataError,
	})
}

func (s *Server) handleScanJavaRuntimes(w http.ResponseWriter, r *http.Request) {
	s.container.Java.ScanInstalled(r.Context())
	_, _ = s.container.Java.RefreshAvailable(r.Context())
	snap := s.container.Java.GetSnapshot(r.Context(), false)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                    true,
		"runtimes":              snap.Runtimes,
		"available":             snap.Available,
		"installing_components": snap.InstallingComponents,
		"runtimes_dir":          snap.RuntimesDir,
		"platform_key":          snap.PlatformKey,
		"metadata_error":        snap.MetadataError,
	})
}

func (s *Server) handleAddCustomJava(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	rt, err := s.container.Java.AddCustomPath(r.Context(), body.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runtime": rt})
}

func (s *Server) handleVerifyJava(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	rt, err := s.container.Java.VerifyRuntime(r.Context(), body.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "runtime": rt, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runtime": rt})
}

func (s *Server) handleInstallJava(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Component string `json:"component"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	if err := s.container.Java.InstallRuntimeAsync(body.Component); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCancelJavaInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Component string `json:"component"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	s.container.Java.CancelInstall(body.Component)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleReinstallJava(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	if err := s.container.Java.ReinstallRuntimeAsync(body.ID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRemoveJava(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	if err := s.container.Java.RemoveRuntime(body.ID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRecommendJava(w http.ResponseWriter, r *http.Request) {
	mc := strings.TrimSpace(r.URL.Query().Get("mc"))
	ld := strings.TrimSpace(r.URL.Query().Get("loader"))
	req, rt := s.container.Java.RecommendForMC(javart.RequirementInput{MCVersion: mc, Loader: ld})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "requirement": req, "recommended": rt})
}

func (s *Server) handleListInstances(w http.ResponseWriter, _ *http.Request) {
	snap := s.container.Instances.List()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "instances": snap.Instances, "paths": snap.Paths})
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	inst, err := s.container.Instances.Get(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "instance": inst})
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var req instances.CreateInstanceRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	inst, err := s.container.CreateInstanceAndPrepare(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "instance": inst})
}

func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	var req instances.UpdateInstanceRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	inst, err := s.container.Instances.Update(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "instance": inst})
}

func (s *Server) handleDuplicateInstance(w http.ResponseWriter, r *http.Request) {
	var req instances.DuplicateInstanceRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	inst, err := s.container.Instances.Duplicate(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "instance": inst})
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	var req instances.DeleteInstanceRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	if err := s.container.Instances.Delete(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleVerifyInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	rep, err := s.container.CheckAndRepairInstance(body.ID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "report": rep})
}

func (s *Server) handleRepairInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	rep, err := s.container.Instances.Repair(body.ID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "report": rep})
}

func (s *Server) handleGetInstanceFiles(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	sum, err := s.container.Instances.GetFilesSummary(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "files": sum})
}

func (s *Server) handleListInstanceMods(w http.ResponseWriter, r *http.Request) {
	mods, err := s.container.Mods.List(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, instances.ErrInstanceNotFound) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mods": mods})
}

func (s *Server) handleSetInstanceModEnabled(w http.ResponseWriter, r *http.Request) {
	var req instances.ModActionRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	if req.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "не указано состояние enabled")
		return
	}
	mods, err := s.container.Mods.SetEnabled(req.InstanceID, req.Filename, *req.Enabled)
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mods": mods})
}

func (s *Server) handleDeleteInstanceMod(w http.ResponseWriter, r *http.Request) {
	var req instances.ModActionRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	mods, err := s.container.Mods.Delete(req.InstanceID, req.Filename)
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mods": mods})
}

func (s *Server) handleInstallInstanceMod(w http.ResponseWriter, r *http.Request) {
	var req instances.InstallModRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	result, err := s.container.Mods.Install(req)
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (s *Server) handleSetAllInstanceModsEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceID string `json:"id"`
		Enabled    bool   `json:"enabled"`
	}
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	mods, err := s.container.Mods.SetAllEnabled(req.InstanceID, req.Enabled)
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mods": mods})
}

func (s *Server) handleDeleteMultipleInstanceMods(w http.ResponseWriter, r *http.Request) {
	var req instances.BatchModActionRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	mods, err := s.container.Mods.DeleteBatch(req.InstanceID, req.Filenames)
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mods": mods})
}

func (s *Server) handleCheckInstanceModUpdates(w http.ResponseWriter, r *http.Request) {
	updates, err := s.container.Mods.CheckUpdates(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updates": updates})
}

func (s *Server) handleApplyInstanceModUpdate(w http.ResponseWriter, r *http.Request) {
	var req instances.ApplyModUpdateRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	res, err := s.container.Mods.ApplyUpdate(req)
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
}

func writeInstanceModErr(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, instances.ErrInstanceNotFound) {
		status = http.StatusNotFound
	}
	writeErr(w, status, err.Error())
}

func (s *Server) handleListInstanceResourcePacks(w http.ResponseWriter, r *http.Request) {
	packs, err := s.container.Content.ListResourcePacks(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "resourcepacks": packs})
}

func (s *Server) handleSetInstanceResourcePackEnabled(w http.ResponseWriter, r *http.Request) {
	var req instances.ContentActionRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	if req.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "не указано состояние enabled")
		return
	}
	packs, err := s.container.Content.SetResourcePackEnabled(req.InstanceID, req.Filename, *req.Enabled)
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "resourcepacks": packs})
}

func (s *Server) handleDeleteInstanceResourcePack(w http.ResponseWriter, r *http.Request) {
	var req instances.ContentActionRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	packs, err := s.container.Content.DeleteResourcePack(req.InstanceID, req.Filename)
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "resourcepacks": packs})
}

func (s *Server) handleListInstanceShaderPacks(w http.ResponseWriter, r *http.Request) {
	shaders, err := s.container.Content.ListShaderPacks(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "shaders": shaders})
}

func (s *Server) handleSetInstanceShaderPackEnabled(w http.ResponseWriter, r *http.Request) {
	var req instances.ContentActionRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	if req.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "не указано состояние enabled")
		return
	}
	shaders, err := s.container.Content.SetShaderPackEnabled(req.InstanceID, req.Filename, *req.Enabled)
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "shaders": shaders})
}

func (s *Server) handleDeleteInstanceShaderPack(w http.ResponseWriter, r *http.Request) {
	var req instances.ContentActionRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	shaders, err := s.container.Content.DeleteShaderPack(req.InstanceID, req.Filename)
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "shaders": shaders})
}

func (s *Server) handleListInstanceWorlds(w http.ResponseWriter, r *http.Request) {
	worlds, err := s.container.Content.ListWorlds(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		writeInstanceModErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "worlds": worlds})
}

func (s *Server) handleExportInstanceWorld(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceID string `json:"id"`
		Folder     string `json:"folder"`
	}
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	base := filepath.Join(s.container.DataDir, "exports", "worlds")
	if err := os.MkdirAll(base, 0o700); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir, err := os.MkdirTemp(base, "world-*")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	res, err := s.container.Content.ExportWorldTo(r.Context(), req.InstanceID, req.Folder, filepath.Join(dir, "world.zip"))
	if err != nil {
		_ = os.Remove(dir)
		writeInstanceModErr(w, err)
		return
	}
	key, err := s.registerExport(base, res.ArchivePath, res.Filename)
	if err != nil {
		_ = os.Remove(res.ArchivePath)
		_ = os.Remove(dir)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	res.DownloadURL = "/api/instances/worlds/export-file?token=" + url.QueryEscape(key)
	res.ArchivePath = ""
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "export": res})
}

func (s *Server) handleDownloadWorldFile(w http.ResponseWriter, r *http.Request) {
	s.serveRegisteredExport(w, r, filepath.Join(s.container.DataDir, "exports", "worlds"), ".zip")
}

func (s *Server) handleGetMinecraftManifest(w http.ResponseWriter, r *http.Request) {
	res, err := s.container.MCVersions.FetchManifest(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetModloaderVersions(w http.ResponseWriter, r *http.Request) {
	res, err := s.container.Modloaders.ListLoaderVersions(r.Context(), modloader.LoaderType(strings.TrimSpace(r.URL.Query().Get("loader"))), strings.TrimSpace(r.URL.Query().Get("mc")))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleInstallProfileVersion(w http.ResponseWriter, r *http.Request) {
	var req modloader.InstallProfileVersionRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	if err := s.container.Modloaders.InstallForInstanceAsync(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCancelProfileInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceID string `json:"instance_id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cancelled": s.container.Modloaders.CancelInstall(body.InstanceID)})
}

func (s *Server) handleGetActiveProfileInstall(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "progress": s.container.Modloaders.GetActiveInstall(strings.TrimSpace(chi.URLParam(r, "id")))})
}

func (s *Server) handleListProfileInstalledVersions(w http.ResponseWriter, r *http.Request) {
	list, err := s.container.Modloaders.ListInstalledVersions(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "versions": list})
}

func (s *Server) handleActivateProfileVersion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceID   string `json:"instance_id"`
		InstalledID  string `json:"installed_id"`
		CreateBackup bool   `json:"create_backup"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	iv, err := s.container.Modloaders.ActivateInstalledVersion(body.InstanceID, body.InstalledID, body.CreateBackup)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": iv})
}

func (s *Server) handleVerifyProfileVersion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceID  string `json:"instance_id"`
		InstalledID string `json:"installed_id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	rep, err := s.container.Modloaders.VerifyInstalledVersion(r.Context(), body.InstanceID, body.InstalledID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "report": rep})
}

func (s *Server) handleDeleteProfileVersion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceID  string `json:"instance_id"`
		InstalledID string `json:"installed_id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	if err := s.container.Modloaders.DeleteInstalledVersion(body.InstanceID, body.InstalledID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleGetAccounts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.container.Accounts.GetSnapshot())
}

func (s *Server) handleAddOfflineAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	v, err := s.container.Accounts.AddOfflineAccount(body.Username)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "account": v})
}

func (s *Server) handleStartMicrosoftLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ReauthAccountID string `json:"reauth_account_id"`
	}
	if !decodeJSONBody(w, r, &body, true) {
		return
	}
	if err := s.container.Accounts.StartMicrosoftLoginAsync(body.ReauthAccountID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCancelMicrosoftLogin(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cancelled": s.container.Accounts.CancelAuth()})
}

func (s *Server) handleSetActiveAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	v, err := s.container.Accounts.SetActive(body.ID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "account": v})
}

func (s *Server) handleRefreshAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID    string `json:"id"`
		Force bool   `json:"force"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	v, err := s.container.Accounts.RefreshAccount(r.Context(), body.ID, body.Force)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "account": v})
}

func (s *Server) handleCheckAccountLicense(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	v, err := s.container.Accounts.CheckAccountLicense(r.Context(), body.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "account": v, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "account": v})
}

func (s *Server) handleSignOutAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	v, err := s.container.Accounts.SignOut(body.ID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "account": v})
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	if err := s.container.Accounts.DeleteAccount(body.ID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleUpdateAccountSkin(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "id")
	if strings.TrimSpace(accountID) == "" {
		writeErr(w, http.StatusBadRequest, "account id is required")
		return
	}
	var body struct {
		SkinURL     string `json:"skin_url"`
		SkinVariant string `json:"skin_variant"`
		CapeURL     string `json:"cape_url"`
		CapeAlias   string `json:"cape_alias"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	v, err := s.container.Accounts.UpdateAccountSkin(accountID, body.SkinURL, body.SkinVariant, body.CapeURL, body.CapeAlias)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "account": v})
}

func (s *Server) handleLauncherSnapshot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.container.Launcher.GetSnapshot())
}

func (s *Server) handleLauncherPreflight(w http.ResponseWriter, r *http.Request) {
	var req launcher.LaunchRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	writeJSON(w, http.StatusOK, s.container.Launcher.Preflight(r.Context(), req))
}

func (s *Server) handleLauncherLaunch(w http.ResponseWriter, r *http.Request) {
	var req launcher.LaunchRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	sess, err := s.container.Launcher.LaunchAsync(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session": sess})
}

func (s *Server) handleLauncherStop(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceID string `json:"instance_id"`
		Force      bool   `json:"force"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	if err := s.container.Launcher.Stop(body.InstanceID, body.Force); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLauncherInstanceSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session": s.container.Launcher.GetInstanceSession(id)})
}

func (s *Server) handleLauncherSessionHistory(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	lim, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if lim <= 0 {
		lim = 20
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sessions": s.container.Launcher.GetSessionHistory(id, lim)})
}

func (s *Server) handleLauncherLogs(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	lim, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "lines": s.container.Launcher.GetInstanceLogs(id, lim)})
}

func (s *Server) handleLauncherCrashReports(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	list, err := s.container.Launcher.ListCrashReports(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "crash_reports": list})
}

func (s *Server) handleLauncherCrashDiagnosis(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	diag, err := s.container.Launcher.AnalyzeInstanceCrash(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "diagnosis": diag})
}

func (s *Server) handleListModpacks(w http.ResponseWriter, _ *http.Request) {
	list, err := s.container.Modpacks.ListModpacks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "modpacks": list})
}
func (s *Server) handleGetModpackProgress(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "progress": s.container.Modpacks.GetActiveProgress()})
}
func (s *Server) handlePreviewModpackImport(w http.ResponseWriter, r *http.Request) {
	var req modpacks.PreviewImportRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	prev, err := s.container.Modpacks.PreviewImport(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, prev)
}
func (s *Server) handleExecuteModpackImport(w http.ResponseWriter, r *http.Request) {
	var req modpacks.ExecuteImportRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	if err := s.container.Modpacks.ExecuteImportAsync(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
func (s *Server) handleExportModpack(w http.ResponseWriter, r *http.Request) {
	var req modpacks.ExportModpackRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	// Never let an HTTP caller choose an arbitrary filesystem destination.
	ext := ".modhub"
	switch req.Format {
	case modpacks.FormatMRPack:
		ext = ".mrpack"
	case modpacks.FormatCurseForge:
		ext = ".zip"
	}
	stem := reSafeFilename.ReplaceAllString(strings.TrimSpace(req.Name), "-")
	stem = strings.Trim(stem, "-")
	if stem == "" {
		stem = "modhub-pack"
	}
	if len(stem) > 96 {
		stem = stem[:96]
	}
	base := filepath.Join(s.container.DataDir, "exports")
	if err := os.MkdirAll(base, 0o700); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir, err := os.MkdirTemp(base, "pack-*")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.Remove(dir) // Removes only an empty directory after a failed export.
	req.OutputPath = filepath.Join(dir, stem+ext)
	res, err := s.container.Modpacks.Export(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if res.OutputPath != "" {
		if err := ensurePathWithin(filepath.Join(s.container.DataDir, "exports"), res.OutputPath); err != nil {
			writeErr(w, http.StatusInternalServerError, "Путь экспорта находится вне каталога данных")
			return
		}
		key, err := s.registerExport(base, res.OutputPath, filepath.Base(res.OutputPath))
		if err != nil {
			_ = os.Remove(res.OutputPath)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		res.DownloadURL = "/api/modpacks/export-file?token=" + url.QueryEscape(key)
		res.OutputPath = ""
	}
	writeJSON(w, http.StatusOK, res)
}

func ensurePathWithin(baseDir, target string) error {
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return err
	}
	full, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(full); resolveErr == nil {
		full = resolved
	}
	resolvedBase, resolveErr := filepath.EvalSymlinks(base)
	if resolveErr == nil {
		base = resolvedBase
	}
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes data directory")
	}
	return nil
}

func (s *Server) handleDownloadExportFile(w http.ResponseWriter, r *http.Request) {
	s.serveRegisteredExport(w, r, filepath.Join(s.container.DataDir, "exports"), ".modhub", ".mrpack", ".zip")
}
func (s *Server) handlePreviewModpackUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ModpackID      string `json:"modpack_id"`
		NewArchivePath string `json:"new_archive_path"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	diff, err := s.container.Modpacks.PreviewUpdate(r.Context(), body.ModpackID, body.NewArchivePath)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, diff)
}
func (s *Server) handleApplyModpackUpdate(w http.ResponseWriter, r *http.Request) {
	var req modpacks.ApplyUpdateRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	v, err := s.container.Modpacks.ApplyUpdate(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "modpack": v})
}
func (s *Server) handleDeleteModpack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	if err := s.container.Modpacks.DeleteModpackRecord(body.ID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

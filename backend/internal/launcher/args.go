package launcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"modhub/internal/accounts"
	"modhub/internal/instances"
	"modhub/internal/mcversion"
	"modhub/internal/security"
)

const (
	LauncherBrandName    = "ModHub"
	LauncherBrandVersion = "0.8.0"
	minRecommendedDiskMB = 256
)

var (
	reShellDangerChars = regexp.MustCompile(`[;&|<>` + "`" + `\r\n\x00]`)
)

func computeRecommendedMaxRAM(totalMB int64) int {
	if totalMB <= 0 {
		return 4096
	}
	// Keep at least 2 GB (or 25% of RAM) reserved for OS and desktop shell
	reserve := totalMB / 4
	if reserve < 2048 {
		reserve = 2048
	}
	maxSafe := int(totalMB - reserve)
	if maxSafe < 1024 {
		maxSafe = 1024
	}
	if maxSafe > 16384 {
		maxSafe = 16384
	}
	// Round down to nearest 512 MB
	maxSafe = (maxSafe / 512) * 512
	if maxSafe < 1024 {
		maxSafe = 1024
	}
	return maxSafe
}

// ParseSafeJVMArgs splits custom JVM flags into argv tokens without ever invoking a shell.
// Rejects control characters, shell chaining operators, and empty/unterminated quotes.
func ParseSafeJVMArgs(raw string) ([]string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	if reShellDangerChars.MatchString(s) {
		return nil, errors.New("недопустимые управляющие или shell-символы в JVM-аргументах")
	}
	if strings.Contains(s, "$(") {
		return nil, errors.New("подстановка команд $() в JVM-аргументах запрещена")
	}

	var args []string
	var cur strings.Builder
	inQuote := rune(0)
	for _, r := range s {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = r
		case r == ' ' || r == '\t':
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if inQuote != 0 {
		return nil, errors.New("незакрытая кавычка в дополнительных JVM-аргументах")
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}

	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return nil, fmt.Errorf("каждый дополнительный JVM-аргумент должен начинаться с '-' (получено %q)", a)
		}
		low := strings.ToLower(a)
		if low == "-cp" || low == "-classpath" || low == "--class-path" || low == "-jar" {
			return nil, fmt.Errorf("аргумент %q управляется лаунчером автоматически и не может быть переопределён вручную", a)
		}
	}
	return args, nil
}

// ResolveEffectiveMetadata loads versionID.json and recursively resolves any `inheritsFrom` parent chain
// (e.g. Fabric/Quilt/Forge/NeoForge -> Vanilla base), returning a cleanly merged VersionMetadata.
func ResolveEffectiveMetadata(mcMgr *mcversion.Manager, versionID string) (*mcversion.VersionMetadata, string, error) {
	if err := mcversion.ValidateVersionID(versionID); err != nil {
		return nil, "", err
	}
	chain := make([]*mcversion.VersionMetadata, 0, 3)
	seen := map[string]bool{}
	curID := versionID
	for curID != "" {
		if seen[curID] {
			return nil, "", fmt.Errorf("обнаружена циклическая ссылка inheritsFrom на версии %s", curID)
		}
		seen[curID] = true
		meta, err := mcversion.ReadVersionMetadataFile(mcMgr.VersionJSONPath(curID))
		if err != nil {
			return nil, "", fmt.Errorf("не удалось прочитать метаданные версии %s: %w", curID, err)
		}
		chain = append(chain, meta)
		curID = strings.TrimSpace(meta.InheritsFrom)
	}
	if len(chain) == 0 {
		return nil, "", fmt.Errorf("метаданные версии %s не найдены", versionID)
	}

	// Root (base Vanilla) is the last element; merge from base -> child so child overrides parent cleanly.
	baseMeta := chain[len(chain)-1]
	baseVersionID := baseMeta.ID
	merged := *baseMeta
	merged.Libraries = append([]mcversion.Library(nil), baseMeta.Libraries...)
	if baseMeta.Arguments != nil {
		merged.Arguments = &mcversion.VersionArguments{
			Game: append([]mcversion.ArgumentEntry(nil), baseMeta.Arguments.Game...),
			JVM:  append([]mcversion.ArgumentEntry(nil), baseMeta.Arguments.JVM...),
		}
	}
	if len(baseMeta.Downloads) > 0 {
		merged.Downloads = make(map[string]mcversion.DownloadArtifact, len(baseMeta.Downloads))
		for k, v := range baseMeta.Downloads {
			merged.Downloads[k] = v
		}
	}

	for i := len(chain) - 2; i >= 0; i-- {
		child := chain[i]
		if child.ID != "" {
			merged.ID = child.ID
		}
		if child.Type != "" {
			merged.Type = child.Type
		}
		if child.MainClass != "" {
			merged.MainClass = child.MainClass
		}
		if child.Assets != "" {
			merged.Assets = child.Assets
		}
		if child.AssetIndex != nil {
			merged.AssetIndex = child.AssetIndex
		}
		if child.JavaVersion != nil && child.JavaVersion.MajorVersion > 0 {
			merged.JavaVersion = child.JavaVersion
		}
		if child.Logging != nil && child.Logging.Client != nil {
			merged.Logging = child.Logging
		}
		if len(child.Downloads) > 0 {
			if merged.Downloads == nil {
				merged.Downloads = make(map[string]mcversion.DownloadArtifact)
			}
			for k, v := range child.Downloads {
				merged.Downloads[k] = v
			}
		}
		// Prepend child libraries so child versions win during Maven group:artifact deduplication!
		if len(child.Libraries) > 0 {
			combined := make([]mcversion.Library, 0, len(child.Libraries)+len(merged.Libraries))
			combined = append(combined, child.Libraries...)
			combined = append(combined, merged.Libraries...)
			merged.Libraries = combined
		}
		// Merge arguments:
		// - If child defines legacy minecraftArguments, it replaces parent minecraftArguments (standard for legacy Forge/Fabric).
		// - If child defines modern arguments.game / arguments.jvm, append child entries to parent entries.
		if strings.TrimSpace(child.MinecraftArguments) != "" {
			merged.MinecraftArguments = child.MinecraftArguments
		}
		if child.Arguments != nil {
			if merged.Arguments == nil {
				merged.Arguments = &mcversion.VersionArguments{}
			}
			if len(child.Arguments.Game) > 0 {
				merged.Arguments.Game = append(merged.Arguments.Game, child.Arguments.Game...)
			}
			if len(child.Arguments.JVM) > 0 {
				merged.Arguments.JVM = append(merged.Arguments.JVM, child.Arguments.JVM...)
			}
		}
	}
	return &merged, baseVersionID, nil
}

// mavenArtifactKey extracts "group:artifact[:classifier]" for classpath deduplication (first/child entry wins).
func mavenArtifactKey(coord string) string {
	c := strings.TrimSpace(coord)
	if idx := strings.LastIndex(c, "@"); idx > 0 {
		c = c[:idx]
	}
	parts := strings.Split(c, ":")
	if len(parts) < 3 {
		return strings.ToLower(c)
	}
	key := strings.ToLower(parts[0] + ":" + parts[1])
	if len(parts) >= 4 && strings.TrimSpace(parts[3]) != "" {
		key += ":" + strings.ToLower(parts[3])
	}
	return key
}

// BuildLaunchSpec constructs the complete, official-schema-compatible Java launch command from real version metadata.
func BuildLaunchSpec(
	sharedDir string,
	inst instances.Instance,
	mergedMeta *mcversion.VersionMetadata,
	baseMCVersion string,
	nativesDir string,
	javaExe string,
	creds accounts.LaunchCredentials,
	plat mcversion.PlatformInfo,
	sysMem SystemMemoryInfo,
) (LaunchSpec, error) {
	if mergedMeta == nil || strings.TrimSpace(mergedMeta.MainClass) == "" {
		return LaunchSpec{}, errors.New("в метаданных версии отсутствует mainClass")
	}
	if strings.TrimSpace(javaExe) == "" {
		return LaunchSpec{}, errors.New("не указан исполняемый файл Java")
	}

	librariesDir := filepath.Join(sharedDir, "libraries")
	assetsRoot := filepath.Join(sharedDir, "assets")
	logConfigsDir := filepath.Join(sharedDir, "assets", "log_configs")

	// 1. Validate & clamp memory settings reasonably against host RAM
	minMB := inst.MemoryMinMB
	maxMB := inst.MemoryMaxMB
	if maxMB <= 0 {
		maxMB = 4096
	}
	if minMB <= 0 {
		minMB = 512
	}
	var warnings []string
	if sysMem.TotalRAMMB > 0 {
		hardCap := int(sysMem.TotalRAMMB - 512)
		if hardCap < 512 {
			hardCap = 512
		}
		if maxMB > hardCap {
			return LaunchSpec{}, fmt.Errorf("выделенная память (%d МБ) превышает доступный объём ОЗУ системы (%d МБ)", maxMB, sysMem.TotalRAMMB)
		}
		if sysMem.RecommendedMaxMB > 0 && maxMB > sysMem.RecommendedMaxMB {
			warnings = append(warnings, fmt.Sprintf("Выделено %d МБ ОЗУ (рекомендуемый максимум для этой системы: %d МБ)", maxMB, sysMem.RecommendedMaxMB))
		}
	}
	if minMB > maxMB {
		minMB = maxMB
	}

	// 2. Parse custom user JVM arguments safely (shell-free)
	customJVMArgs, err := ParseSafeJVMArgs(inst.LaunchSettings.JVMArgs)
	if err != nil {
		return LaunchSpec{}, err
	}

	// 3. Build deduplicated classpath from filtered platform libraries
	filteredLibs := mcversion.FilterLibrariesForPlatform(mergedMeta, plat)
	var cpEntries []string
	seenArtifact := map[string]bool{}
	seenPath := map[string]bool{}
	missingLibs := make([]string, 0)

	for _, lib := range filteredLibs {
		// Skip classic extracted-only native classifier JARs (their .dll/.so are in nativesDir),
		// while keeping normal libraries & modern LWJGL modular jars.
		if lib.IsNative && lib.Extract != nil {
			continue
		}
		artKey := mavenArtifactKey(lib.Name)
		if seenArtifact[artKey] {
			continue
		}
		fullPath := filepath.Join(librariesDir, filepath.FromSlash(lib.RelPath))
		if fi, serr := os.Stat(fullPath); serr != nil || fi.IsDir() || fi.Size() == 0 {
			missingLibs = append(missingLibs, lib.RelPath)
			continue
		}
		lowP := strings.ToLower(filepath.Clean(fullPath))
		if seenPath[lowP] {
			continue
		}
		seenArtifact[artKey] = true
		seenPath[lowP] = true
		cpEntries = append(cpEntries, fullPath)
	}
	if len(missingLibs) > 0 {
		preview := strings.Join(missingLibs[:min(len(missingLibs), 3)], ", ")
		return LaunchSpec{}, fmt.Errorf("отсутствуют обязательные библиотеки (%d шт.: %s)", len(missingLibs), preview)
	}

	// Append Minecraft client JAR to classpath unless modern Forge/NeoForge BootstrapLauncher explicitly loads patched client via module/game layer
	clientJar := filepath.Join(sharedDir, "versions", baseMCVersion, baseMCVersion+".jar")
	if fi, serr := os.Stat(clientJar); serr != nil || fi.IsDir() || fi.Size() == 0 {
		return LaunchSpec{}, fmt.Errorf("отсутствует основной файл клиента %s.jar", baseMCVersion)
	}
	isBootstrapLauncher := strings.EqualFold(strings.TrimSpace(mergedMeta.MainClass), "cpw.mods.bootstraplauncher.BootstrapLauncher")
	if !isBootstrapLauncher {
		cpEntries = append(cpEntries, clientJar)
	}

	classpathStr := strings.Join(cpEntries, string(os.PathListSeparator))

	// 4. Resolve Asset Index ID & Logging Config path
	assetIndexName := mergedMeta.Assets
	if mergedMeta.AssetIndex != nil && strings.TrimSpace(mergedMeta.AssetIndex.ID) != "" {
		assetIndexName = strings.TrimSpace(mergedMeta.AssetIndex.ID)
	}
	if assetIndexName == "" {
		assetIndexName = baseMCVersion
	}
	verType := mergedMeta.Type
	if verType == "" {
		verType = "release"
	}

	hasCustomRes := inst.LaunchSettings.WindowWidth > 0 && inst.LaunchSettings.WindowHeight > 0
	features := map[string]bool{
		"is_demo_user":              false,
		"has_custom_resolution":     hasCustomRes,
		"has_quick_plays_support":   false,
		"is_quick_play_singleplayer": false,
		"is_quick_play_multiplayer": false,
		"is_quick_play_realms":      false,
	}

	// 5. Build standard Minecraft variable substitution table
	clientIDVal := ""
	userType := "msa"
	if creds.UserType != "" {
		userType = creds.UserType
	} else if creds.AccountType == accounts.AccountTypeOffline {
		userType = "mojang"
	}
	vars := map[string]string{
		"auth_player_name":   creds.MinecraftUsername,
		"version_name":       mergedMeta.ID,
		"game_directory":     inst.GameDir,
		"assets_root":        assetsRoot,
		"game_assets":        assetsRoot,
		"assets_index_name":  assetIndexName,
		"auth_uuid":          creds.MinecraftUUID,
		"auth_access_token":  creds.MinecraftAccessToken,
		"auth_session":       "token:" + creds.MinecraftAccessToken + ":" + creds.MinecraftUUID,
		"clientid":           clientIDVal,
		"auth_xuid":          creds.XboxUserID,
		"user_type":          userType,
		"user_properties":    "{}",
		"version_type":       verType,
		"natives_directory":  nativesDir,
		"launcher_name":      LauncherBrandName,
		"launcher_version":   LauncherBrandVersion,
		"classpath":          classpathStr,
		"library_directory":  librariesDir,
		"classpath_separator": string(os.PathListSeparator),
		"resolution_width":   strconv.Itoa(inst.LaunchSettings.WindowWidth),
		"resolution_height":  strconv.Itoa(inst.LaunchSettings.WindowHeight),
	}

	substitute := func(s string, redact bool) string {
		out := s
		for k, v := range vars {
			repl := v
			if redact && (k == "auth_access_token" || k == "auth_session") {
				repl = "[REDACTED]"
			}
			out = strings.ReplaceAll(out, "${"+k+"}", repl)
		}
		return out
	}

	// 6. Assemble JVM arguments
	var rawArgs []string
	var safeArgs []string
	addArg := func(rawVal, safeVal string) {
		rawArgs = append(rawArgs, rawVal)
		safeArgs = append(safeArgs, safeVal)
	}

	// Memory flags first
	xms := fmt.Sprintf("-Xms%dM", minMB)
	xmx := fmt.Sprintf("-Xmx%dM", maxMB)
	addArg(xms, xms)
	addArg(xmx, xmx)

	// User-configured custom JVM args
	for _, ca := range customJVMArgs {
		addArg(ca, security.SanitizeLogText(ca))
	}

	// Logging configuration JVM arg (when defined in metadata and file exists)
	if mergedMeta.Logging != nil && mergedMeta.Logging.Client != nil && strings.TrimSpace(mergedMeta.Logging.Client.Argument) != "" {
		lf := mergedMeta.Logging.Client.File
		lfName := lf.ID
		if lfName == "" && lf.URL != "" {
			lfName = filepath.Base(lf.URL)
		}
		if lfName != "" {
			lfPath := filepath.Join(logConfigsDir, filepath.Base(lfName))
			if fi, serr := os.Stat(lfPath); serr == nil && !fi.IsDir() && fi.Size() > 0 {
				logArg := strings.ReplaceAll(mergedMeta.Logging.Client.Argument, "${path}", lfPath)
				addArg(logArg, logArg)
			}
		}
	}

	// Metadata JVM arguments (modern) or default legacy JVM args (-Djava.library.path + -cp)
	hasMetadataJVM := mergedMeta.Arguments != nil && len(mergedMeta.Arguments.JVM) > 0
	hasCPFlag := false
	hasLibPathFlag := false
	if hasMetadataJVM {
		for _, entry := range mergedMeta.Arguments.JVM {
			if !mcversion.EvaluateRules(entry.Rules, plat, features) {
				continue
			}
			for _, v := range entry.Values {
				rv := substitute(v, false)
				sv := substitute(v, true)
				if rv == "-cp" || rv == "-classpath" {
					hasCPFlag = true
				}
				if strings.HasPrefix(rv, "-Djava.library.path=") {
					hasLibPathFlag = true
				}
				addArg(rv, sv)
			}
		}
	}
	if !hasLibPathFlag {
		lp := "-Djava.library.path=" + nativesDir
		addArg(lp, lp)
	}
	if !hasCPFlag {
		addArg("-cp", "-cp")
		addArg(classpathStr, classpathStr)
	}

	// 7. Main class
	addArg(mergedMeta.MainClass, mergedMeta.MainClass)

	// 8. Game arguments (modern arguments.game OR legacy minecraftArguments)
	hasWidthArg := false
	hasHeightArg := false
	hasFullscreenArg := false
	appendGameTok := func(tok string) {
		rv := substitute(tok, false)
		sv := substitute(tok, true)
		if rv == "--width" {
			hasWidthArg = true
		} else if rv == "--height" {
			hasHeightArg = true
		} else if rv == "--fullscreen" {
			hasFullscreenArg = true
		}
		addArg(rv, sv)
	}

	if mergedMeta.Arguments != nil && len(mergedMeta.Arguments.Game) > 0 {
		for _, entry := range mergedMeta.Arguments.Game {
			if !mcversion.EvaluateRules(entry.Rules, plat, features) {
				continue
			}
			for _, v := range entry.Values {
				appendGameTok(v)
			}
		}
	} else if strings.TrimSpace(mergedMeta.MinecraftArguments) != "" {
		for _, tok := range strings.Fields(mergedMeta.MinecraftArguments) {
			appendGameTok(tok)
		}
	} else {
		return LaunchSpec{}, errors.New("в метаданных версии отсутствуют аргументы запуска игры (arguments.game / minecraftArguments)")
	}

	// Append resolution / fullscreen flags if not already supplied by metadata feature rules
	if hasCustomRes {
		if !hasWidthArg {
			addArg("--width", "--width")
			w := strconv.Itoa(inst.LaunchSettings.WindowWidth)
			addArg(w, w)
		}
		if !hasHeightArg {
			addArg("--height", "--height")
			h := strconv.Itoa(inst.LaunchSettings.WindowHeight)
			addArg(h, h)
		}
	}
	if inst.LaunchSettings.Fullscreen && !hasFullscreenArg {
		addArg("--fullscreen", "--fullscreen")
	}

	return LaunchSpec{
		JavaExe:        javaExe,
		WorkDir:        inst.GameDir,
		MainClass:      mergedMeta.MainClass,
		VersionID:      mergedMeta.ID,
		MCVersion:      baseMCVersion,
		Loader:         string(inst.InstanceType),
		NativesDir:     nativesDir,
		ClasspathCount: len(cpEntries),
		MemoryMinMB:    minMB,
		MemoryMaxMB:    maxMB,
		SanitizedArgs:  safeArgs,
		RawArgs:        rawArgs,
		Warnings:       warnings,
	}, nil
}

package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reFabricRequires  = regexp.MustCompile(`(?i)Mod '([^']+)'[^\n]*requires (?:any version of )?([^\s,;!\n]+)`)
	reForgeMissingDep = regexp.MustCompile(`(?i)Mod ID:\s*'([^']+)',\s*Requested by:\s*'([^']+)'`)
	reClassVersion    = regexp.MustCompile(`(?i)compiled by a more recent version of the Java Runtime \(class file version (\d+)\.0\)`)
	reMixinMod        = regexp.MustCompile(`(?i)(?:from mod|mod id|in mod)\s+['"\[]?([a-z0-9_.-]{2,40})['"\]]?`)
	reDuplicateMod    = regexp.MustCompile(`(?i)Duplicate mods? found[^:\n]*:\s*([^\n]+)`)
	reJavaException   = regexp.MustCompile(`(?m)^(?:Caused by:\s*)?([a-zA-Z0-9_.$]*(?:Exception|Error)):\s*(.+)$`)
)

func requiredJavaFromClassVersion(classMajor string) string {
	switch classMajor {
	case "68":
		return "Java 24"
	case "67":
		return "Java 23"
	case "66":
		return "Java 22"
	case "65":
		return "Java 21"
	case "64":
		return "Java 20"
	case "63":
		return "Java 19"
	case "62":
		return "Java 18"
	case "61":
		return "Java 17"
	case "60":
		return "Java 16"
	case "55":
		return "Java 11"
	case "52":
		return "Java 8"
	default:
		return "более новой версии Java"
	}
}

func readFileTail(path string, maxBytes int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	sz := fi.Size()
	if sz > maxBytes {
		_, _ = f.Seek(sz-maxBytes, 0)
	}
	buf := make([]byte, minInt64(sz, maxBytes))
	n, _ := f.Read(buf)
	return string(buf[:n])
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// AnalyzeCrash inspects logs, crash reports, and exit code to produce a human-readable root-cause diagnosis.
func AnalyzeCrash(exitCode int, workDir string, logLines []LogLine, crashReports []CrashReportInfo) *CrashDiagnosis {
	var corpus strings.Builder
	for _, ln := range logLines {
		corpus.WriteString(ln.Text)
		corpus.WriteByte('\n')
	}
	if len(crashReports) > 0 && strings.TrimSpace(crashReports[0].FullPath) != "" {
		corpus.WriteString("\n--- CRASH REPORT ---\n")
		corpus.WriteString(readFileTail(crashReports[0].FullPath, 96*1024))
	}
	if workDir != "" {
		latestLog := filepath.Join(workDir, "logs", "latest.log")
		if tail := readFileTail(latestLog, 64*1024); tail != "" {
			corpus.WriteString("\n--- LATEST LOG ---\n")
			corpus.WriteString(tail)
		}
	}

	raw := corpus.String()
	low := strings.ToLower(raw)

	// 1. Java version incompatibility
	if m := reClassVersion.FindStringSubmatch(raw); len(m) == 2 {
		reqJava := requiredJavaFromClassVersion(m[1])
		return &CrashDiagnosis{
			Code:       "java_version",
			Title:      "Неподходящая версия Java",
			Cause:      fmt.Sprintf("Игра или один из модов скомпилированы для %s, а текущая Java слишком старая.", reqJava),
			Suggestion: fmt.Sprintf("Откройте «Настройки запуска» и выберите или установите %s (либо включите автоопределение Java).", reqJava),
			ActionHint: "settings",
		}
	}
	if strings.Contains(low, "unsupportedclassversionerror") || strings.Contains(low, "unrecognized vm option") {
		return &CrashDiagnosis{
			Code:       "java_version",
			Title:      "Ошибка совместимости Java",
			Cause:      "Выбранная версия Java или её JVM-аргументы не подходят для этой версии Minecraft.",
			Suggestion: "Проверьте выбранную версию Java и дополнительные аргументы JVM в «Настройках запуска».",
			ActionHint: "settings",
		}
	}

	// 2. Out of memory
	if strings.Contains(low, "outofmemoryerror") || strings.Contains(low, "java heap space") || exitCode == 137 || exitCode == -805306369 {
		return &CrashDiagnosis{
			Code:       "out_of_memory",
			Title:      "Недостаточно оперативной памяти (RAM)",
			Cause:      "Игре не хватило выделенной памяти для загрузки модов, текстур или мира.",
			Suggestion: "Увеличьте лимит оперативной памяти в «Настройках запуска» (например, 4096–6144 МБ для сборок с модами).",
			ActionHint: "settings",
		}
	}

	// 3. Missing / incompatible mod dependencies (Fabric / Quilt / Forge / NeoForge)
	if m := reForgeMissingDep.FindStringSubmatch(raw); len(m) == 3 {
		return &CrashDiagnosis{
			Code:       "missing_dependency",
			Title:      "Не хватает обязательного мода-зависимости",
			Cause:      fmt.Sprintf("Мод «%s» требует установить библиотеку или мод «%s».", m[2], m[1]),
			Suggestion: fmt.Sprintf("Откройте каталог и доустановите «%s» в этот профиль, либо отключите мод «%s».", m[1], m[2]),
			RelatedMod: m[1],
			ActionHint: "mods",
		}
	}
	if m := reFabricRequires.FindStringSubmatch(raw); len(m) == 3 {
		return &CrashDiagnosis{
			Code:       "missing_dependency",
			Title:      "Отсутствует или не подходит зависимость мода",
			Cause:      fmt.Sprintf("Мод «%s» требует компонент или мод «%s» подходящей версии.", m[1], m[2]),
			Suggestion: fmt.Sprintf("Установите или обновите «%s» (например, Fabric API / библиотеку мода) или проверьте совместимость версий.", m[2]),
			RelatedMod: m[2],
			ActionHint: "mods",
		}
	}
	if strings.Contains(low, "incompatible mod set") || strings.Contains(low, "missing or unsupported mandatory dependencies") || strings.Contains(low, "mod resolution failed") {
		return &CrashDiagnosis{
			Code:       "mod_conflict",
			Title:      "Конфликт или несовместимый набор модов",
			Cause:      "Загрузчик модов обнаружил отсутствующую библиотеку либо конфликт версий между установленными модами.",
			Suggestion: "Проверьте вкладку «Моды»: убедитесь, что все моды подходят под версию Minecraft и загрузчик, и установлены нужные зависимости.",
			ActionHint: "mods",
		}
	}

	// 4. Duplicate mods
	if m := reDuplicateMod.FindStringSubmatch(raw); len(m) == 2 {
		modName := strings.TrimSpace(m[1])
		return &CrashDiagnosis{
			Code:       "duplicate_mod",
			Title:      "Обнаружен дубликат мода",
			Cause:      fmt.Sprintf("В папке mods находятся две копии одного мода (%s).", modName),
			Suggestion: "Откройте вкладку «Моды» и удалите старую или дублирующую версию мода.",
			RelatedMod: modName,
			ActionHint: "mods",
		}
	}

	// 5. Mixin error (mod crash / conflict)
	if strings.Contains(low, "mixinapplyerror") || strings.Contains(low, "mixintransformererror") || strings.Contains(low, "critical injection failure") {
		culprit := ""
		if m := reMixinMod.FindStringSubmatch(raw); len(m) == 2 {
			culprit = m[1]
		}
		cause := "Один из модов не смог применить патчи (Mixin) — обычно это происходит из-за конфликта модов или неподходящей версии игры."
		if culprit != "" {
			cause = fmt.Sprintf("Сбой применения Mixin связан с модом «%s» (конфликт с другим модом или версией загрузчика).", culprit)
		}
		return &CrashDiagnosis{
			Code:       "mixin_conflict",
			Title:      "Конфликт модов при запуске (Mixin)",
			Cause:      cause,
			Suggestion: "Попробуйте временно отключить недавно добавленные моды или проверить их обновления во вкладке «Моды».",
			RelatedMod: culprit,
			ActionHint: "mods",
		}
	}

	// 6. OpenGL / GLFW / GPU crash
	if strings.Contains(low, "glfw error") || strings.Contains(low, "pixel format not accelerated") || strings.Contains(low, "opengl") && strings.Contains(low, "error") || strings.Contains(low, "nvoglv64.dll") || strings.Contains(low, "atio6axx.dll") || strings.Contains(low, "igxelpicd64.dll") {
		return &CrashDiagnosis{
			Code:       "graphics_driver",
			Title:      "Ошибка графики или драйвера видеокарты",
			Cause:      "Произошёл сбой инициализации OpenGL/GLFW или драйвера видеокарты.",
			Suggestion: "Обновите драйвер видеокарты, отключите шейдеры или проверьте конфликты модов рендеринга (Sodium / Iris / OptiFine).",
			ActionHint: "mods",
		}
	}

	// 7. Corrupted config / world / TOML
	if strings.Contains(low, "parsingexception") || strings.Contains(low, "configloadingexception") || strings.Contains(low, "com.electronwill.nightconfig") {
		return &CrashDiagnosis{
			Code:       "corrupt_config",
			Title:      "Повреждённый файл настроек мода",
			Cause:      "Один из модов не смог прочитать свой конфигурационный файл (.toml / .json).",
			Suggestion: "Откройте папку профиля → config и удалите повреждённый файл настроек, чтобы мод создал его заново.",
		}
	}

	// 8. Access violation / native crash
	if exitCode == -1073741819 || exitCode == -1073740791 || strings.Contains(low, "exception_access_violation") {
		return &CrashDiagnosis{
			Code:       "native_crash",
			Title:      "Критический системный сбой Java / нативной библиотеки",
			Cause:      fmt.Sprintf("Процесс завершился нативной ошибкой (код %d) в драйвере или библиотеке.", exitCode),
			Suggestion: "Проверьте драйверы видеокарты, версию Java и отключите сторонние оверлеи (Discord, MSI Afterburner, GeForce Experience).",
			ActionHint: "settings",
		}
	}

	// 9. Generic fallback with extracted exception
	if matches := reJavaException.FindAllStringSubmatch(raw, -1); len(matches) > 0 {
		last := matches[len(matches)-1]
		excName := last[1]
		if idx := strings.LastIndex(excName, "."); idx >= 0 {
			excName = excName[idx+1:]
		}
		excMsg := strings.TrimSpace(last[2])
		if len(excMsg) > 180 {
			excMsg = excMsg[:180] + "…"
		}
		return &CrashDiagnosis{
			Code:       "java_exception",
			Title:      fmt.Sprintf("Сбой игры (%s)", excName),
			Cause:      excMsg,
			Suggestion: "Проверьте недавно установленные моды и убедитесь, что версии модов, загрузчика и Java совпадают.",
			ActionHint: "mods",
		}
	}

	if exitCode != 0 {
		return &CrashDiagnosis{
			Code:       "unknown_crash",
			Title:      fmt.Sprintf("Игра завершилась с ошибкой (код %d)", exitCode),
			Cause:      "Точная причина не указана в короткой сводке лога.",
			Suggestion: "Проверьте фильтр «Ошибки» в консоли выше или откройте последний crash report.",
		}
	}

	return nil
}

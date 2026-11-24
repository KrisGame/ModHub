package modloader

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"modhub/internal/downloads"
	"modhub/internal/mcversion"
)

type mavenMetadataXML struct {
	Versioning struct {
		Latest   string `xml:"latest"`
		Release  string `xml:"release"`
		Versions struct {
			Version []string `xml:"version"`
		} `xml:"versions"`
	} `xml:"versioning"`
}

func parseMavenMetadataVersions(xmlBody string) ([]string, string, string, error) {
	var meta mavenMetadataXML
	if err := xml.Unmarshal([]byte(xmlBody), &meta); err != nil {
		return nil, "", "", fmt.Errorf("некорректный XML maven-metadata: %w", err)
	}
	out := make([]string, 0, len(meta.Versioning.Versions.Version))
	for _, v := range meta.Versioning.Versions.Version {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out, strings.TrimSpace(meta.Versioning.Release), strings.TrimSpace(meta.Versioning.Latest), nil
}

func downloadLibrariesBatch(
	ctx context.Context,
	dlMgr *downloads.Manager,
	librariesDir string,
	libs []mcversion.ResolvedLibraryFile,
	groupID string,
	category string,
	progressStage string,
	instanceID string,
	mcVer string,
	loader string,
	loaderVer string,
	onProgress mcversion.ProgressCallback,
) error {
	var missing []mcversion.ResolvedLibraryFile
	seen := map[string]struct{}{}
	for _, lib := range libs {
		if strings.TrimSpace(lib.RelPath) == "" || strings.TrimSpace(lib.URL) == "" {
			continue
		}
		key := strings.ToLower(lib.RelPath)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		dest := filepath.Join(librariesDir, filepath.FromSlash(lib.RelPath))
		if mcversion.FileMatchesSHA1(dest, lib.SHA1, lib.Size) {
			continue
		}
		missing = append(missing, lib)
	}

	total := len(missing)
	if total == 0 {
		return nil
	}
	done := 0
	const batchSize = 24
	for start := 0; start < total; start += batchSize {
		if ctx.Err() != nil {
			dlMgr.CancelGroup(groupID)
			return ctx.Err()
		}
		end := start + batchSize
		if end > total {
			end = total
		}
		batch := missing[start:end]
		type qItem struct {
			name   string
			taskID string
		}
		queued := make([]qItem, 0, len(batch))
		for _, lib := range batch {
			dest := filepath.Join(librariesDir, filepath.FromSlash(lib.RelPath))
			algo := downloads.ChecksumNone
			if strings.TrimSpace(lib.SHA1) != "" {
				algo = downloads.ChecksumSHA1
			}
			tv, err := dlMgr.Enqueue(downloads.DownloadRequest{
				Name: filepath.Base(lib.RelPath), Category: category,
				URL: lib.URL, DestPath: dest, ExpectedSize: lib.Size,
				ChecksumAlgo: algo, ChecksumValue: lib.SHA1, GroupID: groupID,
			})
			if err != nil {
				dlMgr.CancelGroup(groupID)
				return fmt.Errorf("ошибка постановки библиотеки %s в очередь: %w", lib.Name, err)
			}
			queued = append(queued, qItem{name: filepath.Base(lib.RelPath), taskID: tv.ID})
		}
		for _, q := range queued {
			fin, err := dlMgr.Wait(ctx, q.taskID)
			if err != nil {
				dlMgr.CancelGroup(groupID)
				return err
			}
			if fin.Status != downloads.StatusCompleted {
				dlMgr.CancelGroup(groupID)
				return fmt.Errorf("не удалось загрузить библиотеку %s (%s): %s", q.name, fin.Status, fin.Error)
			}
			done++
			if onProgress != nil {
				onProgress(mcversion.InstallProgress{
					InstanceID: instanceID, MCVersion: mcVer, Loader: loader, LoaderVersion: loaderVer,
					Stage: progressStage, Message: fmt.Sprintf("Загрузка библиотек %s: %d из %d", loader, done, total),
					Component: q.name, CompletedFiles: done, TotalFiles: total, Progress: float64(done) * 100 / float64(total),
				})
			}
		}
	}
	return nil
}

func readZipEntryBytes(zr *zip.ReadCloser, entryName string, maxBytes int64) ([]byte, error) {
	want := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(entryName), "\\", "/"), "/")
	for _, f := range zr.File {
		if strings.TrimPrefix(strings.ReplaceAll(f.Name, "\\", "/"), "/") == want {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, maxBytes))
		}
	}
	return nil, fmt.Errorf("запись %q не найдена в архиве", entryName)
}

func extractZipEntrySafe(zr *zip.ReadCloser, entryName, destPath string, maxBytes int64) error {
	raw, err := readZipEntryBytes(zr, entryName, maxBytes)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destPath, raw, 0o644)
}

func sanitizeRelativeArchivePath(rel string) (string, error) {
	s := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/"), "/")
	if s == "" || strings.Contains(s, ":") {
		return "", fmt.Errorf("небезопасный путь %q", rel)
	}
	clean := path.Clean(s)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("обнаружен Path Traversal в пути %q", rel)
	}
	return clean, nil
}

func readJarMainClass(jarPath string) (string, error) {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return "", fmt.Errorf("не удалось открыть JAR %s: %w", filepath.Base(jarPath), err)
	}
	defer zr.Close()
	raw, err := readZipEntryBytes(zr, "META-INF/MANIFEST.MF", 256*1024)
	if err != nil {
		return "", fmt.Errorf("в JAR %s отсутствует META-INF/MANIFEST.MF", filepath.Base(jarPath))
	}
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	var mainClass string
	reading := false
	for sc.Scan() {
		line := sc.Text()
		if reading {
			if strings.HasPrefix(line, " ") {
				mainClass += strings.TrimSpace(line)
				continue
			}
			break
		}
		if strings.HasPrefix(line, "Main-Class:") {
			mainClass = strings.TrimSpace(strings.TrimPrefix(line, "Main-Class:"))
			reading = true
		}
	}
	if mainClass == "" {
		return "", fmt.Errorf("в манифесте %s не указан Main-Class", filepath.Base(jarPath))
	}
	return mainClass, nil
}

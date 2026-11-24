package packio

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"

	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/security"
)

var allowedPackFolders = map[string]struct{}{
	"mods":          {},
	"resourcepacks": {},
	"shaderpacks":   {},
	"datapacks":     {},
}

var windowsReservedNames = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {}, "com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {}, "lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

// SanitizeZIPPath strictly validates folder & filename to prevent Zip Slip traversal.
// Unlike silent sanitization, any path containing traversal, separators, invalid folder,
// control characters, or reserved names is rejected with an explicit error.
func SanitizeZIPPath(folder, filename string) (string, error) {
	f := strings.ToLower(strings.TrimSpace(folder))
	if f == "" {
		f = "mods"
	}
	if _, ok := allowedPackFolders[f]; !ok {
		return "", fmt.Errorf("недопустимая папка назначения %q", f)
	}

	name := strings.TrimSpace(filename)
	if name == "" {
		return "", errors.New("пустое имя файла")
	}
	if len(name) > 200 {
		return "", errors.New("слишком длинное имя файла")
	}
	if name == "." || name == ".." || strings.EqualFold(name, "manifest.txt") {
		return "", fmt.Errorf("запрещённое имя файла %q", name)
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("недопустимый путь в имени файла %q", name)
	}
	if strings.ContainsAny(name, `:<>"|?*`) {
		return "", fmt.Errorf("недопустимые символы в имени файла %q", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("управляющие символы в имени файла %q запрещены", name)
		}
	}
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return "", fmt.Errorf("недопустимое имя файла %q", name)
	}
	baseStem := strings.ToLower(strings.TrimSuffix(name, path.Ext(name)))
	if dotIdx := strings.IndexByte(baseStem, '.'); dotIdx >= 0 {
		baseStem = baseStem[:dotIdx]
	}
	if _, reserved := windowsReservedNames[baseStem]; reserved {
		return "", fmt.Errorf("зарезервированное имя файла %q запрещено", name)
	}

	joined := f + "/" + name
	cleaned := path.Clean(joined)
	if cleaned != joined || strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, "..") || !strings.HasPrefix(cleaned, f+"/") {
		return "", fmt.Errorf("небезопасный путь внутри архива %q", joined)
	}
	return cleaned, nil
}

// ValidatePackItems validates every item's ZIP path and download URL before HTTP headers
// are committed, so invalid/malicious requests fail fast with HTTP 400.
func ValidatePackItems(items []domain.CartItem, urlValidator func(string) error) error {
	if len(items) == 0 {
		return errors.New("сборка пуста")
	}
	if len(items) > 500 {
		return errors.New("слишком много файлов в одной сборке (максимум 500)")
	}
	for i, it := range items {
		if _, err := SanitizeZIPPath(it.Folder, it.Filename); err != nil {
			return fmt.Errorf("элемент #%d (%s): %w", i+1, it.Title, err)
		}
		if strings.TrimSpace(it.DownloadURL) != "" {
			if urlValidator != nil {
				if err := urlValidator(it.DownloadURL); err != nil {
					return fmt.Errorf("элемент #%d (%s): недопустимый URL загрузки: %w", i+1, it.Title, err)
				}
			} else if _, err := netx.ValidateDownloadURL(it.DownloadURL); err != nil {
				return fmt.Errorf("элемент #%d (%s): недопустимый URL загрузки: %w", i+1, it.Title, err)
			}
		}
	}
	return nil
}

func makeUniqueZIPPath(basePath string, used map[string]struct{}) string {
	if _, exists := used[basePath]; !exists {
		return basePath
	}
	dir := path.Dir(basePath)
	base := path.Base(basePath)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; i < 1000; i++ {
		cand := fmt.Sprintf("%s/%s (%d)%s", dir, stem, i, ext)
		if _, exists := used[cand]; !exists {
			return cand
		}
	}
	return ""
}

// StreamPackZIP validates paths & URLs, downloads up to 6 files concurrently straight to
// temporary files (preventing RAM spikes), streams each entry into `w`, removes every
// temp file immediately, and honors context cancellation cleanly.
type PackBuildSummary struct {
	TotalRequested  int
	WrittenCount    int
	DownloadedFiles []string
	FailedFiles     []string
}

func StreamPackZIP(ctx context.Context, w io.Writer, httpClient *netx.HTTPClient, items []domain.CartItem) error {
	_, err := StreamPackZIPWithReport(ctx, w, httpClient, items)
	return err
}

func StreamPackZIPWithReport(ctx context.Context, w io.Writer, httpClient *netx.HTTPClient, items []domain.CartItem) (PackBuildSummary, error) {
	summary := PackBuildSummary{
		TotalRequested:  len(items),
		DownloadedFiles: []string{},
		FailedFiles:     []string{},
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if httpClient == nil {
		return summary, errors.New("HTTP-клиент не инициализирован")
	}
	if err := ValidatePackItems(items, httpClient.ValidateDLURL); err != nil {
		return summary, err
	}
	log.Printf("[ModHub][DownloadPack] Старт загрузки сборки: файлов=%d", len(items))

	type dlJob struct {
		idx      int
		item     domain.CartItem
		safePath string
	}
	type dlResult struct {
		idx      int
		safePath string
		dlURL    string
		tmpPath  string
		size     int64
		errMsg   string
		fatalErr error
	}

	jobs := make([]dlJob, 0, len(items))
	seenJobURL := make(map[string]struct{}, len(items))
	for i, it := range items {
		sp, err := SanitizeZIPPath(it.Folder, it.Filename)
		if err != nil {
			return summary, err
		}
		dlURL := strings.TrimSpace(it.DownloadURL)
		if dlURL != "" {
			key := sp + "|" + dlURL
			if _, dup := seenJobURL[key]; dup {
				continue
			}
			seenJobURL[key] = struct{}{}
		}
		jobs = append(jobs, dlJob{idx: i, item: it, safePath: sp})
	}
	summary.TotalRequested = len(jobs)

	dlCtx, cancelDL := context.WithCancel(ctx)
	defer cancelDL()

	ch := make(chan dlResult, 6)
	sem := make(chan struct{}, 6)
	var totalBytes atomic.Int64
	var wg sync.WaitGroup

	for _, j := range jobs {
		wg.Add(1)
		go func(job dlJob) {
			defer wg.Done()
			if !netx.AcquireSemCtx(dlCtx, sem) {
				ch <- dlResult{idx: job.idx, fatalErr: dlCtx.Err()}
				return
			}
			defer func() { <-sem }()

			dlURL := strings.TrimSpace(job.item.DownloadURL)
			if dlURL == "" {
				ch <- dlResult{
					idx:      job.idx,
					safePath: job.safePath,
					errMsg:   fmt.Sprintf("%s (%s): прямая ссылка недоступна", job.item.Title, job.item.Filename),
				}
				return
			}

			log.Printf("[ModHub][DownloadPack] Скачивание %s (%s): %s", job.safePath, job.item.Platform, security.SanitizeLogText(dlURL))
			tmpPath, sz, err := httpClient.DownloadToTempFile(dlCtx, dlURL, netx.MaxDownloadFileBytes)
			if err != nil {
				if dlCtx.Err() != nil {
					ch <- dlResult{idx: job.idx, fatalErr: dlCtx.Err()}
					return
				}
				log.Printf("[ModHub][DownloadPack] ОШИБКА скачивания %s: %v", job.safePath, err)
				ch <- dlResult{
					idx:      job.idx,
					safePath: job.safePath,
					errMsg:   fmt.Sprintf("%s (%s): %v", job.item.Title, job.item.Filename, err),
				}
				return
			}

			if totalBytes.Add(sz) > netx.MaxPackTotalBytes {
				totalBytes.Add(-sz)
				_ = os.Remove(tmpPath)
				ch <- dlResult{
					idx:      job.idx,
					safePath: job.safePath,
					errMsg:   fmt.Sprintf("%s (%s): превышен общий лимит размера сборки (%d МБ)", job.item.Title, job.item.Filename, netx.MaxPackTotalBytes/(1024*1024)),
				}
				return
			}

			ch <- dlResult{
				idx:      job.idx,
				safePath: job.safePath,
				dlURL:    dlURL,
				tmpPath:  tmpPath,
				size:     sz,
			}
		}(j)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	zw := zip.NewWriter(w)
	var failed []string
	writtenPaths := map[string]struct{}{}
	writtenURLs := map[string]struct{}{}
	var streamErr error
	writtenCount := 0

	for r := range ch {
		if r.fatalErr != nil {
			if streamErr == nil {
				streamErr = r.fatalErr
			}
			if r.tmpPath != "" {
				_ = os.Remove(r.tmpPath)
			}
			continue
		}
		if streamErr != nil {
			if r.tmpPath != "" {
				_ = os.Remove(r.tmpPath)
			}
			continue
		}
		if r.errMsg != "" {
			failed = append(failed, r.errMsg)
			continue
		}
		if r.dlURL != "" {
			if _, dupURL := writtenURLs[r.safePath+"|"+r.dlURL]; dupURL {
				_ = os.Remove(r.tmpPath)
				continue
			}
			writtenURLs[r.safePath+"|"+r.dlURL] = struct{}{}
		}

		finalPath := makeUniqueZIPPath(r.safePath, writtenPaths)
		if finalPath == "" {
			_ = os.Remove(r.tmpPath)
			continue
		}
		writtenPaths[finalPath] = struct{}{}

		method := zip.Deflate
		low := strings.ToLower(finalPath)
		if strings.HasSuffix(low, ".jar") || strings.HasSuffix(low, ".zip") {
			method = zip.Store
		}
		hdr := &zip.FileHeader{Name: finalPath, Method: method}
		ew, err := zw.CreateHeader(hdr)
		if err != nil {
			_ = os.Remove(r.tmpPath)
			streamErr = err
			cancelDL()
			continue
		}

		f, err := os.Open(r.tmpPath)
		if err != nil {
			_ = os.Remove(r.tmpPath)
			failed = append(failed, fmt.Sprintf("%s: ошибка чтения временного файла", finalPath))
			continue
		}
		_, copyErr := netx.CopyWithLimit(ctx, ew, f, netx.MaxDownloadFileBytes)
		_ = f.Close()
		_ = os.Remove(r.tmpPath)
		if copyErr != nil {
			streamErr = copyErr
			cancelDL()
			continue
		}
		writtenCount++
		summary.DownloadedFiles = append(summary.DownloadedFiles, finalPath)
		log.Printf("[ModHub][DownloadPack] Сохранён в ZIP: %s (%d байт)", finalPath, r.size)
	}

	summary.WrittenCount = writtenCount
	summary.FailedFiles = append([]string(nil), failed...)
	if streamErr != nil {
		_ = zw.Close()
		return summary, streamErr
	}

	if writtenCount == 0 {
		_ = zw.Close()
		if len(failed) == 0 {
			return summary, errors.New("не удалось добавить файлы в ZIP-сборку")
		}
		return summary, fmt.Errorf("не удалось скачать ни одного файла для сборки: %s", strings.Join(failed, "; "))
	}

	var manifest strings.Builder
	manifest.WriteString("ModHub Pack Manifest\n====================\n\n")
	for _, j := range jobs {
		manifest.WriteString(fmt.Sprintf("- %s  [%s]\n", j.safePath, j.item.Title))
	}
	if len(failed) > 0 {
		manifest.WriteString("\nManual download needed:\n")
		for _, f := range failed {
			manifest.WriteString("* " + f + "\n")
		}
	}
	mw, err := zw.Create("manifest.txt")
	if err != nil {
		_ = zw.Close()
		return summary, err
	}
	if _, err := mw.Write([]byte(manifest.String())); err != nil {
		_ = zw.Close()
		return summary, err
	}
	if err := zw.Close(); err != nil {
		return summary, err
	}
	log.Printf("[ModHub][DownloadPack] Готово: скачано=%d/%d ошибок=%d", summary.WrittenCount, summary.TotalRequested, len(summary.FailedFiles))
	return summary, nil
}

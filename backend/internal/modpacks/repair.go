package modpacks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"modhub/internal/downloads"
	"modhub/internal/instances"
	"modhub/internal/netx"
)

// RepairMissing restores only absent, enabled managed pack files with a saved
// trusted URL and checksum. Existing files (including modified ones) are never replaced.
func (m *Manager) RepairMissing(ctx context.Context, instanceID string) []instances.IntegrityIssue {
	inst, err := m.instMgr.Get(instanceID)
	if err != nil || inst.LinkedPackID == "" || m.store == nil {
		return nil
	}
	files, err := m.store.ListModpackFiles(inst.LinkedPackID)
	if err != nil {
		return []instances.IntegrityIssue{{Code: "pack_metadata_unavailable", Severity: "warning", Message: "Не удалось прочитать список файлов сборки: " + err.Error()}}
	}
	var issues []instances.IntegrityIssue
	for _, f := range files {
		if f.InstanceID != inst.ID || !f.Managed || !f.Enabled || f.Optional {
			continue
		}
		dest, err := ResolveSafeDestPath(inst.GameDir, f.RelPath)
		if err != nil || IsSensitiveSecretPath(f.RelPath) {
			issues = append(issues, repairIssue(f.RelPath, "небезопасный путь"))
			continue
		}
		// Never traverse symlinks inside the profile or overwrite any existing entry.
		if err := checkRepairPath(inst.GameDir, dest); err != nil {
			issues = append(issues, repairIssue(f.RelPath, err.Error()))
			continue
		}
		if _, err := os.Lstat(dest); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			issues = append(issues, repairIssue(f.RelPath, err.Error()))
			continue
		}
		algo, hash := downloads.ChecksumNone, ""
		switch {
		case validHash(f.SHA256, 32):
			algo, hash = downloads.ChecksumSHA256, f.SHA256
		case validHash(f.SHA1, 20):
			algo, hash = downloads.ChecksumSHA1, f.SHA1
		}
		if hash == "" || f.DownloadURL == "" || m.dlMgr == nil {
			issues = append(issues, repairIssue(f.RelPath, "нет проверенного URL и SHA-1/SHA-256 для восстановления"))
			continue
		}
		if _, err := netx.ValidateDownloadURL(f.DownloadURL); err != nil {
			issues = append(issues, repairIssue(f.RelPath, "источник загрузки не доверенный"))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			issues = append(issues, repairIssue(f.RelPath, err.Error()))
			continue
		}
		// Download into a distinct sibling file, then link atomically without
		// replacing a file created by the user during the download.
		var nonce [12]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			issues = append(issues, repairIssue(f.RelPath, err.Error()))
			continue
		}
		staged := dest + ".modhub-repair-" + hex.EncodeToString(nonce[:])
		task, err := m.dlMgr.Enqueue(downloads.DownloadRequest{
			Name: filepath.Base(dest), Category: f.Category, URL: f.DownloadURL,
			DestPath: staged, ChecksumAlgo: algo, ChecksumValue: hash, ExpectedSize: f.Size,
			GroupID: "repair:" + inst.ID,
		})
		if err == nil {
			var finished downloads.TaskView
			finished, err = m.dlMgr.Wait(ctx, task.ID)
			if err == nil && finished.Status != downloads.StatusCompleted {
				err = fmt.Errorf("загрузка не завершена: %s", finished.Error)
			}
			if ctx.Err() != nil {
				// Wait only stops waiting; cancel the queued/running download too,
				// then wait for its worker to exit before removing the staging path.
				m.dlMgr.Cancel(task.ID)
				if _, waitErr := m.dlMgr.Wait(context.Background(), task.ID); waitErr != nil {
					issues = append(issues, repairIssue(f.RelPath, waitErr.Error()))
					continue
				}
				err = ctx.Err()
			}
		}
		if err == nil {
			if err = checkRepairPath(inst.GameDir, dest); err == nil {
				err = os.Link(staged, dest)
			}
		}
		_ = os.Remove(staged)
		if err != nil {
			issues = append(issues, repairIssue(f.RelPath, err.Error()))
		}
	}
	return issues
}

func repairIssue(rel, reason string) instances.IntegrityIssue {
	return instances.IntegrityIssue{Code: "pack_file_unrepairable", Severity: "missing_component", Message: fmt.Sprintf("Файл сборки %s не восстановлен: %s", rel, reason)}
}

func validHash(value string, size int) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == size
}

func checkRepairPath(base, dest string) error {
	base, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("путь вне профиля")
	}
	dir := base
	for _, segment := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		info, err := os.Lstat(dir)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("путь проходит через ссылку или файл")
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if segment != "." {
			dir = filepath.Join(dir, segment)
		}
	}
	info, err := os.Lstat(dir)
	if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("путь проходит через ссылку или файл")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

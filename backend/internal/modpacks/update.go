package modpacks

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modhub/internal/domain"
	"modhub/internal/downloads"
	"modhub/internal/events"
)

type updateOperation struct {
	path         string
	stage        string
	remove       bool
	previous     string
	existed      bool
	expectedHash string
	backup       bool
	backupPath   string
}

func (m *Manager) applyUpdateSafe(ctx context.Context, req ApplyUpdateRequest) (ModpackView, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req.ConfigConflictMode != "" && req.ConfigConflictMode != ConflictKeepExisting && req.ConfigConflictMode != ConflictOverwrite && req.ConfigConflictMode != ConflictBackupAndOverwrite {
		return ModpackView{}, fmt.Errorf("неподдерживаемая стратегия конфликтов конфигурации")
	}
	// Serialize updates without blocking import progress or preview reads during I/O.
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	m.mu.Lock()
	plan, ok := m.updatePlans[req.PreviewID]
	m.mu.Unlock()
	if !ok || plan.diff.ModpackID != req.ModpackID || time.Since(plan.createdAt) > 2*time.Hour {
		return ModpackView{}, ErrPreviewNotFound
	}
	if m.instMgr == nil || m.store == nil {
		return ModpackView{}, ErrModpackNotFound
	}
	commit, ok := m.store.(interface {
		CommitModpackUpdate(domain.ModpackRecord, domain.ModpackRecord, []domain.ModpackFileRecord, domain.ModpackUpdateRecord) error
	})
	if !ok {
		return ModpackView{}, errors.New("хранилище не поддерживает атомарное обновление сборки")
	}
	recs, err := m.store.ListModpacks()
	if err != nil {
		return ModpackView{}, err
	}
	var rec domain.ModpackRecord
	found := false
	for _, r := range recs {
		if r.ID == req.ModpackID {
			rec, found = r, true
			break
		}
	}
	if !found {
		return ModpackView{}, ErrModpackNotFound
	}
	if rec.Version != plan.diff.CurrentVersion || (plan.oldManifestHash != "" && rec.ManifestHash != plan.oldManifestHash) {
		return ModpackView{}, ErrPreviewNotFound
	}
	if err := m.instMgr.AcquireBusyExclusive(rec.InstanceID); err != nil {
		return ModpackView{}, err
	}
	defer m.instMgr.ReleaseBusy(rec.InstanceID)
	inst, err := m.instMgr.Get(rec.InstanceID)
	if err != nil {
		return ModpackView{}, err
	}
	oldFiles, err := m.store.ListModpackFiles(rec.ID)
	if err != nil {
		return ModpackView{}, err
	}
	old := make(map[string]domain.ModpackFileRecord, len(oldFiles))
	for _, f := range oldFiles {
		old[strings.ToLower(f.RelPath)] = f
	}
	mode := req.ConfigConflictMode
	if mode == "" {
		mode = ConflictKeepExisting
	}
	if len(plan.diff.UnavailableFiles) > 0 {
		return ModpackView{}, errors.New("обновление содержит недоступные файлы; версия сборки не может быть подтверждена даже при пропуске")
	}
	stageDir, err := os.MkdirTemp(inst.GameDir, ".mpupdate-*")
	if err != nil {
		return ModpackView{}, err
	}
	cleanupStage := true
	defer func() {
		if cleanupStage {
			_ = os.RemoveAll(stageDir)
		}
	}()
	var zr *zip.ReadCloser
	zipMap := map[string]*zip.File{}
	if plan.newPrev.ResolvedArchivePath != "" {
		zr, err = zip.OpenReader(plan.newPrev.ResolvedArchivePath)
		if err != nil {
			return ModpackView{}, err
		}
		defer zr.Close()
		for _, f := range zr.File {
			zipMap[f.Name] = f
		}
	}
	apply := map[string]bool{}
	conflict := map[string]bool{}
	for _, e := range plan.diff.Added {
		apply[strings.ToLower(e.RelPath)] = true
	}
	for _, e := range plan.diff.Updated {
		apply[strings.ToLower(e.RelPath)] = true
	}
	for _, e := range plan.diff.ConfigConflicts {
		k := strings.ToLower(e.RelPath)
		conflict[k] = true
		if mode != ConflictKeepExisting {
			apply[k] = true
		}
	}
	operations := make([]updateOperation, 0)
	newFiles := make([]domain.ModpackFileRecord, 0, len(plan.newPrev.Files))
	now := time.Now().Unix()
	for i, nf := range plan.newPrev.Files {
		if nf.Optional && !nf.SelectedByDefault {
			continue
		}
		path, e := ResolveSafeDestPath(inst.GameDir, nf.RelPath)
		if e != nil {
			return ModpackView{}, e
		}
		key := strings.ToLower(nf.RelPath)
		if apply[key] {
			if !nf.Available {
				return ModpackView{}, fmt.Errorf("файл %s недоступен", nf.RelPath)
			}
			staged := filepath.Join(stageDir, fmt.Sprintf("file-%d", i))
			if nf.SourceKind == "download" {
				if m.dlMgr == nil || len(nf.DownloadURLs) == 0 {
					return ModpackView{}, fmt.Errorf("нет загрузки для %s", nf.RelPath)
				}
				algo, checksum := downloads.ChecksumNone, ""
				if nf.SHA256 != "" {
					algo, checksum = downloads.ChecksumSHA256, nf.SHA256
				} else if nf.SHA1 != "" {
					algo, checksum = downloads.ChecksumSHA1, nf.SHA1
				}
				task, e := m.dlMgr.Enqueue(downloads.DownloadRequest{Name: fallbackStr(nf.Title, nf.Filename), Category: string(nf.Category), URL: nf.DownloadURLs[0], DestPath: staged, ExpectedSize: nf.Size, ChecksumAlgo: algo, ChecksumValue: checksum, GroupID: "mpupdate:" + rec.ID})
				if e != nil {
					return ModpackView{}, e
				}
				fin, e := m.dlMgr.Wait(ctx, task.ID)
				if e != nil {
					return ModpackView{}, e
				}
				if fin.Status != downloads.StatusCompleted {
					return ModpackView{}, fmt.Errorf("загрузка %s не завершена: %s", nf.RelPath, fin.Status)
				}
			} else {
				zf := zipMap[nf.ArchiveEntry]
				if zf == nil {
					return ModpackView{}, fmt.Errorf("архив не содержит %s", nf.RelPath)
				}
				if e := extractZipFileBounded(zf, staged, DefaultMaxSingleEntryBytes); e != nil {
					return ModpackView{}, e
				}
			}
			h, e := ComputeFileHashes(staged)
			if e != nil {
				return ModpackView{}, e
			}
			if (nf.SHA256 != "" && !strings.EqualFold(nf.SHA256, h.SHA256)) || (nf.SHA1 != "" && !strings.EqualFold(nf.SHA1, h.SHA1)) || (nf.SHA512 != "" && !strings.EqualFold(nf.SHA512, h.SHA512)) {
				return ModpackView{}, fmt.Errorf("контрольная сумма %s не совпадает", nf.RelPath)
			}
			operations = append(operations, updateOperation{path: path, stage: staged, backup: conflict[key] && mode == ConflictBackupAndOverwrite})
		} else {
			// A kept conflict must stay user-owned, not become a managed file of the new pack.
			if conflict[key] {
				if f, exists := old[key]; exists {
					newFiles = append(newFiles, f)
				}
				continue
			}
			if previous, exists := old[key]; exists {
				current, e := ComputeFileHashes(path)
				if e != nil {
					return ModpackView{}, fmt.Errorf("неизменённый файл %s отсутствует: %w", nf.RelPath, e)
				}
				if (previous.SHA256 != "" && !strings.EqualFold(previous.SHA256, current.SHA256)) || (previous.SHA256 == "" && previous.SHA1 != "" && !strings.EqualFold(previous.SHA1, current.SHA1)) {
					return ModpackView{}, fmt.Errorf("файл %s изменён после анализа; повторите анализ", nf.RelPath)
				}
				newFiles = append(newFiles, previous)
				continue
			}
		}
		url := ""
		if len(nf.DownloadURLs) > 0 {
			url = nf.DownloadURLs[0]
		}
		h, e := ComputeFileHashes(path)
		if apply[key] {
			h, e = ComputeFileHashes(operations[len(operations)-1].stage)
		}
		if e != nil {
			return ModpackView{}, fmt.Errorf("файл %s отсутствует или недоступен: %w", nf.RelPath, e)
		}
		newFiles = append(newFiles, domain.ModpackFileRecord{ModpackID: rec.ID, InstanceID: inst.ID, RelPath: nf.RelPath, Category: string(nf.Category), SourcePlatform: nf.SourcePlatform, SourceProjectID: nf.SourceProjectID, SourceFileID: nf.SourceFileID, SHA1: h.SHA1, SHA256: h.SHA256, SHA512: h.SHA512, Size: h.Size, Managed: true, Optional: nf.Optional, Enabled: true, DownloadURL: url, UpdatedAt: now})
	}
	for _, rem := range plan.diff.Removed {
		path, e := ResolveSafeDestPath(inst.GameDir, rem.RelPath)
		if e != nil {
			return ModpackView{}, e
		}
		operations = append(operations, updateOperation{path: path, remove: true})
	}
	if len(operations) == 0 && len(plan.diff.ConfigConflicts) > 0 {
		return ModpackView{}, errors.New("все изменения сборки конфликтуют с локальными файлами; версия не обновлена")
	}
	// Recheck on-disk state immediately before mutation; previews may be stale.
	for i := range operations {
		op := &operations[i]
		if h, e := ComputeFileHashes(op.path); e == nil {
			op.existed = true
			op.expectedHash = h.SHA256
			key := strings.ToLower(filepath.ToSlash(strings.TrimPrefix(op.path, inst.GameDir+string(filepath.Separator))))
			if f, ok := old[key]; ok && ((f.SHA256 != "" && !strings.EqualFold(h.SHA256, f.SHA256)) || (f.SHA256 == "" && f.SHA1 != "" && !strings.EqualFold(h.SHA1, f.SHA1))) && !op.backup {
				return ModpackView{}, fmt.Errorf("файл %s изменён после импорта; повторите анализ", key)
			}
			if f, ok := old[key]; ok && f.SHA256 == "" && f.SHA1 == "" && !op.backup {
				return ModpackView{}, fmt.Errorf("нет исходной контрольной суммы %s; безопасное удаление невозможно", key)
			}
			if _, ok := old[key]; !ok && !op.backup {
				return ModpackView{}, fmt.Errorf("файл %s добавлен пользователем; повторите анализ", key)
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return ModpackView{}, e
		}
		if op.remove && !op.existed {
			return ModpackView{}, fmt.Errorf("удаляемый файл %s отсутствует; повторите анализ", op.path)
		}
	}
	applied := 0
	rollback := func(cause error) (ModpackView, error) {
		for j := applied - 1; j >= 0; j-- {
			op := operations[j]
			if op.existed {
				_ = os.Remove(op.path)
				if e := os.Rename(op.previous, op.path); e != nil {
					cleanupStage = false // Keep original copy for manual recovery.
					cause = errors.Join(cause, fmt.Errorf("восстановление %s: %w (копия: %s)", op.path, e, op.previous))
				}
			} else {
				_ = os.Remove(op.path)
			}
		}
		for _, op := range operations {
			if op.backupPath != "" {
				_ = os.Remove(op.backupPath)
			}
		}
		return ModpackView{}, cause
	}
	for i := range operations {
		op := &operations[i]
		if e := ctx.Err(); e != nil {
			return rollback(e)
		}
		// A concurrent user edit during staging must not be overwritten.
		if op.existed {
			oldHash, e := ComputeFileHashes(op.path)
			if e != nil {
				return rollback(e)
			}
			if !strings.EqualFold(oldHash.SHA256, op.expectedHash) {
				return rollback(fmt.Errorf("файл %s изменён во время обновления", op.path))
			}
		} else if _, e := os.Lstat(op.path); e == nil {
			return rollback(fmt.Errorf("файл %s появился во время обновления", op.path))
		} else if !errors.Is(e, os.ErrNotExist) {
			return rollback(e)
		}
		if e := os.MkdirAll(filepath.Dir(op.path), 0o755); e != nil {
			return rollback(e)
		}
		if op.existed {
			op.previous = filepath.Join(stageDir, fmt.Sprintf("previous-%d", i))
			if e := os.Rename(op.path, op.previous); e != nil {
				return rollback(e)
			}
		}
		applied++
		if !op.remove {
			if e := os.Rename(op.stage, op.path); e != nil {
				return rollback(e)
			}
		}
	}
	for i := range operations {
		op := &operations[i]
		if !op.backup || !op.existed {
			continue
		}
		backupPath := op.path + ".bak." + genID("update")
		in, e := os.Open(op.previous)
		if e != nil {
			return rollback(e)
		}
		out, e := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if e != nil {
			_ = in.Close()
			return rollback(e)
		}
		op.backupPath = backupPath
		_, e = io.Copy(out, in)
		if ce := out.Close(); e == nil {
			e = ce
		}
		_ = in.Close()
		if e != nil {
			return rollback(e)
		}
	}
	updated := rec
	updated.Version = fallbackStr(plan.newPrev.Version, rec.Version)
	updated.ManifestHash = plan.newPrev.ManifestHash
	updated.SourceVersionID = fallbackStr(plan.newPrev.SourceVersionID, rec.SourceVersionID)
	updated.MCVersion = fallbackStr(plan.newPrev.MCVersion, rec.MCVersion)
	updated.Loader = fallbackStr(plan.newPrev.Loader, rec.Loader)
	updated.LoaderVersion = fallbackStr(plan.newPrev.LoaderVersion, rec.LoaderVersion)
	updated.UpdatedAt = now
	history := domain.ModpackUpdateRecord{ID: genID("mpupd"), ModpackID: rec.ID, FromVersion: rec.Version, ToVersion: updated.Version, AddedCount: len(plan.diff.Added), UpdatedCount: len(plan.diff.Updated), RemovedCount: len(plan.diff.Removed), PreservedCount: len(plan.diff.PreservedUser), Status: "completed", Summary: "Обновление завершено", AppliedAt: now}
	if e := commit.CommitModpackUpdate(rec, updated, newFiles, history); e != nil {
		return rollback(e)
	}
	m.mu.Lock()
	delete(m.updatePlans, req.PreviewID)
	m.mu.Unlock()
	view := m.recordToView(updated)
	if m.bus != nil {
		m.bus.Publish(events.EventModpackUpdated, view)
	}
	return view, nil
}

package storage

import (
	"database/sql"
	"modhub/internal/domain"
)

func (d *Database) UpsertModpack(rec domain.ModpackRecord) error {
	if d == nil {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	_, err := d.writeDB.Exec(`
		INSERT INTO modpacks (
			id, name, author, description, version, format, source_type, source_ref,
			source_project_id, source_version_id, mc_version, loader, loader_version,
			instance_id, manifest_hash, status, last_error, created_at, updated_at, installed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, author=excluded.author, description=excluded.description,
			version=excluded.version, format=excluded.format, source_type=excluded.source_type,
			source_ref=excluded.source_ref, source_project_id=excluded.source_project_id,
			source_version_id=excluded.source_version_id, mc_version=excluded.mc_version,
			loader=excluded.loader, loader_version=excluded.loader_version,
			instance_id=excluded.instance_id, manifest_hash=excluded.manifest_hash,
			status=excluded.status, last_error=excluded.last_error,
			updated_at=excluded.updated_at, installed_at=excluded.installed_at
	`,
		rec.ID, rec.Name, rec.Author, rec.Description, rec.Version, rec.Format, rec.SourceType, rec.SourceRef,
		rec.SourceProjectID, rec.SourceVersionID, rec.MCVersion, rec.Loader, rec.LoaderVersion,
		rec.InstanceID, rec.ManifestHash, rec.Status, rec.LastError, rec.CreatedAt, rec.UpdatedAt, rec.InstalledAt,
	)
	return err
}

func (d *Database) ListModpacks() ([]domain.ModpackRecord, error) {
	if d == nil {
		return nil, nil
	}
	rows, err := d.readDB.Query(`
		SELECT id, name, author, description, version, format, source_type, source_ref,
		       source_project_id, source_version_id, mc_version, loader, loader_version,
		       instance_id, manifest_hash, status, last_error, created_at, updated_at, installed_at
		FROM modpacks ORDER BY updated_at DESC, created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ModpackRecord, 0, 16)
	for rows.Next() {
		var rec domain.ModpackRecord
		if rows.Scan(
			&rec.ID, &rec.Name, &rec.Author, &rec.Description, &rec.Version, &rec.Format, &rec.SourceType, &rec.SourceRef,
			&rec.SourceProjectID, &rec.SourceVersionID, &rec.MCVersion, &rec.Loader, &rec.LoaderVersion,
			&rec.InstanceID, &rec.ManifestHash, &rec.Status, &rec.LastError, &rec.CreatedAt, &rec.UpdatedAt, &rec.InstalledAt,
		) == nil {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (d *Database) DeleteModpack(id string) error {
	if d == nil || id == "" {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	tx, err := d.writeDB.Begin()
	if err != nil {
		return err
	}
	_, _ = tx.Exec(`DELETE FROM modpack_files WHERE modpack_id = ?`, id)
	_, _ = tx.Exec(`DELETE FROM modpack_updates WHERE modpack_id = ?`, id)
	if _, err := tx.Exec(`DELETE FROM modpacks WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *Database) ReplaceModpackFiles(modpackID string, files []domain.ModpackFileRecord) error {
	if d == nil || modpackID == "" {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	tx, err := d.writeDB.Begin()
	if err != nil {
		return err
	}
	if err := replaceModpackFilesTx(tx, modpackID, files); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func replaceModpackFilesTx(tx *sql.Tx, modpackID string, files []domain.ModpackFileRecord) error {
	if _, err := tx.Exec(`DELETE FROM modpack_files WHERE modpack_id = ?`, modpackID); err != nil {
		return err
	}
	if len(files) > 0 {
		stmt, err := tx.Prepare(`
			INSERT OR REPLACE INTO modpack_files (
				modpack_id, instance_id, rel_path, category, source_platform,
				source_project_id, source_file_id, sha1, sha256, sha512,
				size, managed, optional, enabled, download_url, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, f := range files {
			mng, opt, en := 0, 0, 0
			if f.Managed {
				mng = 1
			}
			if f.Optional {
				opt = 1
			}
			if f.Enabled {
				en = 1
			}
			if _, err := stmt.Exec(
				modpackID, f.InstanceID, f.RelPath, f.Category, f.SourcePlatform,
				f.SourceProjectID, f.SourceFileID, f.SHA1, f.SHA256, f.SHA512,
				f.Size, mng, opt, en, f.DownloadURL, f.UpdatedAt,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

// CommitModpackUpdate atomically advances pack metadata, file inventory and history.
func (d *Database) CommitModpackUpdate(expected, rec domain.ModpackRecord, files []domain.ModpackFileRecord, update domain.ModpackUpdateRecord) error {
	if d == nil || d.closed.Load() {
		return sql.ErrConnDone
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return sql.ErrConnDone
	}
	tx, err := d.writeDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE modpacks SET version=?, manifest_hash=?, source_version_id=?, mc_version=?, loader=?, loader_version=?, updated_at=? WHERE id=? AND version=? AND manifest_hash=? AND instance_id=?`, rec.Version, rec.ManifestHash, rec.SourceVersionID, rec.MCVersion, rec.Loader, rec.LoaderVersion, rec.UpdatedAt, rec.ID, expected.Version, expected.ManifestHash, expected.InstanceID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	if err := replaceModpackFilesTx(tx, rec.ID, files); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO modpack_updates (id, modpack_id, from_version, to_version, added_count, updated_count, removed_count, preserved_count, status, summary, applied_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, update.ID, update.ModpackID, update.FromVersion, update.ToVersion, update.AddedCount, update.UpdatedCount, update.RemovedCount, update.PreservedCount, update.Status, update.Summary, update.AppliedAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (d *Database) ListModpackFiles(modpackID string) ([]domain.ModpackFileRecord, error) {
	if d == nil || modpackID == "" {
		return nil, nil
	}
	rows, err := d.readDB.Query(`
		SELECT modpack_id, instance_id, rel_path, category, source_platform,
		       source_project_id, source_file_id, sha1, sha256, sha512,
		       size, managed, optional, enabled, download_url, updated_at
		FROM modpack_files WHERE modpack_id = ? ORDER BY rel_path ASC
	`, modpackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ModpackFileRecord, 0, 32)
	for rows.Next() {
		var f domain.ModpackFileRecord
		var mng, opt, en int
		if rows.Scan(
			&f.ModpackID, &f.InstanceID, &f.RelPath, &f.Category, &f.SourcePlatform,
			&f.SourceProjectID, &f.SourceFileID, &f.SHA1, &f.SHA256, &f.SHA512,
			&f.Size, &mng, &opt, &en, &f.DownloadURL, &f.UpdatedAt,
		) == nil {
			f.Managed = mng != 0
			f.Optional = opt != 0
			f.Enabled = en != 0
			out = append(out, f)
		}
	}
	return out, nil
}

func (d *Database) InsertModpackUpdate(rec domain.ModpackUpdateRecord) error {
	if d == nil {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	_, err := d.writeDB.Exec(`
		INSERT INTO modpack_updates (
			id, modpack_id, from_version, to_version, added_count, updated_count,
			removed_count, preserved_count, status, summary, applied_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rec.ID, rec.ModpackID, rec.FromVersion, rec.ToVersion, rec.AddedCount, rec.UpdatedCount,
		rec.RemovedCount, rec.PreservedCount, rec.Status, rec.Summary, rec.AppliedAt,
	)
	return err
}

func (d *Database) ListModpackUpdates(modpackID string) ([]domain.ModpackUpdateRecord, error) {
	if d == nil || modpackID == "" {
		return nil, nil
	}
	rows, err := d.readDB.Query(`
		SELECT id, modpack_id, from_version, to_version, added_count, updated_count,
		       removed_count, preserved_count, status, summary, applied_at
		FROM modpack_updates WHERE modpack_id = ? ORDER BY applied_at DESC LIMIT 50
	`, modpackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ModpackUpdateRecord, 0, 8)
	for rows.Next() {
		var rec domain.ModpackUpdateRecord
		if rows.Scan(
			&rec.ID, &rec.ModpackID, &rec.FromVersion, &rec.ToVersion, &rec.AddedCount, &rec.UpdatedCount,
			&rec.RemovedCount, &rec.PreservedCount, &rec.Status, &rec.Summary, &rec.AppliedAt,
		) == nil {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (d *Database) ListModpackFileStats() (map[string][2]int, error) {
	out := map[string][2]int{}
	if d == nil {
		return out, nil
	}
	rows, err := d.readDB.Query(`
		SELECT modpack_id, COUNT(*), SUM(CASE WHEN managed != 0 THEN 1 ELSE 0 END)
		FROM modpack_files
		GROUP BY modpack_id
	`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var total, managed int
		if rows.Scan(&id, &total, &managed) == nil && id != "" {
			out[id] = [2]int{total, managed}
		}
	}
	return out, nil
}

func (d *Database) ListRecentModpackUpdatesMap(perPackLimit int) (map[string][]domain.ModpackUpdateRecord, error) {
	out := map[string][]domain.ModpackUpdateRecord{}
	if d == nil {
		return out, nil
	}
	if perPackLimit <= 0 {
		perPackLimit = 20
	}
	rows, err := d.readDB.Query(`
		SELECT id, modpack_id, from_version, to_version, added_count, updated_count,
		       removed_count, preserved_count, status, summary, applied_at
		FROM modpack_updates
		ORDER BY applied_at DESC
		LIMIT 500
	`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var rec domain.ModpackUpdateRecord
		if rows.Scan(
			&rec.ID, &rec.ModpackID, &rec.FromVersion, &rec.ToVersion, &rec.AddedCount, &rec.UpdatedCount,
			&rec.RemovedCount, &rec.PreservedCount, &rec.Status, &rec.Summary, &rec.AppliedAt,
		) == nil && rec.ModpackID != "" {
			if len(out[rec.ModpackID]) < perPackLimit {
				out[rec.ModpackID] = append(out[rec.ModpackID], rec)
			}
		}
	}
	return out, nil
}

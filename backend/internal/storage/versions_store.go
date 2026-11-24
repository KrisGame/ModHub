package storage

import "modhub/internal/domain"

func (d *Database) UpsertInstalledVersion(rec domain.InstalledVersionRecord) error {
	if d == nil {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	verInt, actInt := 0, 0
	if rec.Verified {
		verInt = 1
	}
	if rec.Active {
		actInt = 1
	}
	tx, err := d.writeDB.Begin()
	if err != nil {
		return err
	}
	if rec.Active {
		if _, err := tx.Exec(`UPDATE installed_versions SET active = 0 WHERE instance_id = ?`, rec.InstanceID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	_, err = tx.Exec(`
		INSERT INTO installed_versions (
			id, instance_id, version_id, mc_version, install_type, modloader, loader_version,
			java_runtime_id, java_path, java_major, metadata_path, installed_at,
			verified, verified_at, verify_error, active
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, version_id) DO UPDATE SET
			mc_version=excluded.mc_version,
			install_type=excluded.install_type,
			modloader=excluded.modloader,
			loader_version=excluded.loader_version,
			java_runtime_id=excluded.java_runtime_id,
			java_path=excluded.java_path,
			java_major=excluded.java_major,
			metadata_path=excluded.metadata_path,
			installed_at=excluded.installed_at,
			verified=excluded.verified,
			verified_at=excluded.verified_at,
			verify_error=excluded.verify_error,
			active=excluded.active
	`,
		rec.ID, rec.InstanceID, rec.VersionID, rec.MCVersion, rec.InstallType, rec.Modloader, rec.LoaderVersion,
		rec.JavaRuntimeID, rec.JavaPath, rec.JavaMajor, rec.MetadataPath, rec.InstalledAt,
		verInt, rec.VerifiedAt, rec.VerifyError, actInt,
	)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *Database) ListInstalledVersions(instanceID string) ([]domain.InstalledVersionRecord, error) {
	if d == nil {
		return nil, nil
	}
	var (
		rows interface {
			Next() bool
			Scan(...any) error
			Close() error
		}
		err error
	)
	if instanceID != "" {
		rows, err = d.readDB.Query(`
			SELECT id, instance_id, version_id, mc_version, install_type, modloader, loader_version,
			       java_runtime_id, java_path, java_major, metadata_path, installed_at,
			       verified, verified_at, verify_error, active
			FROM installed_versions
			WHERE instance_id = ?
			ORDER BY active DESC, installed_at DESC
		`, instanceID)
	} else {
		rows, err = d.readDB.Query(`
			SELECT id, instance_id, version_id, mc_version, install_type, modloader, loader_version,
			       java_runtime_id, java_path, java_major, metadata_path, installed_at,
			       verified, verified_at, verify_error, active
			FROM installed_versions
			ORDER BY active DESC, installed_at DESC
		`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.InstalledVersionRecord, 0, 8)
	for rows.Next() {
		var rec domain.InstalledVersionRecord
		var verInt, actInt int
		if rows.Scan(
			&rec.ID, &rec.InstanceID, &rec.VersionID, &rec.MCVersion, &rec.InstallType, &rec.Modloader, &rec.LoaderVersion,
			&rec.JavaRuntimeID, &rec.JavaPath, &rec.JavaMajor, &rec.MetadataPath, &rec.InstalledAt,
			&verInt, &rec.VerifiedAt, &rec.VerifyError, &actInt,
		) == nil {
			rec.Verified = verInt != 0
			rec.Active = actInt != 0
			out = append(out, rec)
		}
	}
	return out, nil
}

func (d *Database) SetActiveInstalledVersion(instanceID, installedID string) error {
	if d == nil || instanceID == "" || installedID == "" {
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
	if _, err := tx.Exec(`UPDATE installed_versions SET active = 0 WHERE instance_id = ?`, instanceID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE installed_versions SET active = 1 WHERE instance_id = ? AND id = ?`, instanceID, installedID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *Database) DeleteInstalledVersion(instanceID, installedID string) error {
	if d == nil || installedID == "" {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	_, err := d.writeDB.Exec(`DELETE FROM installed_versions WHERE instance_id = ? AND id = ?`, instanceID, installedID)
	return err
}

func (d *Database) CountVersionIDReferences(versionID string) int {
	if d == nil || versionID == "" {
		return 0
	}
	var cnt int
	_ = d.readDB.QueryRow(`SELECT COUNT(*) FROM installed_versions WHERE version_id = ?`, versionID).Scan(&cnt)
	return cnt
}

package storage

import "modhub/internal/domain"

func (d *Database) UpsertJavaRuntime(rec domain.JavaRuntimeRecord) error {
	if d == nil {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	mng, ver := 0, 0
	if rec.Managed {
		mng = 1
	}
	if rec.Verified {
		ver = 1
	}
	_, err := d.writeDB.Exec(`
		INSERT INTO java_runtimes (
			id, component, name, executable_path, root_dir, version,
			major_version, arch, bitness, vendor, source, managed,
			verified, installed_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(executable_path) DO UPDATE SET
			component=excluded.component,
			name=excluded.name,
			root_dir=excluded.root_dir,
			version=excluded.version,
			major_version=excluded.major_version,
			arch=excluded.arch,
			bitness=excluded.bitness,
			vendor=excluded.vendor,
			source=excluded.source,
			managed=excluded.managed,
			verified=excluded.verified,
			updated_at=excluded.updated_at
	`,
		rec.ID, rec.Component, rec.Name, rec.ExecutablePath, rec.RootDir, rec.Version,
		rec.MajorVersion, rec.Arch, rec.Bitness, rec.Vendor, rec.Source, mng,
		ver, rec.InstalledAt, rec.UpdatedAt,
	)
	return err
}

func (d *Database) ListJavaRuntimes() ([]domain.JavaRuntimeRecord, error) {
	if d == nil {
		return nil, nil
	}
	rows, err := d.readDB.Query(`
		SELECT id, component, name, executable_path, root_dir, version,
		       major_version, arch, bitness, vendor, source, managed,
		       verified, installed_at, updated_at
		FROM java_runtimes
		ORDER BY managed DESC, major_version DESC, version DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.JavaRuntimeRecord, 0, 8)
	for rows.Next() {
		var rec domain.JavaRuntimeRecord
		var mng, ver int
		if err := rows.Scan(
			&rec.ID, &rec.Component, &rec.Name, &rec.ExecutablePath, &rec.RootDir, &rec.Version,
			&rec.MajorVersion, &rec.Arch, &rec.Bitness, &rec.Vendor, &rec.Source, &mng,
			&ver, &rec.InstalledAt, &rec.UpdatedAt,
		); err == nil {
			rec.Managed = mng != 0
			rec.Verified = ver != 0
			out = append(out, rec)
		}
	}
	return out, nil
}

func (d *Database) DeleteJavaRuntime(id string) error {
	if d == nil || id == "" {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	_, err := d.writeDB.Exec(`DELETE FROM java_runtimes WHERE id = ?`, id)
	return err
}

package storage

import "modhub/internal/domain"

func (d *Database) UpsertInstance(rec domain.InstanceRecord) error {
	if d == nil {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	_, err := d.writeDB.Exec(`
		INSERT INTO instances (
			id, name, description, icon, mc_version, instance_type, loader_version,
			root_dir, game_dir, java_mode, java_runtime_id, java_path,
			memory_min_mb, memory_max_mb, launch_settings_json,
			linked_pack_id, linked_pack_name, status,
			created_at, updated_at, last_played_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			description=excluded.description,
			icon=excluded.icon,
			mc_version=excluded.mc_version,
			instance_type=excluded.instance_type,
			loader_version=excluded.loader_version,
			root_dir=excluded.root_dir,
			game_dir=excluded.game_dir,
			java_mode=excluded.java_mode,
			java_runtime_id=excluded.java_runtime_id,
			java_path=excluded.java_path,
			memory_min_mb=excluded.memory_min_mb,
			memory_max_mb=excluded.memory_max_mb,
			launch_settings_json=excluded.launch_settings_json,
			linked_pack_id=excluded.linked_pack_id,
			linked_pack_name=excluded.linked_pack_name,
			status=excluded.status,
			updated_at=excluded.updated_at,
			last_played_at=excluded.last_played_at
	`,
		rec.ID, rec.Name, rec.Description, rec.Icon, rec.MCVersion, rec.InstanceType, rec.LoaderVersion,
		rec.RootDir, rec.GameDir, rec.JavaMode, rec.JavaRuntimeID, rec.JavaPath,
		rec.MemoryMinMB, rec.MemoryMaxMB, rec.LaunchSettingsJSON,
		rec.LinkedPackID, rec.LinkedPackName, rec.Status,
		rec.CreatedAt, rec.UpdatedAt, rec.LastPlayedAt,
	)
	return err
}

func (d *Database) ListInstances() ([]domain.InstanceRecord, error) {
	if d == nil {
		return nil, nil
	}
	rows, err := d.readDB.Query(`
		SELECT id, name, description, icon, mc_version, instance_type, loader_version,
		       root_dir, game_dir, java_mode, java_runtime_id, java_path,
		       memory_min_mb, memory_max_mb, launch_settings_json,
		       linked_pack_id, linked_pack_name, status,
		       created_at, updated_at, last_played_at
		FROM instances
		ORDER BY updated_at DESC, created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.InstanceRecord, 0, 16)
	for rows.Next() {
		var rec domain.InstanceRecord
		if err := rows.Scan(
			&rec.ID, &rec.Name, &rec.Description, &rec.Icon, &rec.MCVersion, &rec.InstanceType, &rec.LoaderVersion,
			&rec.RootDir, &rec.GameDir, &rec.JavaMode, &rec.JavaRuntimeID, &rec.JavaPath,
			&rec.MemoryMinMB, &rec.MemoryMaxMB, &rec.LaunchSettingsJSON,
			&rec.LinkedPackID, &rec.LinkedPackName, &rec.Status,
			&rec.CreatedAt, &rec.UpdatedAt, &rec.LastPlayedAt,
		); err == nil {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (d *Database) DeleteInstance(id string) error {
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
	_, _ = tx.Exec(`DELETE FROM instance_pack_links WHERE instance_id = ?`, id)
	if _, err := tx.Exec(`DELETE FROM instances WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *Database) SetInstancePackLink(instanceID, packID, packName string, linkedAt int64) error {
	if d == nil || instanceID == "" {
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
	if _, err := tx.Exec(`DELETE FROM instance_pack_links WHERE instance_id = ?`, instanceID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if packID != "" {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO instance_pack_links (instance_id, pack_id, pack_name, linked_at) VALUES (?, ?, ?, ?)`,
			instanceID, packID, packName, linkedAt,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (d *Database) ListInstancePackLinks(instanceID string) ([]domain.InstancePackLinkRecord, error) {
	if d == nil {
		return nil, nil
	}
	var (
		rows interface{ Next() bool; Scan(...any) error; Close() error }
		err  error
	)
	if instanceID != "" {
		rows, err = d.readDB.Query(`SELECT instance_id, pack_id, pack_name, linked_at FROM instance_pack_links WHERE instance_id = ? ORDER BY linked_at DESC`, instanceID)
	} else {
		rows, err = d.readDB.Query(`SELECT instance_id, pack_id, pack_name, linked_at FROM instance_pack_links ORDER BY linked_at DESC`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.InstancePackLinkRecord
	for rows.Next() {
		var r domain.InstancePackLinkRecord
		if rows.Scan(&r.InstanceID, &r.PackID, &r.PackName, &r.LinkedAt) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

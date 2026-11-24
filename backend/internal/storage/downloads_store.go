package storage

import "modhub/internal/domain"

func (d *Database) UpsertDownloadTask(rec domain.DownloadTaskRecord) error {
	if d == nil {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	supRange := 0
	if rec.SupportsRange {
		supRange = 1
	}
	_, err := d.writeDB.Exec(`
		INSERT INTO download_tasks (
			id, name, category, url, dest_path, temp_path,
			total_bytes, downloaded_bytes, checksum_algo, checksum_value,
			integrity_note, status, error_msg, attempts, max_retries,
			supports_range, max_bytes, group_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			category=excluded.category,
			url=excluded.url,
			dest_path=excluded.dest_path,
			temp_path=excluded.temp_path,
			total_bytes=excluded.total_bytes,
			downloaded_bytes=excluded.downloaded_bytes,
			checksum_algo=excluded.checksum_algo,
			checksum_value=excluded.checksum_value,
			integrity_note=excluded.integrity_note,
			status=excluded.status,
			error_msg=excluded.error_msg,
			attempts=excluded.attempts,
			max_retries=excluded.max_retries,
			supports_range=excluded.supports_range,
			max_bytes=excluded.max_bytes,
			group_id=excluded.group_id,
			updated_at=excluded.updated_at
	`,
		rec.ID, rec.Name, rec.Category, rec.URL, rec.DestPath, rec.TempPath,
		rec.TotalBytes, rec.DownloadedBytes, rec.ChecksumAlgo, rec.ChecksumValue,
		rec.IntegrityNote, rec.Status, rec.ErrorMsg, rec.Attempts, rec.MaxRetries,
		supRange, rec.MaxBytes, rec.GroupID, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

func (d *Database) ListDownloadTasks() ([]domain.DownloadTaskRecord, error) {
	if d == nil {
		return nil, nil
	}
	rows, err := d.readDB.Query(`
		SELECT id, name, category, url, dest_path, temp_path,
		       total_bytes, downloaded_bytes, checksum_algo, checksum_value,
		       integrity_note, status, error_msg, attempts, max_retries,
		       supports_range, max_bytes, group_id, created_at, updated_at
		FROM download_tasks
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.DownloadTaskRecord, 0, 16)
	for rows.Next() {
		var rec domain.DownloadTaskRecord
		var supRange int
		if err := rows.Scan(
			&rec.ID, &rec.Name, &rec.Category, &rec.URL, &rec.DestPath, &rec.TempPath,
			&rec.TotalBytes, &rec.DownloadedBytes, &rec.ChecksumAlgo, &rec.ChecksumValue,
			&rec.IntegrityNote, &rec.Status, &rec.ErrorMsg, &rec.Attempts, &rec.MaxRetries,
			&supRange, &rec.MaxBytes, &rec.GroupID, &rec.CreatedAt, &rec.UpdatedAt,
		); err == nil {
			rec.SupportsRange = supRange != 0
			out = append(out, rec)
		}
	}
	return out, nil
}

func (d *Database) DeleteDownloadTask(id string) error {
	if d == nil || id == "" {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	_, err := d.writeDB.Exec(`DELETE FROM download_tasks WHERE id = ?`, id)
	return err
}

func (d *Database) DeleteDownloadTasksByStatuses(statuses []string) error {
	if d == nil || len(statuses) == 0 {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	args := make([]any, len(statuses))
	for i, s := range statuses {
		args[i] = s
	}
	_, err := d.writeDB.Exec(`DELETE FROM download_tasks WHERE status IN (`+buildInPlaceholders(len(statuses))+`)`, args...)
	return err
}

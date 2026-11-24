package storage

import "modhub/internal/domain"

func (d *Database) UpsertGameSession(rec domain.GameSessionRecord) error {
	if d == nil {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	_, err := d.writeDB.Exec(`
		INSERT INTO game_sessions (
			id, instance_id, instance_name, version_id, mc_version, loader, loader_version,
			account_id, username, java_path, java_version, pid, state, exit_code,
			exit_reason, error_msg, log_file_path, started_at, ended_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			instance_name=excluded.instance_name,
			version_id=excluded.version_id,
			mc_version=excluded.mc_version,
			loader=excluded.loader,
			loader_version=excluded.loader_version,
			account_id=excluded.account_id,
			username=excluded.username,
			java_path=excluded.java_path,
			java_version=excluded.java_version,
			pid=excluded.pid,
			state=excluded.state,
			exit_code=excluded.exit_code,
			exit_reason=excluded.exit_reason,
			error_msg=excluded.error_msg,
			log_file_path=excluded.log_file_path,
			ended_at=excluded.ended_at
	`,
		rec.ID, rec.InstanceID, rec.InstanceName, rec.VersionID, rec.MCVersion, rec.Loader, rec.LoaderVersion,
		rec.AccountID, rec.Username, rec.JavaPath, rec.JavaVersion, rec.PID, rec.State, rec.ExitCode,
		rec.ExitReason, rec.ErrorMsg, rec.LogFilePath, rec.StartedAt, rec.EndedAt,
	)
	return err
}

func (d *Database) ListGameSessions(instanceID string, limit int) ([]domain.GameSessionRecord, error) {
	if d == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
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
			SELECT id, instance_id, instance_name, version_id, mc_version, loader, loader_version,
			       account_id, username, java_path, java_version, pid, state, exit_code,
			       exit_reason, error_msg, log_file_path, started_at, ended_at
			FROM game_sessions
			WHERE instance_id = ?
			ORDER BY started_at DESC
			LIMIT ?
		`, instanceID, limit)
	} else {
		rows, err = d.readDB.Query(`
			SELECT id, instance_id, instance_name, version_id, mc_version, loader, loader_version,
			       account_id, username, java_path, java_version, pid, state, exit_code,
			       exit_reason, error_msg, log_file_path, started_at, ended_at
			FROM game_sessions
			ORDER BY started_at DESC
			LIMIT ?
		`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.GameSessionRecord, 0, 16)
	for rows.Next() {
		var rec domain.GameSessionRecord
		if rows.Scan(
			&rec.ID, &rec.InstanceID, &rec.InstanceName, &rec.VersionID, &rec.MCVersion, &rec.Loader, &rec.LoaderVersion,
			&rec.AccountID, &rec.Username, &rec.JavaPath, &rec.JavaVersion, &rec.PID, &rec.State, &rec.ExitCode,
			&rec.ExitReason, &rec.ErrorMsg, &rec.LogFilePath, &rec.StartedAt, &rec.EndedAt,
		) == nil {
			out = append(out, rec)
		}
	}
	return out, nil
}

package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"modhub/internal/domain"
)

type Migration struct {
	Version int
	Name    string
	Up      func(tx *sql.Tx) error
}

type AppliedMigration struct {
	Version   int
	Name      string
	AppliedAt int64
}

const createSchemaMigrationsSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at INTEGER NOT NULL
);
`

func DefaultMigrations() []Migration {
	return []Migration{
		{Version: 1, Name: "initial_core_tables", Up: mig001InitialCoreTables},
		{Version: 2, Name: "reverse_deps_verified_composite_pk", Up: mig002ReverseDepsVerifiedPK},
		{Version: 3, Name: "catalog_fts5_rowid_sync", Up: mig003CatalogFTS5RowidSync},
		{Version: 4, Name: "reverse_deps_updated_index", Up: mig004ReverseDepsUpdatedIndex},
		{Version: 5, Name: "download_tasks_queue", Up: mig005DownloadTasksQueue},
		{Version: 6, Name: "java_runtimes_registry", Up: mig006JavaRuntimesRegistry},
		{Version: 7, Name: "instances_registry", Up: mig007InstancesRegistry},
		{Version: 8, Name: "installed_versions_registry", Up: mig008InstalledVersionsRegistry},
		{Version: 9, Name: "microsoft_accounts_registry", Up: mig009MicrosoftAccountsRegistry},
		{Version: 10, Name: "game_sessions_history", Up: mig010GameSessionsHistory},
		{Version: 11, Name: "modpacks_manager_tables", Up: mig011ModpacksManagerTables},
		{Version: 12, Name: "principal_covering_indexes", Up: mig012PrincipalCoveringIndexes},
		{Version: 13, Name: "account_type_column", Up: mig013AccountTypeColumn},
	}
}

func mig001InitialCoreTables(tx *sql.Tx) error {
	schema := `
	CREATE TABLE IF NOT EXISTS http_cache (
		cache_key TEXT PRIMARY KEY,
		expires_at INTEGER NOT NULL,
		payload TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_http_cache_exp ON http_cache(expires_at);

	CREATE TABLE IF NOT EXISTS catalog_items (
		source_key TEXT PRIMARY KEY,
		platform TEXT NOT NULL,
		content_type TEXT NOT NULL,
		title TEXT NOT NULL,
		norm_title TEXT NOT NULL,
		downloads INTEGER NOT NULL DEFAULT 0,
		data TEXT NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_cat_type_dl ON catalog_items(content_type, downloads DESC);
	CREATE INDEX IF NOT EXISTS idx_cat_norm ON catalog_items(content_type, norm_title);

	CREATE TABLE IF NOT EXISTS reverse_deps (
		dep_key TEXT NOT NULL,
		dependent_key TEXT NOT NULL,
		dep_type TEXT NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (dep_key, dependent_key, dep_type)
	) WITHOUT ROWID;
	CREATE INDEX IF NOT EXISTS idx_rdeps_dep ON reverse_deps(dep_key, dep_type);

	CREATE TABLE IF NOT EXISTS lookup_log (
		lookup_key TEXT PRIMARY KEY,
		updated_at INTEGER NOT NULL
	);
	`
	_, err := tx.Exec(schema)
	return err
}

func mig002ReverseDepsVerifiedPK(tx *sql.Tx) error {
	var legacyDone int64
	if err := tx.QueryRow(`SELECT updated_at FROM lookup_log WHERE lookup_key = 'schema_mig::rdeps_v2_verified'`).Scan(&legacyDone); err == nil && legacyDone > 0 {
		return nil
	}

	rows, err := tx.Query(`PRAGMA table_info(reverse_deps)`)
	if err != nil {
		return err
	}
	pkCols := 0
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk) == nil && pk > 0 {
			pkCols++
		}
	}
	_ = rows.Close()

	var rowCount int64
	_ = tx.QueryRow(`SELECT COUNT(*) FROM reverse_deps`).Scan(&rowCount)

	if pkCols != 3 || rowCount == 0 {
		if pkCols != 3 {
			if _, err := tx.Exec(`DROP TABLE IF EXISTS reverse_deps`); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS reverse_deps (
				dep_key TEXT NOT NULL,
				dependent_key TEXT NOT NULL,
				dep_type TEXT NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (dep_key, dependent_key, dep_type)
			) WITHOUT ROWID;
			CREATE INDEX IF NOT EXISTS idx_rdeps_dep ON reverse_deps(dep_key, dep_type);
		`); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`INSERT OR REPLACE INTO lookup_log (lookup_key, updated_at) VALUES ('schema_mig::rdeps_v2_verified', ?)`, time.Now().Unix())
	return err
}

func mig003CatalogFTS5RowidSync(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS catalog_fts USING fts5(
			source_key UNINDEXED,
			title,
			summary,
			authors,
			tokenize='unicode61'
		);
	`); err != nil {
		return nil
	}

	var legacyDone int64
	if err := tx.QueryRow(`SELECT updated_at FROM lookup_log WHERE lookup_key = 'schema_mig::fts_rowid_v3'`).Scan(&legacyDone); err == nil && legacyDone > 0 {
		return nil
	}

	if _, err := tx.Exec(`DELETE FROM catalog_fts`); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT rowid, source_key, data FROM catalog_items`)
	if err != nil {
		return err
	}
	type ftsRow struct {
		rid int64
		sk  string
		raw string
	}
	var toIndex []ftsRow
	for rows.Next() {
		var fr ftsRow
		if rows.Scan(&fr.rid, &fr.sk, &fr.raw) == nil {
			toIndex = append(toIndex, fr)
		}
	}
	_ = rows.Close()

	if len(toIndex) > 0 {
		stmt, err := tx.Prepare(`INSERT INTO catalog_fts (rowid, source_key, title, summary, authors) VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, fr := range toIndex {
			var card domain.RawProjectCard
			if json.Unmarshal([]byte(fr.raw), &card) == nil {
				if _, err := stmt.Exec(fr.rid, fr.sk, card.Title, card.Summary, strings.Join(card.Authors, " ")); err != nil {
					return err
				}
			}
		}
	}

	_, err = tx.Exec(`INSERT OR REPLACE INTO lookup_log (lookup_key, updated_at) VALUES ('schema_mig::fts_rowid_v3', ?)`, time.Now().Unix())
	return err
}

func mig004ReverseDepsUpdatedIndex(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_rdeps_updated ON reverse_deps(dep_key, updated_at DESC);`)
	return err
}

func mig005DownloadTasksQueue(tx *sql.Tx) error {
	_, err := tx.Exec(`
	CREATE TABLE IF NOT EXISTS download_tasks (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		category TEXT NOT NULL DEFAULT 'generic',
		url TEXT NOT NULL,
		dest_path TEXT NOT NULL,
		temp_path TEXT NOT NULL,
		total_bytes INTEGER NOT NULL DEFAULT 0,
		downloaded_bytes INTEGER NOT NULL DEFAULT 0,
		checksum_algo TEXT NOT NULL DEFAULT '',
		checksum_value TEXT NOT NULL DEFAULT '',
		integrity_note TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		error_msg TEXT NOT NULL DEFAULT '',
		attempts INTEGER NOT NULL DEFAULT 0,
		max_retries INTEGER NOT NULL DEFAULT 3,
		supports_range INTEGER NOT NULL DEFAULT 0,
		max_bytes INTEGER NOT NULL DEFAULT 0,
		group_id TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_dl_tasks_status_created ON download_tasks(status, created_at ASC);
	CREATE INDEX IF NOT EXISTS idx_dl_tasks_group ON download_tasks(group_id);
	`)
	return err
}

func mig006JavaRuntimesRegistry(tx *sql.Tx) error {
	_, err := tx.Exec(`
	CREATE TABLE IF NOT EXISTS java_runtimes (
		id TEXT PRIMARY KEY,
		component TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL,
		executable_path TEXT NOT NULL UNIQUE,
		root_dir TEXT NOT NULL DEFAULT '',
		version TEXT NOT NULL DEFAULT '',
		major_version INTEGER NOT NULL DEFAULT 0,
		arch TEXT NOT NULL DEFAULT '',
		bitness INTEGER NOT NULL DEFAULT 0,
		vendor TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT 'system',
		managed INTEGER NOT NULL DEFAULT 0,
		verified INTEGER NOT NULL DEFAULT 0,
		installed_at INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_java_runtimes_major ON java_runtimes(major_version DESC, managed DESC);
	`)
	return err
}

func mig007InstancesRegistry(tx *sql.Tx) error {
	_, err := tx.Exec(`
	CREATE TABLE IF NOT EXISTS instances (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		icon TEXT NOT NULL DEFAULT 'grass',
		mc_version TEXT NOT NULL DEFAULT '',
		instance_type TEXT NOT NULL DEFAULT 'empty',
		loader_version TEXT NOT NULL DEFAULT '',
		root_dir TEXT NOT NULL UNIQUE,
		game_dir TEXT NOT NULL,
		java_mode TEXT NOT NULL DEFAULT 'auto',
		java_runtime_id TEXT NOT NULL DEFAULT '',
		java_path TEXT NOT NULL DEFAULT '',
		memory_min_mb INTEGER NOT NULL DEFAULT 512,
		memory_max_mb INTEGER NOT NULL DEFAULT 4096,
		launch_settings_json TEXT NOT NULL DEFAULT '{}',
		linked_pack_id TEXT NOT NULL DEFAULT '',
		linked_pack_name TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'needs_setup',
		created_at INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0,
		last_played_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_instances_updated ON instances(updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_instances_pack ON instances(linked_pack_id);

	CREATE TABLE IF NOT EXISTS instance_pack_links (
		instance_id TEXT NOT NULL,
		pack_id TEXT NOT NULL,
		pack_name TEXT NOT NULL DEFAULT '',
		linked_at INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (instance_id, pack_id)
	) WITHOUT ROWID;
	CREATE INDEX IF NOT EXISTS idx_inst_pack_links_pack ON instance_pack_links(pack_id);
	`)
	return err
}

func mig008InstalledVersionsRegistry(tx *sql.Tx) error {
	_, err := tx.Exec(`
	CREATE TABLE IF NOT EXISTS installed_versions (
		id TEXT PRIMARY KEY,
		instance_id TEXT NOT NULL,
		version_id TEXT NOT NULL,
		mc_version TEXT NOT NULL,
		install_type TEXT NOT NULL DEFAULT 'vanilla',
		modloader TEXT NOT NULL DEFAULT '',
		loader_version TEXT NOT NULL DEFAULT '',
		java_runtime_id TEXT NOT NULL DEFAULT '',
		java_path TEXT NOT NULL DEFAULT '',
		java_major INTEGER NOT NULL DEFAULT 0,
		metadata_path TEXT NOT NULL DEFAULT '',
		installed_at INTEGER NOT NULL DEFAULT 0,
		verified INTEGER NOT NULL DEFAULT 0,
		verified_at INTEGER NOT NULL DEFAULT 0,
		verify_error TEXT NOT NULL DEFAULT '',
		active INTEGER NOT NULL DEFAULT 0
	);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_inst_ver_unique ON installed_versions(instance_id, version_id);
	CREATE INDEX IF NOT EXISTS idx_inst_ver_instance ON installed_versions(instance_id, active DESC, installed_at DESC);
	`)
	return err
}

func mig009MicrosoftAccountsRegistry(tx *sql.Tx) error {
	_, err := tx.Exec(`
	CREATE TABLE IF NOT EXISTS accounts (
		id TEXT PRIMARY KEY,
		microsoft_account_id TEXT NOT NULL DEFAULT '',
		xbox_user_id TEXT NOT NULL DEFAULT '',
		xbox_gamertag TEXT NOT NULL DEFAULT '',
		minecraft_uuid TEXT NOT NULL DEFAULT '',
		minecraft_username TEXT NOT NULL DEFAULT '',
		profile_name TEXT NOT NULL DEFAULT '',
		skin_url TEXT NOT NULL DEFAULT '',
		skin_variant TEXT NOT NULL DEFAULT '',
		cape_url TEXT NOT NULL DEFAULT '',
		cape_alias TEXT NOT NULL DEFAULT '',
		auth_status TEXT NOT NULL DEFAULT 'unauthorized',
		license_status TEXT NOT NULL DEFAULT 'unknown',
		last_auth_at INTEGER NOT NULL DEFAULT 0,
		last_license_check_at INTEGER NOT NULL DEFAULT 0,
		profile_updated_at INTEGER NOT NULL DEFAULT 0,
		ms_expires_at INTEGER NOT NULL DEFAULT 0,
		xbl_expires_at INTEGER NOT NULL DEFAULT 0,
		xsts_expires_at INTEGER NOT NULL DEFAULT 0,
		mc_expires_at INTEGER NOT NULL DEFAULT 0,
		vault_key TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		active INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_accounts_active ON accounts(active DESC, updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_accounts_uuid ON accounts(minecraft_uuid);
	`)
	return err
}

func mig010GameSessionsHistory(tx *sql.Tx) error {
	_, err := tx.Exec(`
	CREATE TABLE IF NOT EXISTS game_sessions (
		id TEXT PRIMARY KEY,
		instance_id TEXT NOT NULL,
		instance_name TEXT NOT NULL DEFAULT '',
		version_id TEXT NOT NULL DEFAULT '',
		mc_version TEXT NOT NULL DEFAULT '',
		loader TEXT NOT NULL DEFAULT '',
		loader_version TEXT NOT NULL DEFAULT '',
		account_id TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		java_path TEXT NOT NULL DEFAULT '',
		java_version TEXT NOT NULL DEFAULT '',
		pid INTEGER NOT NULL DEFAULT 0,
		state TEXT NOT NULL DEFAULT 'exited',
		exit_code INTEGER NOT NULL DEFAULT 0,
		exit_reason TEXT NOT NULL DEFAULT '',
		error_msg TEXT NOT NULL DEFAULT '',
		log_file_path TEXT NOT NULL DEFAULT '',
		started_at INTEGER NOT NULL DEFAULT 0,
		ended_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_game_sessions_inst ON game_sessions(instance_id, started_at DESC);
	CREATE INDEX IF NOT EXISTS idx_game_sessions_state ON game_sessions(state, started_at DESC);
	`)
	return err
}

func mig011ModpacksManagerTables(tx *sql.Tx) error {
	_, err := tx.Exec(`
	CREATE TABLE IF NOT EXISTS modpacks (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		author TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		version TEXT NOT NULL DEFAULT '',
		format TEXT NOT NULL DEFAULT 'modhub',
		source_type TEXT NOT NULL DEFAULT 'file',
		source_ref TEXT NOT NULL DEFAULT '',
		source_project_id TEXT NOT NULL DEFAULT '',
		source_version_id TEXT NOT NULL DEFAULT '',
		mc_version TEXT NOT NULL DEFAULT '',
		loader TEXT NOT NULL DEFAULT '',
		loader_version TEXT NOT NULL DEFAULT '',
		instance_id TEXT NOT NULL DEFAULT '',
		manifest_hash TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'installed',
		last_error TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0,
		installed_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_modpacks_instance ON modpacks(instance_id);
	CREATE INDEX IF NOT EXISTS idx_modpacks_updated ON modpacks(updated_at DESC);

	CREATE TABLE IF NOT EXISTS modpack_files (
		modpack_id TEXT NOT NULL,
		instance_id TEXT NOT NULL DEFAULT '',
		rel_path TEXT NOT NULL,
		category TEXT NOT NULL DEFAULT 'mod',
		source_platform TEXT NOT NULL DEFAULT '',
		source_project_id TEXT NOT NULL DEFAULT '',
		source_file_id TEXT NOT NULL DEFAULT '',
		sha1 TEXT NOT NULL DEFAULT '',
		sha256 TEXT NOT NULL DEFAULT '',
		sha512 TEXT NOT NULL DEFAULT '',
		size INTEGER NOT NULL DEFAULT 0,
		managed INTEGER NOT NULL DEFAULT 1,
		optional INTEGER NOT NULL DEFAULT 0,
		enabled INTEGER NOT NULL DEFAULT 1,
		download_url TEXT NOT NULL DEFAULT '',
		updated_at INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (modpack_id, rel_path)
	) WITHOUT ROWID;
	CREATE INDEX IF NOT EXISTS idx_modpack_files_inst ON modpack_files(instance_id);

	CREATE TABLE IF NOT EXISTS modpack_updates (
		id TEXT PRIMARY KEY,
		modpack_id TEXT NOT NULL,
		from_version TEXT NOT NULL DEFAULT '',
		to_version TEXT NOT NULL DEFAULT '',
		added_count INTEGER NOT NULL DEFAULT 0,
		updated_count INTEGER NOT NULL DEFAULT 0,
		removed_count INTEGER NOT NULL DEFAULT 0,
		preserved_count INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'completed',
		summary TEXT NOT NULL DEFAULT '',
		applied_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_modpack_updates_mp ON modpack_updates(modpack_id, applied_at DESC);
	`)
	return err
}

func mig012PrincipalCoveringIndexes(tx *sql.Tx) error {
	_, err := tx.Exec(`
	CREATE INDEX IF NOT EXISTS idx_modpack_files_managed ON modpack_files(modpack_id, managed);
	CREATE INDEX IF NOT EXISTS idx_game_sessions_state_pid ON game_sessions(state, pid);
	CREATE INDEX IF NOT EXISTS idx_download_tasks_status_created ON download_tasks(status, created_at DESC);
	`)
	return err
}

func mig013AccountTypeColumn(tx *sql.Tx) error {
	_, err := tx.Exec(`
	ALTER TABLE accounts ADD COLUMN account_type TEXT NOT NULL DEFAULT 'microsoft';
	CREATE INDEX IF NOT EXISTS idx_accounts_type ON accounts(account_type);
	`)
	return err
}

func RunMigrations(db *sql.DB) (int, error) {
	return RunMigrationsWith(db, DefaultMigrations())
}

func RunMigrationsWith(db *sql.DB, migrations []Migration) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("nil database handle")
	}
	if _, err := db.Exec(createSchemaMigrationsSQL); err != nil {
		return 0, fmt.Errorf("init schema_migrations: %w", err)
	}

	sorted := append([]Migration(nil), migrations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })
	seenVer := make(map[int]struct{}, len(sorted))
	for _, m := range sorted {
		if m.Version <= 0 {
			return 0, fmt.Errorf("invalid migration version %d (%s)", m.Version, m.Name)
		}
		if _, dup := seenVer[m.Version]; dup {
			return 0, fmt.Errorf("duplicate migration version %d", m.Version)
		}
		seenVer[m.Version] = struct{}{}
	}

	applied := map[int]bool{}
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return 0, fmt.Errorf("query schema_migrations: %w", err)
	}
	currentVer := 0
	for rows.Next() {
		var v int
		if rows.Scan(&v) == nil {
			applied[v] = true
			if v > currentVer {
				currentVer = v
			}
		}
	}
	_ = rows.Close()

	for _, m := range sorted {
		if applied[m.Version] {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return currentVer, fmt.Errorf("begin migration %d (%s): %w", m.Version, m.Name, err)
		}
		if m.Up != nil {
			if err := m.Up(tx); err != nil {
				_ = tx.Rollback()
				return currentVer, fmt.Errorf("migration %d (%s) failed: %w", m.Version, m.Name, err)
			}
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
			m.Version, m.Name, time.Now().Unix(),
		); err != nil {
			_ = tx.Rollback()
			return currentVer, fmt.Errorf("record migration %d (%s): %w", m.Version, m.Name, err)
		}
		if err := tx.Commit(); err != nil {
			return currentVer, fmt.Errorf("commit migration %d (%s): %w", m.Version, m.Name, err)
		}
		currentVer = m.Version
	}
	return currentVer, nil
}

func GetSchemaVersion(db *sql.DB) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("nil database handle")
	}
	var v sql.NullInt64
	err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	if err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

func ListAppliedMigrations(db *sql.DB) ([]AppliedMigration, error) {
	if db == nil {
		return nil, fmt.Errorf("nil database handle")
	}
	rows, err := db.Query(`SELECT version, name, applied_at FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppliedMigration
	for rows.Next() {
		var am AppliedMigration
		if rows.Scan(&am.Version, &am.Name, &am.AppliedAt) == nil {
			out = append(out, am)
		}
	}
	return out, nil
}

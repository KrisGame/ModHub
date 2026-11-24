package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"modhub/internal/domain"
)

func TestMigrationsFreshIdempotentAndSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "mig_test.db")

	db, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	wantVer := len(DefaultMigrations())
	if got := db.SchemaVersion(); got != wantVer {
		t.Fatalf("expected schema version %d, got %d", wantVer, got)
	}
	applied, err := db.AppliedMigrations()
	if err != nil || len(applied) != wantVer {
		t.Fatalf("expected %d applied migrations, got %+v (err=%v)", wantVer, applied, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db2, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("reopen OpenDatabase: %v", err)
	}
	defer db2.Close()

	applied2, err := db2.AppliedMigrations()
	if err != nil || len(applied2) != wantVer {
		t.Fatalf("reopen applied migrations mismatch: %+v (err=%v)", applied2, err)
	}
	for i := range applied {
		if applied[i].Version != applied2[i].Version || applied[i].AppliedAt != applied2[i].AppliedAt {
			t.Fatalf("migration %d re-executed on reopen", applied[i].Version)
		}
	}
}

func TestMigrationsLegacyDatabaseUpgradePreservesAllData(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	legacySQL := `
	CREATE TABLE http_cache (cache_key TEXT PRIMARY KEY, expires_at INTEGER NOT NULL, payload TEXT NOT NULL);
	CREATE TABLE catalog_items (source_key TEXT PRIMARY KEY, platform TEXT NOT NULL, content_type TEXT NOT NULL, title TEXT NOT NULL, norm_title TEXT NOT NULL, downloads INTEGER NOT NULL DEFAULT 0, data TEXT NOT NULL, updated_at INTEGER NOT NULL);
	CREATE TABLE reverse_deps (dep_key TEXT NOT NULL, dependent_key TEXT NOT NULL, dep_type TEXT NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY (dep_key, dependent_key, dep_type)) WITHOUT ROWID;
	CREATE TABLE lookup_log (lookup_key TEXT PRIMARY KEY, updated_at INTEGER NOT NULL);
	INSERT INTO http_cache VALUES ('k1', 4102444800, '{"ok":true}');
	INSERT INTO catalog_items VALUES ('modrinth:sod', 'modrinth', 'mod', 'Sodium', 'sodium', 50000000, '{"platform":"modrinth","source_key":"modrinth:sod","id":"sod","content_type":"mod","title":"Sodium","downloads":50000000}', 1700000000);
	INSERT INTO reverse_deps VALUES ('modrinth:jei', 'modrinth:jer', 'required', 1700000000);
	INSERT INTO lookup_log VALUES ('schema_mig::rdeps_v2_verified', 1700000000);
	`
	if _, err := rawDB.Exec(legacySQL); err != nil {
		t.Fatalf("seed legacy DB: %v", err)
	}
	_ = rawDB.Close()

	db, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase on legacy DB: %v", err)
	}
	defer db.Close()

	if v, ok := db.CacheGet("k1"); !ok || v != `{"ok":true}` {
		t.Fatalf("http_cache lost during legacy migration: ok=%v val=%q", ok, v)
	}
	if card, ok := db.GetCatalogItem("modrinth:sod"); !ok || card.Title != "Sodium" {
		t.Fatalf("catalog_items lost during legacy migration: ok=%v card=%+v", ok, card)
	}
	if edges := db.GetReverseDependents([]string{"modrinth:jei"}, []string{"required"}, 10); len(edges) != 1 || edges[0].DependentKey != "modrinth:jer" {
		t.Fatalf("reverse_deps lost during legacy migration: %+v", edges)
	}
	if hits := db.FindSourceKeysByQuery("Sodium", 5); len(hits) != 1 || hits[0] != "modrinth:sod" {
		t.Fatalf("FTS5 index not populated from legacy catalog_items: %+v", hits)
	}
}

func TestMigrationsRollbackOnFailure(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := sql.Open("sqlite", filepath.Join(dir, "rollback.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer sqlDB.Close()

	migs := []Migration{
		{
			Version: 1,
			Name:    "create_good",
			Up: func(tx *sql.Tx) error {
				_, err := tx.Exec(`CREATE TABLE good_tbl (id INTEGER PRIMARY KEY)`)
				return err
			},
		},
		{
			Version: 2,
			Name:    "failing_step",
			Up: func(tx *sql.Tx) error {
				if _, err := tx.Exec(`CREATE TABLE partial_tbl (id INTEGER PRIMARY KEY)`); err != nil {
					return err
				}
				return errors.New("simulated migration error")
			},
		},
	}

	ver, err := RunMigrationsWith(sqlDB, migs)
	if err == nil {
		t.Fatal("expected migration 2 to fail")
	}
	if ver != 1 {
		t.Fatalf("expected version 1 after rollback, got %d", ver)
	}
	if gotVer, err := GetSchemaVersion(sqlDB); err != nil || gotVer != 1 {
		t.Fatalf("GetSchemaVersion = (%d, %v), want (1, nil)", gotVer, err)
	}
	var name string
	if err := sqlDB.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='partial_tbl'`).Scan(&name); err == nil {
		t.Fatalf("rolled-back table partial_tbl should not exist")
	}
}

func TestDatabaseCacheClearAndFTSAndReverseDeps(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cache_fts.db")
	db, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}

	for i := 0; i < 20; i++ {
		db.CacheSet("k_"+string(rune('a'+i)), `{"v":1}`, 300)
	}
	db.CacheClear()
	time.Sleep(220 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if _, ok := db.CacheGet("k_" + string(rune('a'+i))); ok {
			t.Fatalf("stale cached key resurrected after CacheClear")
		}
	}

	db.UpsertCatalogItems([]domain.RawProjectCard{{
		Platform: "modrinth", SourceKey: "modrinth:abc", ID: "abc", ContentType: "mod",
		Title: "OldNameAlpha", Summary: "Initial", Authors: []string{"dev1"}, Downloads: 100,
	}})
	db.UpsertCatalogItems([]domain.RawProjectCard{{
		Platform: "modrinth", SourceKey: "modrinth:abc", ID: "abc", ContentType: "mod",
		Title: "NewNameOmega", Summary: "Updated", Authors: []string{"dev1"}, Downloads: 500,
	}})
	if hitsOld := db.FindSourceKeysByQuery("OldNameAlpha", 5); len(hitsOld) != 0 {
		t.Fatalf("stale FTS entry survived update: %+v", hitsOld)
	}
	if hitsNew := db.FindSourceKeysByQuery("NewNameOmega", 5); len(hitsNew) != 1 || hitsNew[0] != "modrinth:abc" {
		t.Fatalf("expected NewNameOmega match, got %+v", hitsNew)
	}

	edges := make([]domain.RDepEdge, 0, 125)
	for i := 0; i < 120; i++ {
		edges = append(edges, domain.RDepEdge{DepKey: "curseforge:238222", DependentKey: "curseforge:" + strconv.Itoa(1000+i), DepType: "optional"})
	}
	for i := 0; i < 5; i++ {
		edges = append(edges, domain.RDepEdge{DepKey: "curseforge:238222", DependentKey: "curseforge:" + strconv.Itoa(9000+i), DepType: "required"})
	}
	db.UpsertReverseDeps(edges)
	if reqEdges := db.GetReverseDependents([]string{"curseforge:238222"}, []string{"required"}, 20); len(reqEdges) != 5 {
		t.Fatalf("expected 5 required edges, got %d", len(reqEdges))
	}

	oldTS := time.Now().Unix() - 200000
	_, _ = db.writeDB.Exec(`UPDATE lookup_log SET updated_at = ? WHERE lookup_key LIKE 'schema_mig::%'`, oldTS)
	db.CleanupExpiredCache()
	_ = db.Close()

	db2, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	if reqEdges := db2.GetReverseDependents([]string{"curseforge:238222"}, []string{"required"}, 20); len(reqEdges) != 5 {
		t.Fatalf("reverse_deps lost after CleanupExpiredCache + reopen: got %d", len(reqEdges))
	}
}

func TestSingleFlightRecoversFromPanic(t *testing.T) {
	sf := NewSingleFlight[string]()
	_, err := sf.DoCtx(context.Background(), "panic-key", func() (string, error) { panic("boom") })
	if err == nil || !strings.Contains(err.Error(), "singleflight panic") {
		t.Fatalf("expected recovered panic error, got: %v", err)
	}
	val, err := sf.DoCtx(context.Background(), "panic-key", func() (string, error) { return "ok", nil })
	if err != nil || val != "ok" {
		t.Fatalf("expected recovery cleanup, got val=%q err=%v", val, err)
	}
}

func TestMigrationV12AndModpackBatchStats(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "v12.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer db.Close()

	if v := db.SchemaVersion(); v < 12 {
		t.Fatalf("expected schema version >= 12, got %d", v)
	}
	_ = db.UpsertModpack(domain.ModpackRecord{ID: "mp1", Name: "Pack1", Format: "modhub"})
	_ = db.ReplaceModpackFiles("mp1", []domain.ModpackFileRecord{
		{ModpackID: "mp1", RelPath: "mods/a.jar", Managed: true, Enabled: true},
		{ModpackID: "mp1", RelPath: "config/b.toml", Managed: false, Enabled: true},
	})
	stats, err := db.ListModpackFileStats()
	if err != nil {
		t.Fatalf("ListModpackFileStats: %v", err)
	}
	if st := stats["mp1"]; st[0] != 2 || st[1] != 1 {
		t.Fatalf("expected (total=2, managed=1), got %+v", st)
	}
}

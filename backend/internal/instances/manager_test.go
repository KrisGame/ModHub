package instances

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"modhub/internal/domain"
	"modhub/internal/events"
	"modhub/internal/storage"
)

func setupTestInstanceManager(t *testing.T, packs []domain.SavedPack) (*Manager, *storage.Database, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "inst_test.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.NewBus()
	t.Cleanup(func() { bus.Close() })

	packFn := func() []domain.SavedPack { return packs }
	mgr := NewManager(
		dir,
		filepath.Join(dir, "instances"),
		filepath.Join(dir, "shared"),
		filepath.Join(dir, "runtimes"),
		filepath.Join(dir, "media_cache"),
		db,
		nil,
		bus,
		packFn,
	)
	return mgr, db, dir
}

// Covers scenarios 1, 2, 3, 15: create multiple profiles, isolation, edit metadata, ModHub pack linking
func TestCreateIsolationEditAndPackLinking(t *testing.T) {
	packs := []domain.SavedPack{{
		ID: "pack_rpg_1", Name: "RPG 1.21.1", MCVersion: "1.21.1", Loader: "neoforge",
		Items: []domain.CartItem{{Title: "Sodium", Folder: "mods", Filename: "sodium.jar"}},
	}}
	mgr, _, _ := setupTestInstanceManager(t, packs)

	p1, err := mgr.Create(CreateInstanceRequest{
		Name: "RPG Profile", Description: "Fantasy", MCVersion: "1.21.1",
		InstanceType: TypeNeoForge, LoaderVersion: "21.1.77", MemoryMaxMB: 6144, LinkedPackID: "pack_rpg_1",
	})
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	if p1.Status != StatusNeedsSetup {
		t.Fatalf("expected honest needs_setup status without installed MC client, got %s", p1.Status)
	}
	if p1.LinkedPackID != "pack_rpg_1" || p1.LinkedPackName != "RPG 1.21.1" || p1.LinkedPackItems != 1 {
		t.Fatalf("linked pack metadata mismatch: %+v", p1)
	}

	p2, err := mgr.Create(CreateInstanceRequest{Name: "Empty Sandbox"})
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	if p1.RootDir == p2.RootDir || p1.GameDir == p2.GameDir {
		t.Fatal("profiles must have strictly independent directories")
	}

	// Writing a mod into p1 MUST NOT appear in p2
	_ = os.WriteFile(filepath.Join(p1.GameDir, "mods", "alpha.jar"), []byte("JAR1"), 0o644)
	g1, _ := mgr.Get(p1.ID)
	g2, _ := mgr.Get(p2.ID)
	if g1.ModsCount != 1 || g2.ModsCount != 0 {
		t.Fatalf("isolation failed: p1=%d mods, p2=%d mods", g1.ModsCount, g2.ModsCount)
	}

	// 3: Edit metadata
	newName := "RPG Extended"
	newMem := 8192
	upd, err := mgr.Update(UpdateInstanceRequest{ID: p1.ID, Name: &newName, MemoryMaxMB: &newMem})
	if err != nil || upd.Name != newName || upd.MemoryMaxMB != 8192 {
		t.Fatalf("Update failed: upd=%+v err=%v", upd, err)
	}
}

// Covers scenarios 4, 5, 6: duplicate empty profile, duplicate with files, selective copying & independence
func TestDuplicateEmptyAndSelectiveCopy(t *testing.T) {
	mgr, _, _ := setupTestInstanceManager(t, nil)

	emptyProf, err := mgr.Create(CreateInstanceRequest{Name: "Blank"})
	if err != nil {
		t.Fatalf("Create blank: %v", err)
	}
	dupEmpty, err := mgr.Duplicate(DuplicateInstanceRequest{SourceID: emptyProf.ID, NewName: "Blank Copy"})
	if err != nil || dupEmpty.ID == emptyProf.ID || dupEmpty.RootDir == emptyProf.RootDir {
		t.Fatalf("Duplicate empty failed: %+v err=%v", dupEmpty, err)
	}

	// Seed files in mods, config, saves, resourcepacks, shaderpacks
	src, _ := mgr.Create(CreateInstanceRequest{Name: "Source", MCVersion: "1.20.1", InstanceType: TypeForge, MemoryMaxMB: 5120})
	_ = os.WriteFile(filepath.Join(src.GameDir, "mods", "m1.jar"), []byte("MOD-ORIGINAL"), 0o644)
	_ = os.WriteFile(filepath.Join(src.GameDir, "config", "c1.toml"), []byte("cfg=true"), 0o644)
	_ = os.WriteFile(filepath.Join(src.GameDir, "saves", "world.dat"), []byte("SAVE-DATA"), 0o644)
	_ = os.WriteFile(filepath.Join(src.GameDir, "resourcepacks", "rp.zip"), []byte("RP"), 0o644)
	_ = os.WriteFile(filepath.Join(src.GameDir, "shaderpacks", "sh.zip"), []byte("SH"), 0o644)

	// Selective copy: mods + configs + settings ONLY (NO saves, resourcepacks, shaderpacks)
	dupSel, err := mgr.Duplicate(DuplicateInstanceRequest{
		SourceID: src.ID, NewName: "Selective Copy",
		CopySettings: true, CopyMods: true, CopyConfigs: true,
		CopyResourcePacks: false, CopyShaderPacks: false, CopySaves: false,
	})
	if err != nil {
		t.Fatalf("Duplicate selective: %v", err)
	}
	if dupSel.MemoryMaxMB != 5120 {
		t.Fatalf("settings not copied: MemoryMaxMB=%d", dupSel.MemoryMaxMB)
	}
	if _, err := os.Stat(filepath.Join(dupSel.GameDir, "mods", "m1.jar")); err != nil {
		t.Fatal("expected mods copied")
	}
	if _, err := os.Stat(filepath.Join(dupSel.GameDir, "config", "c1.toml")); err != nil {
		t.Fatal("expected config copied")
	}
	if _, err := os.Stat(filepath.Join(dupSel.GameDir, "saves", "world.dat")); !os.IsNotExist(err) {
		t.Fatal("saves should NOT have been copied")
	}
	if _, err := os.Stat(filepath.Join(dupSel.GameDir, "resourcepacks", "rp.zip")); !os.IsNotExist(err) {
		t.Fatal("resourcepacks should NOT have been copied")
	}

	// Mutating duplicated mod MUST NOT affect source mod!
	_ = os.WriteFile(filepath.Join(dupSel.GameDir, "mods", "m1.jar"), []byte("MOD-EDITED"), 0o644)
	srcBytes, _ := os.ReadFile(filepath.Join(src.GameDir, "mods", "m1.jar"))
	if string(srcBytes) != "MOD-ORIGINAL" {
		t.Fatalf("source mod mutated via duplicate: %q", string(srcBytes))
	}
}

// Covers scenarios 7, 8, 9: delete record only, delete with files, protect shared libraries & Java runtimes
func TestDeleteModesAndSharedProtection(t *testing.T) {
	mgr, _, dir := setupTestInstanceManager(t, nil)

	// Seed shared library & Java runtime sentinel files
	sharedLib := filepath.Join(dir, "shared", "libraries", "keep.jar")
	javaBin := filepath.Join(dir, "runtimes", "keep_java.txt")
	_ = os.WriteFile(sharedLib, []byte("SHARED"), 0o644)
	_ = os.WriteFile(javaBin, []byte("JAVA"), 0o644)

	// 7: Delete DB record ONLY -> files stay on disk!
	pKeep, _ := mgr.Create(CreateInstanceRequest{Name: "KeepFiles"})
	_ = os.WriteFile(filepath.Join(pKeep.GameDir, "saves", "myworld.dat"), []byte("WORLD"), 0o644)
	if err := mgr.Delete(DeleteInstanceRequest{ID: pKeep.ID, DeleteFiles: false}); err != nil {
		t.Fatalf("Delete record only: %v", err)
	}
	if _, err := mgr.Get(pKeep.ID); !errors.Is(err, ErrInstanceNotFound) {
		t.Fatal("expected deleted profile absent from manager")
	}
	if _, err := os.Stat(filepath.Join(pKeep.GameDir, "saves", "myworld.dat")); err != nil {
		t.Fatal("files should have been preserved when DeleteFiles=false")
	}

	// 8 & 9: Delete profile WITH files -> profile dir removed, shared libs & Java untouched!
	pDel, _ := mgr.Create(CreateInstanceRequest{Name: "DeleteAll"})
	if err := mgr.Delete(DeleteInstanceRequest{ID: pDel.ID, DeleteFiles: true}); err != nil {
		t.Fatalf("Delete with files: %v", err)
	}
	if _, err := os.Stat(pDel.RootDir); !os.IsNotExist(err) {
		t.Fatal("expected profile dir removed when DeleteFiles=true")
	}
	if _, err := os.Stat(sharedLib); err != nil {
		t.Fatal("shared library was touched by instance deletion!")
	}
	if _, err := os.Stat(javaBin); err != nil {
		t.Fatal("shared Java runtime was touched by instance deletion!")
	}
}

// Covers scenarios 10, 11, 12, 13, 14: integrity check, interrupted creation recovery, restart reload, ResolveOpenDir, invalid paths
func TestIntegrityRecoveryRestartAndPathSafety(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "recover.db")
	db1, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}

	mgr1 := NewManager(dir, filepath.Join(dir, "instances"), filepath.Join(dir, "shared"), filepath.Join(dir, "runtimes"), filepath.Join(dir, "media_cache"), db1, nil, nil, nil)
	pGood, err := mgr1.Create(CreateInstanceRequest{Name: "Healthy", MCVersion: "1.21.1", InstanceType: TypeFabric})
	if err != nil {
		t.Fatalf("Create pGood: %v", err)
	}

	// 14: Path traversal & dangerous custom paths MUST be rejected
	for _, badDir := range []string{filepath.Join(dir, "shared"), filepath.Join(dir, "runtimes"), "/"} {
		if _, err := mgr1.Create(CreateInstanceRequest{Name: "BadPath", CustomDir: badDir}); err == nil {
			t.Errorf("expected dangerous custom path %q rejected", badDir)
		}
	}
	if _, err := mgr1.ResolveOpenDir(pGood.ID, "../secret"); err == nil {
		t.Fatal("expected subdir traversal rejected in ResolveOpenDir")
	}
	if openPath, err := mgr1.ResolveOpenDir(pGood.ID, "mods"); err != nil || !strings.HasSuffix(openPath, "mods") {
		t.Fatalf("expected ResolveOpenDir(mods) to succeed, got (%q, %v)", openPath, err)
	}

	// 10: Corrupt instance.json -> Verify MUST report StatusCorrupted, Repair fixes it back to StatusNeedsSetup
	_ = os.WriteFile(filepath.Join(pGood.RootDir, "instance.json"), []byte("{corrupt"), 0o644)
	repBad, err := mgr1.Verify(pGood.ID)
	if err != nil || repBad.Status != StatusCorrupted || repBad.ManifestValid {
		t.Fatalf("expected corrupted status on broken manifest, got %+v err=%v", repBad, err)
	}
	repFixed, err := mgr1.Repair(pGood.ID)
	if err != nil || repFixed.Status != StatusNeedsSetup || !repFixed.ManifestValid {
		t.Fatalf("expected Repair to restore manifest & needs_setup, got %+v err=%v", repFixed, err)
	}

	// 11: Simulate interrupted creation left in SQLite as StatusCreating
	now := time.Now().Unix()
	_ = db1.UpsertInstance(domain.InstanceRecord{
		ID: "inst_interrupted_1", Name: "Interrupted", RootDir: filepath.Join(dir, "instances", "inst_interrupted_1"),
		GameDir: filepath.Join(dir, "instances", "inst_interrupted_1", "minecraft"), Status: string(StatusCreating),
		CreatedAt: now, UpdatedAt: now,
	})
	_ = db1.Close()

	// 12: Reopen DB + Manager -> both profiles loaded; interrupted creation flagged corrupted (never auto-deleted)
	db2, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("reopen DB: %v", err)
	}
	defer db2.Close()
	mgr2 := NewManager(dir, filepath.Join(dir, "instances"), filepath.Join(dir, "shared"), filepath.Join(dir, "runtimes"), filepath.Join(dir, "media_cache"), db2, nil, nil, nil)
	snap := mgr2.List()
	if len(snap.Instances) != 2 {
		t.Fatalf("expected 2 restored profiles after restart, got %d", len(snap.Instances))
	}
	intProf, err := mgr2.Get("inst_interrupted_1")
	if err != nil || intProf.Status != StatusCorrupted {
		t.Fatalf("expected interrupted creation to be flagged corrupted, got %+v err=%v", intProf, err)
	}
}

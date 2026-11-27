package modpacks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"modhub/internal/domain"
	"modhub/internal/instances"
)

func TestApplyUpdateFailureKeepsFilesAndVersion(t *testing.T) {
	m, im, db, dir := setupTestModpackEnv(t, nil, "", nil)
	inst, err := im.Create(instances.CreateInstanceRequest{Name: "update test", MCVersion: "1.21.1"})
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(inst.GameDir, "mods", "old.jar")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	hashes, _ := ComputeFileHashes(oldPath)
	base := domain.ModpackRecord{ID: "update-test", InstanceID: inst.ID, Name: "Test", Version: "1", ManifestHash: "old", Status: "installed", CreatedAt: time.Now().Unix()}
	if err := db.UpsertModpack(base); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceModpackFiles(base.ID, []domain.ModpackFileRecord{{ModpackID: base.ID, InstanceID: inst.ID, RelPath: "mods/old.jar", SHA256: hashes.SHA256, Managed: true}}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "update.zip")
	writeTestZIP(t, archive, map[string][]byte{"new.jar": []byte("new")})
	previewID := "failure-plan"
	m.updatePlans[previewID] = cachedUpdatePlan{createdAt: time.Now(), diff: UpdateDiffPreview{ModpackID: base.ID, CurrentVersion: "1", Removed: []UpdateDiffEntry{{RelPath: "mods/old.jar"}}, Added: []UpdateDiffEntry{{RelPath: "mods/new.jar"}}}, newPrev: ImportPreview{Version: "2", ManifestHash: "new", ResolvedArchivePath: archive, Files: []ModpackFilePlan{{RelPath: "mods/new.jar", ArchiveEntry: "missing.jar", Available: true}}}}
	if _, err := m.ApplyUpdate(context.Background(), ApplyUpdateRequest{ModpackID: base.ID, PreviewID: previewID}); err == nil {
		t.Fatal("missing archive entry unexpectedly succeeded")
	}
	if b, err := os.ReadFile(oldPath); err != nil || string(b) != "old" {
		t.Fatalf("old file lost: %q %v", b, err)
	}
	assertUnchanged := func() {
		t.Helper()
		recs, err := db.ListModpacks()
		if err != nil || len(recs) != 1 || recs[0].Version != "1" {
			t.Fatalf("version advanced: %+v %v", recs, err)
		}
		files, err := db.ListModpackFiles(base.ID)
		if err != nil || len(files) != 1 || files[0].RelPath != "mods/old.jar" {
			t.Fatalf("inventory lost: %+v %v", files, err)
		}
		history, err := db.ListModpackUpdates(base.ID)
		if err != nil || len(history) != 0 {
			t.Fatalf("false update history: %+v %v", history, err)
		}
	}
	assertUnchanged()
	m.updatePlans[previewID] = cachedUpdatePlan{createdAt: time.Now(), diff: UpdateDiffPreview{ModpackID: base.ID, CurrentVersion: "1", Removed: []UpdateDiffEntry{{RelPath: "mods/old.jar"}}, Added: []UpdateDiffEntry{{RelPath: "mods/new.jar"}}}, newPrev: ImportPreview{Version: "2", ManifestHash: "new", ResolvedArchivePath: archive, Files: []ModpackFilePlan{{RelPath: "mods/new.jar", ArchiveEntry: "new.jar", Available: true}}}}
	if err := os.WriteFile(oldPath, []byte("user changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ApplyUpdate(context.Background(), ApplyUpdateRequest{ModpackID: base.ID, PreviewID: previewID}); err == nil {
		t.Fatal("user edit overwritten")
	}
	if b, _ := os.ReadFile(oldPath); !strings.EqualFold(string(b), "user changed") {
		t.Fatalf("user edit lost: %q", b)
	}
	assertUnchanged()
}

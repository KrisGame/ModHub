package instances

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"modhub/internal/domain"
)

func TestSanitizeModFilename(t *testing.T) {
	tests := []struct {
		name          string
		allowDisabled bool
		wantErr       bool
	}{
		{"good.jar", false, false},
		{"good.jar.disabled", true, false},
		{"..\\evil.jar", true, true},
		{"sub/evil.jar", true, true},
		{"C:\\evil.jar", true, true},
		{"evil.txt", true, true},
		{"UPPER.JAR", false, true},
		{"", false, true},
		{strings.Repeat("a", 197) + ".jar", false, true},
		{"bad\x00name.jar", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sanitizeModFilename(tt.name, tt.allowDisabled)
			if (err != nil) != tt.wantErr {
				t.Fatalf("sanitizeModFilename(%q): err=%v, wantErr=%v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestValidateModTargetContainment(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mods")
	if err := validateModTarget(dir, filepath.Join(dir, "good.jar")); err != nil {
		t.Fatalf("valid target rejected: %v", err)
	}
	if err := validateModTarget(dir, filepath.Join(dir, "..", "escape.jar")); err == nil {
		t.Fatal("expected traversal target rejection")
	}
}

func setupModsService(t *testing.T) (*Manager, *Mods, Instance) {
	t.Helper()
	mgr, _, _ := setupTestInstanceManager(t, []domain.SavedPack{})
	inst, err := mgr.Create(CreateInstanceRequest{Name: "Mods test"})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	return mgr, NewMods(mgr, nil, nil), inst
}

func TestModsToggleRoundTripAndDelete(t *testing.T) {
	mgr, mods, inst := setupModsService(t)
	jar := filepath.Join(inst.GameDir, "mods", "sample.jar")
	if err := os.WriteFile(jar, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := mods.SetEnabled(inst.ID, "sample.jar", false)
	if err != nil || len(list) != 1 || list[0].Enabled || list[0].Filename != "sample.jar.disabled" {
		t.Fatalf("disable: list=%+v err=%v", list, err)
	}
	if _, err := mods.SetEnabled(inst.ID, "sample.jar.disabled", false); err != nil {
		t.Fatalf("disable idempotently: %v", err)
	}
	list, err = mods.SetEnabled(inst.ID, "sample.jar.disabled", true)
	if err != nil || len(list) != 1 || !list[0].Enabled || list[0].Filename != "sample.jar" {
		t.Fatalf("enable: list=%+v err=%v", list, err)
	}
	if _, err := mods.Delete(inst.ID, "sample.jar"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err := mgr.Get(inst.ID)
	if err != nil || got.ModsCount != 0 {
		t.Fatalf("refresh after delete: count=%d err=%v", got.ModsCount, err)
	}
	if _, err := mods.Delete(inst.ID, "missing.jar"); err == nil || !strings.Contains(err.Error(), "файл не найден") {
		t.Fatalf("expected clear missing-file error, got %v", err)
	}
}

func TestModsRejectTraversalAndBusyProfile(t *testing.T) {
	mgr, mods, inst := setupModsService(t)
	for _, name := range []string{"..\\evil.jar", "sub/evil.jar", "C:\\evil.jar", "evil.txt"} {
		if _, err := mods.Delete(inst.ID, name); err == nil {
			t.Errorf("expected filename rejection for %q", name)
		}
	}
	if err := mgr.AcquireBusy(inst.ID); err != nil {
		t.Fatal(err)
	}
	defer mgr.ReleaseBusy(inst.ID)
	if _, err := mods.SetEnabled(inst.ID, "valid.jar", false); err != ErrInstanceBusy {
		t.Fatalf("expected ErrInstanceBusy, got %v", err)
	}
}

func TestModsInstallValidationAndExistingDestination(t *testing.T) {
	_, mods, inst := setupModsService(t)
	if _, err := mods.Install(InstallModRequest{InstanceID: inst.ID, Filename: "bad.txt", URL: "https://example.com/mod"}); err == nil {
		t.Fatal("expected non-jar filename rejection")
	}
	if _, err := mods.Install(InstallModRequest{InstanceID: inst.ID, Filename: "valid.jar"}); err == nil {
		t.Fatal("expected missing URL rejection")
	}
	jar := filepath.Join(inst.GameDir, "mods", "present.jar")
	if err := os.WriteFile(jar, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mods.Install(InstallModRequest{InstanceID: inst.ID, Filename: "present.jar", URL: "https://example.com/mod.jar"}); err == nil || !strings.Contains(err.Error(), "установлен") {
		t.Fatalf("expected existing destination rejection, got %v", err)
	}
}

func TestModsSetAllEnabledAndDeleteBatch(t *testing.T) {
	_, mods, inst := setupModsService(t)
	modsDir := filepath.Join(inst.GameDir, "mods")
	_ = os.WriteFile(filepath.Join(modsDir, "mod_a.jar"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(modsDir, "mod_b.jar"), []byte("b"), 0o644)
	_ = os.WriteFile(filepath.Join(modsDir, "mod_c.jar"), []byte("c"), 0o644)

	// 1. Disable all
	list, err := mods.SetAllEnabled(inst.ID, false)
	if err != nil || len(list) != 3 {
		t.Fatalf("SetAllEnabled(false): err=%v, len=%d", err, len(list))
	}
	for _, m := range list {
		if m.Enabled {
			t.Fatalf("expected mod %s to be disabled", m.Filename)
		}
	}

	// 2. Enable all
	list, err = mods.SetAllEnabled(inst.ID, true)
	if err != nil || len(list) != 3 {
		t.Fatalf("SetAllEnabled(true): err=%v, len=%d", err, len(list))
	}
	for _, m := range list {
		if !m.Enabled {
			t.Fatalf("expected mod %s to be enabled", m.Filename)
		}
	}

	// 3. Delete batch
	list, err = mods.DeleteBatch(inst.ID, []string{"mod_a.jar", "mod_b.jar"})
	if err != nil || len(list) != 1 {
		t.Fatalf("DeleteBatch: err=%v, len=%d", err, len(list))
	}
	if list[0].Filename != "mod_c.jar" {
		t.Fatalf("expected only mod_c.jar remaining, got %s", list[0].Filename)
	}
}

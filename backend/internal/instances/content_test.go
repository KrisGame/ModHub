package instances

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"modhub/internal/domain"
)

func setupContentTest(t *testing.T) (*Manager, *Content, Instance) {
	t.Helper()
	mgr, _, _ := setupTestInstanceManager(t, []domain.SavedPack{})
	inst, err := mgr.Create(CreateInstanceRequest{Name: "Content test", MCVersion: "1.21.1"})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	content := NewContent(mgr, nil, nil)
	return mgr, content, inst
}

func createTestZip(t *testing.T, targetPath string, files map[string][]byte) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(targetPath), 0o755)
	f, err := os.Create(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResourcePacksListToggleDelete(t *testing.T) {
	_, content, inst := setupContentTest(t)
	rpDir := filepath.Join(inst.GameDir, "resourcepacks")

	// 1. Create a zip resource pack with pack.mcmeta and pack.png
	zipPath := filepath.Join(rpDir, "faithful.zip")
	createTestZip(t, zipPath, map[string][]byte{
		"pack.mcmeta": []byte(`{"pack": {"description": "Faithful 32x", "pack_format": 15}}`),
		"pack.png":    []byte("PNG_FAKEDATA"),
	})

	// 2. List resource packs
	list, err := content.ListResourcePacks(inst.ID)
	if err != nil {
		t.Fatalf("ListResourcePacks: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 resource pack, got %d", len(list))
	}
	rp := list[0]
	if rp.Filename != "faithful.zip" || rp.Name != "faithful" || !rp.Enabled {
		t.Fatalf("unexpected pack info: %+v", rp)
	}
	if rp.Description != "Faithful 32x" || rp.PackFormat != 15 {
		t.Fatalf("mcmeta not parsed: %+v", rp)
	}
	if !strings.HasPrefix(rp.IconDataURL, "data:image/png;base64,") {
		t.Fatalf("expected base64 icon data URL, got %q", rp.IconDataURL)
	}

	// 3. Toggle disable
	list, err = content.SetResourcePackEnabled(inst.ID, "faithful.zip", false)
	if err != nil || len(list) != 1 {
		t.Fatalf("disable pack: err=%v, len=%d", err, len(list))
	}
	if list[0].Enabled || list[0].Filename != "faithful.zip.disabled" {
		t.Fatalf("expected disabled pack, got %+v", list[0])
	}

	// 4. Toggle re-enable
	list, err = content.SetResourcePackEnabled(inst.ID, "faithful.zip.disabled", true)
	if err != nil || len(list) != 1 || !list[0].Enabled || list[0].Filename != "faithful.zip" {
		t.Fatalf("enable pack: err=%v, pack=%+v", err, list[0])
	}

	// 5. Delete pack
	list, err = content.DeleteResourcePack(inst.ID, "faithful.zip")
	if err != nil || len(list) != 0 {
		t.Fatalf("delete pack: err=%v, len=%d", err, len(list))
	}
}

func TestShaderPacksListToggleDelete(t *testing.T) {
	_, content, inst := setupContentTest(t)
	shadersDir := filepath.Join(inst.GameDir, "shaderpacks")

	zipPath := filepath.Join(shadersDir, "BSL.zip")
	createTestZip(t, zipPath, map[string][]byte{"shaders/main.vsh": []byte("// vertex")})

	list, err := content.ListShaderPacks(inst.ID)
	if err != nil || len(list) != 1 || !list[0].Enabled || list[0].Filename != "BSL.zip" {
		t.Fatalf("list shaders: err=%v, list=%+v", err, list)
	}

	list, err = content.SetShaderPackEnabled(inst.ID, "BSL.zip", false)
	if err != nil || len(list) != 1 || list[0].Enabled || list[0].Filename != "BSL.zip.disabled" {
		t.Fatalf("disable shader: err=%v, shader=%+v", err, list[0])
	}

	list, err = content.DeleteShaderPack(inst.ID, "BSL.zip.disabled")
	if err != nil || len(list) != 0 {
		t.Fatalf("delete shader: err=%v, len=%d", err, len(list))
	}
}

func buildFakeLevelDat(name string, gameType int32, hardcore bool, version string) []byte {
	var buf bytes.Buffer
	// Root TAG_Compound (10)
	buf.WriteByte(10)
	// Root name ""
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))

	// Compound "Data" (TAG_Compound = 10)
	buf.WriteByte(10)
	_ = binary.Write(&buf, binary.BigEndian, uint16(4))
	buf.WriteString("Data")

	// LevelName (TAG_String = 8)
	buf.WriteByte(8)
	_ = binary.Write(&buf, binary.BigEndian, uint16(9))
	buf.WriteString("LevelName")
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(name)))
	buf.WriteString(name)

	// GameType (TAG_Int = 3)
	buf.WriteByte(3)
	_ = binary.Write(&buf, binary.BigEndian, uint16(8))
	buf.WriteString("GameType")
	_ = binary.Write(&buf, binary.BigEndian, gameType)

	// hardcore (TAG_Byte = 1)
	buf.WriteByte(1)
	_ = binary.Write(&buf, binary.BigEndian, uint16(8))
	buf.WriteString("hardcore")
	if hardcore {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}

	// LastPlayed (TAG_Long = 4)
	buf.WriteByte(4)
	_ = binary.Write(&buf, binary.BigEndian, uint16(10))
	buf.WriteString("LastPlayed")
	_ = binary.Write(&buf, binary.BigEndian, int64(1700000000000))

	// Version compound
	buf.WriteByte(10)
	_ = binary.Write(&buf, binary.BigEndian, uint16(7))
	buf.WriteString("Version")
	// Version.Name (TAG_String = 8)
	buf.WriteByte(8)
	_ = binary.Write(&buf, binary.BigEndian, uint16(4))
	buf.WriteString("Name")
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(version)))
	buf.WriteString(version)
	// Version TAG_End
	buf.WriteByte(0)

	// Data TAG_End
	buf.WriteByte(0)

	// Root TAG_End
	buf.WriteByte(0)

	// Compress with gzip
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write(buf.Bytes())
	_ = gw.Close()

	return gzBuf.Bytes()
}

func TestWorldsListAndExport(t *testing.T) {
	_, content, inst := setupContentTest(t)
	savesDir := filepath.Join(inst.GameDir, "saves")
	worldDir := filepath.Join(savesDir, "MySurvivalWorld")
	_ = os.MkdirAll(worldDir, 0o755)

	levelDat := buildFakeLevelDat("Epic Survival", 0, false, "1.21.1")
	if err := os.WriteFile(filepath.Join(worldDir, "level.dat"), levelDat, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "icon.png"), []byte("FAKE_ICON_DATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	// List worlds
	worlds, err := content.ListWorlds(inst.ID)
	if err != nil {
		t.Fatalf("ListWorlds: %v", err)
	}
	if len(worlds) != 1 {
		t.Fatalf("expected 1 world, got %d", len(worlds))
	}
	w := worlds[0]
	if w.Folder != "MySurvivalWorld" || w.Name != "Epic Survival" || w.GameType != "survival" || w.Version != "1.21.1" {
		t.Fatalf("unexpected world data: %+v", w)
	}
	if !strings.HasPrefix(w.IconDataURL, "data:image/png;base64,") {
		t.Fatalf("expected icon data URL, got %q", w.IconDataURL)
	}

	// Export world
	exp, err := content.ExportWorld(inst.ID, "MySurvivalWorld")
	if err != nil {
		t.Fatalf("ExportWorld: %v", err)
	}
	if !strings.HasSuffix(exp.Filename, ".zip") || exp.Size <= 0 {
		t.Fatalf("invalid export result: %+v", exp)
	}
	if _, err := os.Stat(exp.ArchivePath); err != nil {
		t.Fatalf("export archive missing: %v", err)
	}
}

package instances

import (
	"modhub/internal/domain"
	javart "modhub/internal/runtime"
)

type Status string

const (
	StatusCreating   Status = "creating"
	StatusReady      Status = "ready"
	StatusNeedsSetup Status = "needs_setup"
	StatusCorrupted  Status = "corrupted"
	StatusDeleting   Status = "deleting"
)

type InstanceType string

const (
	TypeEmpty    InstanceType = "empty"
	TypeVanilla  InstanceType = "vanilla"
	TypeNeoForge InstanceType = "neoforge"
	TypeFabric   InstanceType = "fabric"
	TypeForge    InstanceType = "forge"
	TypeQuilt    InstanceType = "quilt"
)

type JavaMode string

const (
	JavaModeAuto    JavaMode = "auto"
	JavaModeManaged JavaMode = "managed"
	JavaModeCustom  JavaMode = "custom"
)

// LaunchSettings stores declarative per-instance launch configuration (no arbitrary commands are executed in Stage 5).
type LaunchSettings struct {
	JVMArgs         string `json:"jvm_args,omitempty"`
	WindowWidth     int    `json:"window_width,omitempty"`
	WindowHeight    int    `json:"window_height,omitempty"`
	Fullscreen      bool   `json:"fullscreen,omitempty"`
	ActiveVersionID string `json:"active_version_id,omitempty"`
	AccountID       string `json:"account_id,omitempty"`
}

// InstalledModRef holds basic metadata about a detected mod file inside minecraft/mods/.
type InstalledModRef struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Enabled  bool   `json:"enabled"`
}

// IntegrityIssue distinguishes missing installable components from actual profile corruption.
type IntegrityIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // missing_component | corruption | warning
	Message  string `json:"message"`
}

// IntegrityReport is returned by Verify() and attached to Instance details.
type IntegrityReport struct {
	InstanceID         string           `json:"instance_id"`
	Status             Status           `json:"status"`
	RootExists         bool             `json:"root_exists"`
	ManifestValid      bool             `json:"manifest_valid"`
	GameDirExists      bool             `json:"game_dir_exists"`
	SubdirsOK          bool             `json:"subdirs_ok"`
	MissingSubdirs     []string         `json:"missing_subdirs,omitempty"`
	SharedResourcesOK  bool             `json:"shared_resources_ok"`
	JavaOK             bool             `json:"java_ok"`
	JavaSummary        string           `json:"java_summary"`
	GameFilesInstalled bool             `json:"game_files_installed"`
	ModsCount          int              `json:"mods_count"`
	Issues             []IntegrityIssue `json:"issues"`
	CheckedAt          int64            `json:"checked_at"`
}

// Instance is the rich runtime view of a managed Minecraft profile.
type Instance struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	Description     string                    `json:"description"`
	Icon            string                    `json:"icon"`
	MCVersion       string                    `json:"mc_version"`
	InstanceType    InstanceType              `json:"instance_type"`
	LoaderVersion   string                    `json:"loader_version,omitempty"`
	RootDir         string                    `json:"root_dir"`
	GameDir         string                    `json:"game_dir"`
	JavaMode        JavaMode                  `json:"java_mode"`
	JavaRuntimeID   string                    `json:"java_runtime_id,omitempty"`
	JavaPath        string                    `json:"java_path,omitempty"`
	JavaRequirement *javart.RequirementResult `json:"java_requirement,omitempty"`
	JavaResolved    *javart.JavaRuntime       `json:"java_resolved,omitempty"`
	MemoryMinMB     int                       `json:"memory_min_mb"`
	MemoryMaxMB     int                       `json:"memory_max_mb"`
	LaunchSettings  LaunchSettings            `json:"launch_settings"`
	LinkedPackID    string                    `json:"linked_pack_id,omitempty"`
	LinkedPackName  string                    `json:"linked_pack_name,omitempty"`
	LinkedPackItems int                       `json:"linked_pack_items,omitempty"`
	InstalledMods   []InstalledModRef         `json:"installed_mods"`
	ModsCount       int                       `json:"mods_count"`
	Status          Status                    `json:"status"`
	Issues          []IntegrityIssue          `json:"issues"`
	InUse           bool                      `json:"in_use"`
	CreatedAt       int64                     `json:"created_at"`
	UpdatedAt       int64                     `json:"updated_at"`
	LastPlayedAt    int64                     `json:"last_played_at"`
}

// InstanceManifest is written atomically to <root_dir>/instance.json for portability and self-description.
type InstanceManifest struct {
	SchemaVersion  int            `json:"schema_version"`
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Icon           string         `json:"icon"`
	MCVersion      string         `json:"mc_version"`
	InstanceType   InstanceType   `json:"instance_type"`
	LoaderVersion  string         `json:"loader_version,omitempty"`
	GameDir        string         `json:"game_dir"`
	JavaMode       JavaMode       `json:"java_mode"`
	JavaRuntimeID  string         `json:"java_runtime_id,omitempty"`
	JavaPath       string         `json:"java_path,omitempty"`
	MemoryMinMB    int            `json:"memory_min_mb"`
	MemoryMaxMB    int            `json:"memory_max_mb"`
	LaunchSettings LaunchSettings `json:"launch_settings"`
	LinkedPackID   string         `json:"linked_pack_id,omitempty"`
	LinkedPackName string         `json:"linked_pack_name,omitempty"`
	CreatedAt      int64          `json:"created_at"`
	UpdatedAt      int64          `json:"updated_at"`
	LastPlayedAt   int64          `json:"last_played_at"`
}

type CreateInstanceRequest struct {
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Icon           string         `json:"icon,omitempty"`
	MCVersion      string         `json:"mc_version,omitempty"`
	InstanceType   InstanceType   `json:"instance_type,omitempty"`
	LoaderVersion  string         `json:"loader_version,omitempty"`
	CustomDir      string         `json:"custom_dir,omitempty"`
	JavaMode       JavaMode       `json:"java_mode,omitempty"`
	JavaRuntimeID  string         `json:"java_runtime_id,omitempty"`
	JavaPath       string         `json:"java_path,omitempty"`
	MemoryMinMB    int            `json:"memory_min_mb,omitempty"`
	MemoryMaxMB    int            `json:"memory_max_mb,omitempty"`
	LaunchSettings LaunchSettings `json:"launch_settings,omitempty"`
	LinkedPackID   string         `json:"linked_pack_id,omitempty"`
}

type UpdateInstanceRequest struct {
	ID             string          `json:"id"`
	Name           *string         `json:"name,omitempty"`
	Description    *string         `json:"description,omitempty"`
	Icon           *string         `json:"icon,omitempty"`
	MCVersion      *string         `json:"mc_version,omitempty"`
	InstanceType   *InstanceType   `json:"instance_type,omitempty"`
	LoaderVersion  *string         `json:"loader_version,omitempty"`
	JavaMode       *JavaMode       `json:"java_mode,omitempty"`
	JavaRuntimeID  *string         `json:"java_runtime_id,omitempty"`
	JavaPath       *string         `json:"java_path,omitempty"`
	MemoryMinMB    *int            `json:"memory_min_mb,omitempty"`
	MemoryMaxMB    *int            `json:"memory_max_mb,omitempty"`
	LaunchSettings *LaunchSettings `json:"launch_settings,omitempty"`
	LinkedPackID   *string         `json:"linked_pack_id,omitempty"`
}

type DuplicateInstanceRequest struct {
	SourceID          string `json:"source_id"`
	NewName           string `json:"new_name"`
	Description       string `json:"description,omitempty"`
	CopySettings      bool   `json:"copy_settings"`
	CopyMods          bool   `json:"copy_mods"`
	CopyConfigs       bool   `json:"copy_configs"`
	CopyResourcePacks bool   `json:"copy_resourcepacks"`
	CopyShaderPacks   bool   `json:"copy_shaderpacks"`
	CopySaves         bool   `json:"copy_saves"`
}

type DeleteInstanceRequest struct {
	ID          string `json:"id"`
	DeleteFiles bool   `json:"delete_files"`
}

type FolderStat struct {
	Name       string `json:"name"`
	RelPath    string `json:"rel_path"`
	FullPath   string `json:"full_path"`
	Exists     bool   `json:"exists"`
	FileCount  int    `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
}

type InstanceFilesSummary struct {
	InstanceID  string       `json:"instance_id"`
	RootDir     string       `json:"root_dir"`
	GameDir     string       `json:"game_dir"`
	SharedDir   string       `json:"shared_dir"`
	RuntimesDir string       `json:"runtimes_dir"`
	Folders     []FolderStat `json:"folders"`
	TotalBytes  int64        `json:"total_bytes"`
}

type SharedStoragePaths struct {
	SharedDir    string `json:"shared_dir"`
	AssetsDir    string `json:"assets_dir"`
	LibrariesDir string `json:"libraries_dir"`
	VersionsDir  string `json:"versions_dir"`
	RuntimesDir  string `json:"runtimes_dir"`
	InstancesDir string `json:"instances_dir"`
}

type ListSnapshot struct {
	Instances []Instance         `json:"instances"`
	Paths     SharedStoragePaths `json:"paths"`
}

// Store abstracts SQLite persistence for instances and pack links.
type Store interface {
	UpsertInstance(rec domain.InstanceRecord) error
	ListInstances() ([]domain.InstanceRecord, error)
	DeleteInstance(id string) error
	SetInstancePackLink(instanceID, packID, packName string, linkedAt int64) error
	ListInstancePackLinks(instanceID string) ([]domain.InstancePackLinkRecord, error)
}

// PackLookup provides read-only access to saved ModHub packs (packs.json).
type PackLookup func() []domain.SavedPack

// ResourcePackRef holds metadata about a resource pack in resourcepacks/.
type ResourcePackRef struct {
	Filename    string `json:"filename"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PackFormat  int    `json:"pack_format"`
	Size        int64  `json:"size"`
	Enabled     bool   `json:"enabled"`
	IconDataURL string `json:"icon_data_url,omitempty"`
}

// ShaderPackRef holds metadata about a shaderpack in shaderpacks/.
type ShaderPackRef struct {
	Filename string `json:"filename"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Enabled  bool   `json:"enabled"`
}

// WorldRef holds parsed metadata about a saved singleplayer world in saves/.
type WorldRef struct {
	Folder      string `json:"folder"`
	Name        string `json:"name"`
	GameType    string `json:"game_type"` // survival, creative, adventure, spectator, hardcore
	Hardcore    bool   `json:"hardcore"`
	Version     string `json:"version"`
	LastPlayed  int64  `json:"last_played"` // unix seconds
	Size        int64  `json:"size"`
	IconDataURL string `json:"icon_data_url,omitempty"`
}

// ExportWorldResult holds details about an exported world archive.
type ExportWorldResult struct {
	ArchivePath string `json:"archive_path"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"download_url,omitempty"`
}

type ContentActionRequest struct {
	InstanceID string `json:"id"`
	Filename   string `json:"filename"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

// ModUpdateInfo describes available update for an installed mod.
type ModUpdateInfo struct {
	CurrentFilename  string `json:"current_filename"`
	CurrentSHA1      string `json:"current_sha1"`
	CurrentVersion   string `json:"current_version,omitempty"`
	ProjectID        string `json:"project_id,omitempty"`
	ProjectTitle     string `json:"project_title,omitempty"`
	NewVersionID     string `json:"new_version_id,omitempty"`
	NewVersionNumber string `json:"new_version_number,omitempty"`
	NewFilename      string `json:"new_filename,omitempty"`
	NewDownloadURL   string `json:"new_download_url,omitempty"`
	NewSize          int64  `json:"new_size,omitempty"`
	NewSHA1          string `json:"new_sha1,omitempty"`
	ReleaseType      string `json:"release_type,omitempty"` // release | beta | alpha
	HasUpdate        bool   `json:"has_update"`
}

// ApplyModUpdateRequest defines parameters to update an installed mod to a newer version.
type ApplyModUpdateRequest struct {
	InstanceID      string `json:"id"`
	CurrentFilename string `json:"current_filename"`
	NewFilename     string `json:"new_filename"`
	NewURL          string `json:"new_url"`
	NewSize         int64  `json:"new_size,omitempty"`
	NewSHA1         string `json:"new_sha1,omitempty"`
}

// BatchModActionRequest defines parameters for bulk operations on instance mods.
type BatchModActionRequest struct {
	InstanceID string   `json:"id"`
	Filenames  []string `json:"filenames,omitempty"`
	Enabled    *bool    `json:"enabled,omitempty"`
}

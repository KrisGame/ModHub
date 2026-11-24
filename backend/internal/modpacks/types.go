package modpacks

import "modhub/internal/domain"

type ModpackFormat string

const (
	FormatMRPack     ModpackFormat = "mrpack"
	FormatCurseForge ModpackFormat = "curseforge"
	FormatModHub     ModpackFormat = "modhub"

	ModHubFormatVersionCurrent = 1
)

type ComponentCategory string

const (
	CatMod          ComponentCategory = "mod"
	CatAddon        ComponentCategory = "addon"
	CatResourcePack ComponentCategory = "resourcepack"
	CatShader       ComponentCategory = "shader"
	CatDatapack     ComponentCategory = "datapack"
	CatConfig       ComponentCategory = "config"
	CatUserFile     ComponentCategory = "userfile"
)

type ConflictResolution string

const (
	ConflictKeepExisting       ConflictResolution = "keep_existing"
	ConflictOverwrite          ConflictResolution = "overwrite"
	ConflictBackupAndOverwrite ConflictResolution = "backup_and_overwrite"
)

type ModrinthIndexJSON struct {
	FormatVersion int                 `json:"formatVersion"`
	Game          string              `json:"game"`
	VersionID     string              `json:"versionId"`
	Name          string              `json:"name"`
	Summary       string              `json:"summary,omitempty"`
	Files         []ModrinthIndexFile `json:"files"`
	Dependencies  map[string]string   `json:"dependencies"`
}

type ModrinthIndexFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       *ModrinthFileEnv  `json:"env,omitempty"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

type ModrinthFileEnv struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

type CurseForgeManifestJSON struct {
	Minecraft       CFManifestMinecraft `json:"minecraft"`
	ManifestType    string              `json:"manifestType"`
	ManifestVersion int                 `json:"manifestVersion"`
	Name            string              `json:"name"`
	Version         string              `json:"version"`
	Author          string              `json:"author"`
	Files           []CFManifestFile    `json:"files"`
	Overrides       string              `json:"overrides"`
}

type CFManifestMinecraft struct {
	Version    string             `json:"version"`
	ModLoaders []CFManifestLoader `json:"modLoaders"`
}

type CFManifestLoader struct {
	ID      string `json:"id"`
	Primary bool   `json:"primary"`
}

type CFManifestFile struct {
	ProjectID int  `json:"projectID"`
	FileID    int  `json:"fileID"`
	Required  bool `json:"required"`
}

type ModHubPackManifest struct {
	FormatVersion   int                       `json:"format_version"`
	Name            string                    `json:"name"`
	Author          string                    `json:"author,omitempty"`
	Description     string                    `json:"description,omitempty"`
	Version         string                    `json:"version"`
	MCVersion       string                    `json:"mc_version"`
	Loader          string                    `json:"loader"`
	LoaderVersion   string                    `json:"loader_version,omitempty"`
	CreatedAt       int64                     `json:"created_at"`
	ProfileSettings *ModHubPackProfileParams  `json:"profile_settings,omitempty"`
	Files           []ModHubPackFileEntry     `json:"files"`
	Overrides       []ModHubPackOverrideEntry `json:"overrides,omitempty"`
}

type ModHubPackProfileParams struct {
	MemoryMinMB  int    `json:"memory_min_mb,omitempty"`
	MemoryMaxMB  int    `json:"memory_max_mb,omitempty"`
	JVMArgs      string `json:"jvm_args,omitempty"`
	WindowWidth  int    `json:"window_width,omitempty"`
	WindowHeight int    `json:"window_height,omitempty"`
	Fullscreen   bool   `json:"fullscreen,omitempty"`
}

type ModHubPackFileEntry struct {
	Path           string            `json:"path"`
	Filename       string            `json:"filename"`
	Title          string            `json:"title,omitempty"`
	Category       ComponentCategory `json:"category"`
	SourcePlatform string            `json:"source_platform,omitempty"`
	ProjectID      string            `json:"project_id,omitempty"`
	VersionID      string            `json:"version_id,omitempty"`
	FileID         string            `json:"file_id,omitempty"`
	DownloadURL    string            `json:"download_url,omitempty"`
	ArchiveEntry   string            `json:"archive_entry,omitempty"`
	SHA1           string            `json:"sha1,omitempty"`
	SHA256         string            `json:"sha256,omitempty"`
	SHA512         string            `json:"sha512,omitempty"`
	Size           int64             `json:"size,omitempty"`
	Optional       bool              `json:"optional,omitempty"`
	EnvClient      string            `json:"env_client,omitempty"`
	EnvServer      string            `json:"env_server,omitempty"`
}

type ModHubPackOverrideEntry struct {
	RelPath      string            `json:"rel_path"`
	ArchiveEntry string            `json:"archive_entry"`
	Category     ComponentCategory `json:"category"`
	SHA256       string            `json:"sha256,omitempty"`
	Size         int64             `json:"size,omitempty"`
}

type ImportCounts struct {
	Mods          int `json:"mods"`
	Addons        int `json:"addons"`
	ResourcePacks int `json:"resourcepacks"`
	Shaders       int `json:"shaders"`
	Datapacks     int `json:"datapacks"`
	Configs       int `json:"configs"`
	UserFiles     int `json:"user_files"`
	TotalFiles    int `json:"total_files"`
	OptionalFiles int `json:"optional_files"`
	Unavailable   int `json:"unavailable"`
	ServerSkipped int `json:"server_skipped"`
}

type ModpackFilePlan struct {
	RelPath           string            `json:"rel_path"`
	Title             string            `json:"title"`
	Filename          string            `json:"filename"`
	Category          ComponentCategory `json:"category"`
	SourceKind        string            `json:"source_kind"`
	ArchiveEntry      string            `json:"archive_entry,omitempty"`
	SourcePlatform    string            `json:"source_platform,omitempty"`
	SourceProjectID   string            `json:"source_project_id,omitempty"`
	SourceFileID      string            `json:"source_file_id,omitempty"`
	DownloadURLs      []string          `json:"download_urls,omitempty"`
	SHA1              string            `json:"sha1,omitempty"`
	SHA256            string            `json:"sha256,omitempty"`
	SHA512            string            `json:"sha512,omitempty"`
	Size              int64             `json:"size"`
	Optional          bool              `json:"optional"`
	SelectedByDefault bool              `json:"selected_by_default"`
	Available         bool              `json:"available"`
	UnavailableReason string            `json:"unavailable_reason,omitempty"`
	Suspicious        bool              `json:"suspicious,omitempty"`
	SuspiciousReason  string            `json:"suspicious_reason,omitempty"`
}

type ImportConflict struct {
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

type PreviewImportRequest struct {
	SourceType string `json:"source_type"`
	SourceRef  string `json:"source_ref"`
	VersionID  string `json:"version_id,omitempty"`
}

type ImportPreview struct {
	PreviewID           string                   `json:"preview_id"`
	Format              ModpackFormat            `json:"format"`
	SourceType          string                   `json:"source_type"`
	SourceRef           string                   `json:"source_ref"`
	SourceProjectID     string                   `json:"source_project_id,omitempty"`
	SourceVersionID     string                   `json:"source_version_id,omitempty"`
	ResolvedArchivePath string                   `json:"resolved_archive_path,omitempty"`
	Name                string                   `json:"name"`
	Author              string                   `json:"author,omitempty"`
	Description         string                   `json:"description,omitempty"`
	Version             string                   `json:"version"`
	MCVersion           string                   `json:"mc_version"`
	Loader              string                   `json:"loader"`
	LoaderVersion       string                   `json:"loader_version,omitempty"`
	ManifestHash        string                   `json:"manifest_hash"`
	EstimatedBytes      int64                    `json:"estimated_bytes"`
	Counts              ImportCounts             `json:"counts"`
	Files               []ModpackFilePlan        `json:"files"`
	UnavailableFiles    []ModpackFilePlan        `json:"unavailable_files,omitempty"`
	OptionalFiles       []ModpackFilePlan        `json:"optional_files,omitempty"`
	Conflicts           []ImportConflict         `json:"conflicts,omitempty"`
	Warnings            []string                 `json:"warnings,omitempty"`
	ProfileSettings     *ModHubPackProfileParams `json:"profile_settings,omitempty"`
}

type ExecuteImportRequest struct {
	PreviewID        string             `json:"preview_id"`
	ProfileName      string             `json:"profile_name,omitempty"`
	Description      string             `json:"description,omitempty"`
	Icon             string             `json:"icon,omitempty"`
	CustomDir        string             `json:"custom_dir,omitempty"`
	MemoryMaxMB      int                `json:"memory_max_mb,omitempty"`
	SelectedOptional []string           `json:"selected_optional,omitempty"`
	SkipUnavailable  bool               `json:"skip_unavailable"`
	ConflictPolicy   ConflictResolution `json:"conflict_policy,omitempty"`
	InstallMinecraft bool               `json:"install_minecraft"`
	TargetInstanceID string             `json:"target_instance_id,omitempty"`
}

type ImportProgress struct {
	ModpackID      string  `json:"modpack_id,omitempty"`
	InstanceID     string  `json:"instance_id,omitempty"`
	Name           string  `json:"name"`
	Stage          string  `json:"stage"`
	Message        string  `json:"message"`
	CompletedFiles int     `json:"completed_files"`
	TotalFiles     int     `json:"total_files"`
	Progress       float64 `json:"progress"`
	Error          string  `json:"error,omitempty"`
	UpdatedAt      int64   `json:"updated_at"`
}

type ImportResult struct {
	Modpack          ModpackView       `json:"modpack"`
	InstanceID       string            `json:"instance_id"`
	DownloadedFiles  int               `json:"downloaded_files"`
	AppliedOverrides int               `json:"applied_overrides"`
	SkippedOptional  int               `json:"skipped_optional"`
	Unavailable      []ModpackFilePlan `json:"unavailable,omitempty"`
	FailedFiles      []string          `json:"failed_files,omitempty"`
	Warnings         []string          `json:"warnings,omitempty"`
	Status           string            `json:"status"`
}

type ExportModpackRequest struct {
	SourceType           string        `json:"source_type"`
	SourceID             string        `json:"source_id"`
	Format               ModpackFormat `json:"format"`
	OutputPath           string        `json:"output_path,omitempty"`
	Name                 string        `json:"name,omitempty"`
	Author               string        `json:"author,omitempty"`
	Description          string        `json:"description,omitempty"`
	Version              string        `json:"version,omitempty"`
	IncludeMods          bool          `json:"include_mods"`
	IncludeConfigs       bool          `json:"include_configs"`
	IncludeResourcePacks bool          `json:"include_resourcepacks"`
	IncludeShaders       bool          `json:"include_shaders"`
	IncludeDatapacks     bool          `json:"include_datapacks"`
	IncludeScreenshots   bool          `json:"include_screenshots"`
	IncludeSaves         bool          `json:"include_saves"`
	IncludeExtraFiles    bool          `json:"include_extra_files"`
}

type ExportModpackResult struct {
	Cancelled  bool          `json:"cancelled"`
	Format     ModpackFormat `json:"format"`
	OutputPath string        `json:"output_path,omitempty"`
	TotalFiles int           `json:"total_files"`
	SizeBytes  int64         `json:"size_bytes"`
	Warnings   []string      `json:"warnings,omitempty"`
}

type UpdateDiffEntry struct {
	RelPath    string            `json:"rel_path"`
	Title      string            `json:"title,omitempty"`
	Category   ComponentCategory `json:"category"`
	Action     string            `json:"action"`
	OldHash    string            `json:"old_hash,omitempty"`
	NewHash    string            `json:"new_hash,omitempty"`
	UserEdited bool              `json:"user_edited,omitempty"`
	Reason     string            `json:"reason,omitempty"`
}

type UpdateDiffPreview struct {
	ModpackID        string            `json:"modpack_id"`
	PreviewID        string            `json:"preview_id"`
	CurrentVersion   string            `json:"current_version"`
	NewVersion       string            `json:"new_version"`
	HasUpdate        bool              `json:"has_update"`
	SourceSupported  bool              `json:"source_supported"`
	SourceNote       string            `json:"source_note,omitempty"`
	Added            []UpdateDiffEntry `json:"added"`
	Updated          []UpdateDiffEntry `json:"updated"`
	Removed          []UpdateDiffEntry `json:"removed"`
	ConfigConflicts  []UpdateDiffEntry `json:"config_conflicts"`
	PreservedUser    []UpdateDiffEntry `json:"preserved_user"`
	UnavailableFiles []ModpackFilePlan `json:"unavailable_files,omitempty"`
	Warnings         []string          `json:"warnings,omitempty"`
}

type ApplyUpdateRequest struct {
	ModpackID          string             `json:"modpack_id"`
	PreviewID          string             `json:"preview_id"`
	ConfigConflictMode ConflictResolution `json:"config_conflict_mode,omitempty"`
	SkipUnavailable    bool               `json:"skip_unavailable"`
}

type ModpackView struct {
	ID              string                       `json:"id"`
	Name            string                       `json:"name"`
	Author          string                       `json:"author,omitempty"`
	Description     string                       `json:"description,omitempty"`
	Version         string                       `json:"version"`
	Format          ModpackFormat                `json:"format"`
	SourceType      string                       `json:"source_type"`
	SourceRef       string                       `json:"source_ref,omitempty"`
	SourceProjectID string                       `json:"source_project_id,omitempty"`
	SourceVersionID string                       `json:"source_version_id,omitempty"`
	MCVersion       string                       `json:"mc_version"`
	Loader          string                       `json:"loader"`
	LoaderVersion   string                       `json:"loader_version,omitempty"`
	InstanceID      string                       `json:"instance_id,omitempty"`
	Status          string                       `json:"status"`
	LastError       string                       `json:"last_error,omitempty"`
	FilesCount      int                          `json:"files_count"`
	ManagedCount    int                          `json:"managed_count"`
	CanCheckRemote  bool                         `json:"can_check_remote"`
	CreatedAt       int64                        `json:"created_at"`
	UpdatedAt       int64                        `json:"updated_at"`
	InstalledAt     int64                        `json:"installed_at"`
	UpdateHistory   []domain.ModpackUpdateRecord `json:"update_history,omitempty"`
}

type Store interface {
	UpsertModpack(rec domain.ModpackRecord) error
	ListModpacks() ([]domain.ModpackRecord, error)
	DeleteModpack(id string) error
	ReplaceModpackFiles(modpackID string, files []domain.ModpackFileRecord) error
	ListModpackFiles(modpackID string) ([]domain.ModpackFileRecord, error)
	InsertModpackUpdate(rec domain.ModpackUpdateRecord) error
	ListModpackUpdates(modpackID string) ([]domain.ModpackUpdateRecord, error)
	ListModpackFileStats() (map[string][2]int, error)
	ListRecentModpackUpdatesMap(perPackLimit int) (map[string][]domain.ModpackUpdateRecord, error)
}

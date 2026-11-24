package domain

type ProjectLinks struct {
	SourceURL  string `json:"source_url"`
	IssuesURL  string `json:"issues_url"`
	WikiURL    string `json:"wiki_url"`
	DiscordURL string `json:"discord_url"`
}

type ProjectSource struct {
	Platform    string   `json:"platform"`
	SourceKey   string   `json:"source_key"`
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Downloads   int64    `json:"downloads"`
	Authors     []string `json:"authors"`
	Loaders     []string `json:"loaders"`
	ProjectType string   `json:"project_type"`
}

type DependentInfo struct {
	Relation string `json:"relation"`
	Label    string `json:"label"`
	Order    int    `json:"order"`
}

type RawProjectCard struct {
	Platform          string       `json:"platform"`
	SourceKey         string       `json:"source_key"`
	ID                string       `json:"id"`
	Slug              string       `json:"slug"`
	ClassID           int          `json:"class_id"`
	ContentType       string       `json:"content_type"`
	Title             string       `json:"title"`
	Summary           string       `json:"summary"`
	Authors           []string     `json:"authors"`
	IconURL           string       `json:"icon_url"`
	Downloads         int64        `json:"downloads"`
	DateModified      string       `json:"date_modified"`
	DateCreated       string       `json:"date_created,omitempty"`
	License           string       `json:"license"`
	Status            string       `json:"status"`
	ClientSide        string       `json:"client_side"`
	ServerSide        string       `json:"server_side"`
	Loaders           []string     `json:"loaders"`
	GameVersions      []string     `json:"game_versions"`
	Categories        []string     `json:"categories"`
	URL               string       `json:"url"`
	Links             ProjectLinks `json:"links"`
	DependentRelation string       `json:"dependent_relation,omitempty"`
	DependentLabel    string       `json:"dependent_label,omitempty"`
}

type ProjectCard struct {
	GroupID       string          `json:"group_id"`
	ContentType   string          `json:"content_type"`
	Title         string          `json:"title"`
	Slug          string          `json:"slug"`
	Summary       string          `json:"summary"`
	Authors       []string        `json:"authors"`
	IconURL       string          `json:"icon_url"`
	Downloads     int64           `json:"downloads"`
	DateModified  string          `json:"date_modified"`
	DateCreated   string          `json:"date_created,omitempty"`
	License       string          `json:"license"`
	Status        string          `json:"status"`
	ClientSide    string          `json:"client_side"`
	ServerSide    string          `json:"server_side"`
	Loaders       []string        `json:"loaders"`
	GameVersions  []string        `json:"game_versions"`
	Categories    []string        `json:"categories"`
	Platforms     []string        `json:"platforms"`
	IsMerged      bool            `json:"is_merged"`
	Sources       []ProjectSource `json:"sources"`
	Links         ProjectLinks    `json:"links"`
	DependentInfo *DependentInfo  `json:"dependent_info,omitempty"`
}

type DependencyRef struct {
	Platform  string `json:"platform"`
	ProjectID string `json:"project_id"`
	VersionID string `json:"version_id"`
	Type      string `json:"type"`
}

type VersionFile struct {
	Platform      string          `json:"platform"`
	SourceKey     string          `json:"source_key"`
	VersionID     string          `json:"version_id"`
	ModID         string          `json:"mod_id"`
	Name          string          `json:"name"`
	VersionNumber string          `json:"version_number"`
	ReleaseType   string          `json:"release_type"`
	DatePublished string          `json:"date_published"`
	Downloads     int64           `json:"downloads"`
	Loaders       []string        `json:"loaders"`
	GameVersions  []string        `json:"game_versions"`
	Filename      string          `json:"filename"`
	FileSize      int64           `json:"file_size"`
	DownloadURL   string          `json:"download_url"`
	FallbackURL   string          `json:"fallback_url"`
	IsAvailable   bool            `json:"is_available"`
	Changelog     string          `json:"changelog"`
	Dependencies  []DependencyRef `json:"dependencies"`
}

type GalleryItem struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ModDetailsResponse struct {
	OK         bool          `json:"ok"`
	Item       ProjectCard   `json:"item"`
	Body       string        `json:"body"`
	BodyFormat string        `json:"body_format"`
	Gallery    []GalleryItem `json:"gallery"`
	Links      ProjectLinks  `json:"links"`
	Versions   []VersionFile `json:"versions"`
}

type CartItem struct {
	Title         string   `json:"title"`
	IconURL       string   `json:"icon_url"`
	Platform      string   `json:"platform"`
	SourceKey     string   `json:"source_key"`
	SourceKeys    []string `json:"source_keys"`
	GroupID       string   `json:"group_id"`
	Filename      string   `json:"filename"`
	FileSize      int64    `json:"file_size"`
	DownloadURL   string   `json:"download_url"`
	FallbackURL   string   `json:"fallback_url"`
	IsAvailable   bool     `json:"is_available"`
	Folder        string   `json:"folder"`
	Loaders       []string `json:"loaders"`
	GameVersions  []string `json:"game_versions"`
	MCVersion     string   `json:"mc_version"`
	Loader        string   `json:"loader"`
	QuickAddedKey string   `json:"quick_added_key,omitempty"`
	AutoDepFor    []string `json:"auto_dep_for"`
}

type QuickFileRequest struct {
	Item               ProjectCard `json:"item"`
	MCVersion          string      `json:"mc_version"`
	Loader             string      `json:"loader"`
	IncludeDeps        bool        `json:"include_deps"`
	ExistingSourceKeys []string    `json:"existing_source_keys"`
	ExistingFilenames  []string    `json:"existing_filenames"`
}

type UnresolvedDep struct {
	ModID     string `json:"mod_id"`
	Platform  string `json:"platform,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Type      string `json:"type"`
	Source    string `json:"source,omitempty"`
	Reason    string `json:"reason"`
}

type QuickFileResponse struct {
	OK             bool              `json:"ok"`
	File           *CartItem         `json:"file"`
	ResolvedDeps   []CartItem        `json:"resolved_deps"`
	UnresolvedDeps   []UnresolvedDep   `json:"unresolved_deps,omitempty"`
	NeedsConfirmDeps []DepTreeNode     `json:"needs_confirm_deps,omitempty"`
	RequiredDeps   []DependencyRef   `json:"required_deps,omitempty"`
	DepWarnings    []string          `json:"dep_warnings,omitempty"`
	DepErrors      map[string]string `json:"dep_errors,omitempty"`
	DepStatus      string            `json:"dep_status,omitempty"`
	ManifestStatus string            `json:"manifest_status,omitempty"`
	Reason         string            `json:"reason,omitempty"`
}

type SearchParams struct {
	Query          string
	ContentType    string
	Version        string
	LoadersIn      []string
	LoadersOut     []string
	CategoriesIn   []string
	CategoriesOut  []string
	CatMode        string
	Env            string
	UpdatedWithin  int
	HideAbandoned  bool
	Sort           string
	Sources        []string
	AutoMerge      bool
	Page           int
	Limit          int
	MROffset       int
	CFOffset       int
}

type SearchResponse struct {
	OK               bool              `json:"ok"`
	Items            []ProjectCard     `json:"items"`
	Totals           map[string]int64  `json:"totals"`
	HasMore          bool              `json:"has_more"`
	NextMROffset     int               `json:"next_mr_offset"`
	NextCFOffset     int               `json:"next_cf_offset"`
	CurseForgeStatus string            `json:"curseforge_status"`
	SmartNotice      string            `json:"smart_notice"`
	SourceMode       string            `json:"source_mode"`
	Errors           map[string]string `json:"errors"`
}

type DependentsRequest struct {
	Item          ProjectCard `json:"item"`
	Relation      string      `json:"relation"`
	ProjectFilter string      `json:"project_filter"`
	ContentType   string      `json:"content_type"`
	MCVersion     string      `json:"mc_version"`
	LoadersIn     []string    `json:"loaders_in"`
	LoadersOut    []string    `json:"loaders_out"`
	CategoriesIn  []string    `json:"categories"`
	CategoriesOut []string    `json:"categories_out"`
	CatMode       string      `json:"cat_mode"`
	Env           string      `json:"env"`
	UpdatedWithin int         `json:"updated_within"`
	HideAbandoned bool        `json:"hide_abandoned"`
	Query         string      `json:"q"`
	Sort          string      `json:"sort"`
	Sources       []string    `json:"sources"`
	CFIndex       int         `json:"cf_index"`
	PageSize      int         `json:"page_size"`
}

type DependentsResponse struct {
	OK               bool              `json:"ok"`
	Items            []ProjectCard     `json:"items"`
	TotalCF          int64             `json:"total_cf"`
	CFScanned        int               `json:"cf_scanned,omitempty"`
	Count            int               `json:"count"`
	CountsByType     map[string]int    `json:"counts_by_type"`
	SkippedModpacks  int               `json:"skipped_modpacks"`
	NextCFIndex      int               `json:"next_cf_index"`
	HasMore          bool              `json:"has_more"`
	CFDependentsURL  string            `json:"cf_dependents_url"`
	Relation         string            `json:"relation"`
	ProjectFilter    string            `json:"project_filter"`
	SourceMode       string            `json:"source_mode"`
	CFWebBlocked     bool              `json:"cf_web_blocked"`
	CFConfigured     bool              `json:"cf_configured"`
	HasCFSource      bool              `json:"has_cf_source"`
	CurseForgeStatus string            `json:"curseforge_status"`
	Errors           map[string]string `json:"errors"`
}

type CategoryDef struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	MRTags  []string `json:"mr_tags"`
	CFSlugs []string `json:"cf_slugs"`
}

type AppConfig struct {
	Port                   int    `json:"port"`
	CurseForgeAPIKey       string `json:"curseforge_api_key"`
	MergeThreshold         int    `json:"merge_threshold"`
	ProxyURL               string `json:"proxy_url,omitempty"`
	AutoInstallMediumDeps  bool   `json:"auto_install_medium_deps,omitempty"`
	MaxConcurrentDownloads int    `json:"max_concurrent_downloads,omitempty"`
	MicrosoftClientID      string `json:"microsoft_client_id,omitempty"`
}

type PublicConfig struct {
	CurseForgeConfigured   bool      `json:"curseforge_configured"`
	CurseForgeKeyMasked    string    `json:"curseforge_key_masked"`
	MergeThreshold         int       `json:"merge_threshold"`
	ProxyURL               string    `json:"proxy_url"`
	ProxyDetected          ProxyInfo `json:"proxy_detected"`
	AutoInstallMediumDeps  bool      `json:"auto_install_medium_deps"`
	MaxConcurrentDownloads int       `json:"max_concurrent_downloads"`
	MicrosoftConfigured    bool      `json:"microsoft_configured"`
	MicrosoftClientID      string    `json:"microsoft_client_id"`
}

type DownloadTaskRecord struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Category        string `json:"category"`
	URL             string `json:"url"`
	DestPath        string `json:"dest_path"`
	TempPath        string `json:"temp_path"`
	TotalBytes      int64  `json:"total_bytes"`
	DownloadedBytes int64  `json:"downloaded_bytes"`
	ChecksumAlgo    string `json:"checksum_algo"`
	ChecksumValue   string `json:"checksum_value"`
	IntegrityNote   string `json:"integrity_note"`
	Status          string `json:"status"`
	ErrorMsg        string `json:"error_msg"`
	Attempts        int    `json:"attempts"`
	MaxRetries      int    `json:"max_retries"`
	SupportsRange   bool   `json:"supports_range"`
	MaxBytes        int64  `json:"max_bytes"`
	GroupID         string `json:"group_id"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

type JavaRuntimeRecord struct {
	ID             string `json:"id"`
	Component      string `json:"component"`
	Name           string `json:"name"`
	ExecutablePath string `json:"executable_path"`
	RootDir        string `json:"root_dir"`
	Version        string `json:"version"`
	MajorVersion   int    `json:"major_version"`
	Arch           string `json:"arch"`
	Bitness        int    `json:"bitness"`
	Vendor         string `json:"vendor"`
	Source         string `json:"source"` // managed | system | path | custom
	Managed        bool   `json:"managed"`
	Verified       bool   `json:"verified"`
	InstalledAt    int64  `json:"installed_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

type SavedPack struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	MCVersion string     `json:"mc_version"`
	Loader    string     `json:"loader"`
	CreatedAt string     `json:"created_at"`
	Items     []CartItem `json:"items"`
}

type IndexStatus struct {
	CatalogCount int  `json:"catalog_count"`
	RDepsCount   int  `json:"rdeps_count"`
	Syncing      bool `json:"syncing"`
}


type ProxyInfo struct {
	Mode    string `json:"mode"`
	Address string `json:"address"`
	Label   string `json:"label"`
}

type EndpointProbe struct {
	OK        bool   `json:"ok"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type NetworkCheckResult struct {
	OK         bool          `json:"ok"`
	Proxy      ProxyInfo     `json:"proxy"`
	Modrinth   EndpointProbe `json:"modrinth"`
	CurseForge EndpointProbe `json:"curseforge"`
}

type OverridesData struct {
	Merges [][]string `json:"merges"`
	Splits [][]string `json:"splits"`
}

type FilterCategoryItem struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Icon      string   `json:"icon"`
	Tags      []string `json:"tags"`
	Platforms []string `json:"platforms"`
}

type RDepEdge struct {
	DepKey       string
	DependentKey string
	DepType      string
}

type DepInstallItemStatus struct {
	ProjectID   string `json:"project_id"`
	Platform    string `json:"platform"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Source      string `json:"source"`
	Stage       string `json:"stage"`
	Filename    string `json:"filename,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	Folder      string `json:"folder,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type DepTreeNode struct {
	Platform     string        `json:"platform"`
	ProjectID    string        `json:"project_id"`
	SourceKey    string        `json:"source_key"`
	Type         string        `json:"type"`
	Title        string        `json:"title"`
	Slug         string        `json:"slug"`
	IconURL      string        `json:"icon_url"`
	URL          string        `json:"url"`
	Filename     string        `json:"filename"`
	DownloadURL  string        `json:"download_url"`
	IsAvailable  bool          `json:"is_available"`
	Folder       string        `json:"folder"`
	ContentType  string        `json:"content_type"`
	Source       string        `json:"source,omitempty"`
	ManifestFile string        `json:"manifest_file,omitempty"`
	Confidence   string        `json:"confidence,omitempty"`
	Evidence     string        `json:"evidence,omitempty"`
	TextOrigin   string        `json:"text_origin,omitempty"`
	Resolved     bool          `json:"resolved"`
	Children     []DepTreeNode `json:"children"`
}

type DepTreeRequest struct {
	Dependencies []DependencyRef `json:"dependencies"`
	MCVersion    string          `json:"mc_version,omitempty"`
	Loader       string          `json:"loader,omitempty"`
	FileURL      string          `json:"file_url,omitempty"`
	Platform     string          `json:"platform,omitempty"`
	ProjectID    string          `json:"project_id,omitempty"`
	Title        string          `json:"title,omitempty"`
	Summary      string          `json:"summary,omitempty"`
	Body         string          `json:"body,omitempty"`
	Changelog    string          `json:"changelog,omitempty"`
	MaxDepth     int             `json:"max_depth,omitempty"`
}

type DepTreeResult struct {
	Tree           []DepTreeNode     `json:"tree"`
	Unresolved     []UnresolvedDep   `json:"unresolved"`
	Warnings       []string          `json:"warnings"`
	Errors         map[string]string `json:"errors"`
	ManifestStatus string            `json:"manifest_status"`
	TextStatus     string            `json:"text_status"`
	Status         string            `json:"status"`
}

type DepResolveRequest struct {
	Dependencies          []DependencyRef `json:"dependencies"`
	MCVersion             string          `json:"mc_version"`
	Loader                string          `json:"loader"`
	FileURL               string          `json:"file_url,omitempty"`
	Platform              string          `json:"platform,omitempty"`
	ProjectID             string          `json:"project_id,omitempty"`
	Title                 string          `json:"title,omitempty"`
	Summary               string          `json:"summary,omitempty"`
	Body                  string          `json:"body,omitempty"`
	Changelog             string          `json:"changelog,omitempty"`
	AllowMediumConfidence bool            `json:"allow_medium_confidence,omitempty"`
	ExistingSourceKeys    []string        `json:"existing_source_keys"`
	ExistingFilenames     []string        `json:"existing_filenames"`
}

type DepResolveResult struct {
	Resolved       []CartItem             `json:"resolved"`
	NeedsConfirm   []DepTreeNode          `json:"needs_confirm,omitempty"`
	Unresolved     []UnresolvedDep        `json:"unresolved"`
	Warnings       []string               `json:"warnings"`
	Errors         map[string]string      `json:"errors"`
	ManifestStatus string                 `json:"manifest_status"`
	TextStatus     string                 `json:"text_status"`
	Status         string                 `json:"status"`
	ItemsStatus    []DepInstallItemStatus `json:"items_status,omitempty"`
	Diagnostics    []string               `json:"diagnostics,omitempty"`
}

type InstanceRecord struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Icon               string `json:"icon"`
	MCVersion          string `json:"mc_version"`
	InstanceType       string `json:"instance_type"` // vanilla | neoforge | fabric | forge | quilt | empty
	LoaderVersion      string `json:"loader_version"`
	RootDir            string `json:"root_dir"`
	GameDir            string `json:"game_dir"`
	JavaMode           string `json:"java_mode"` // auto | managed | custom
	JavaRuntimeID      string `json:"java_runtime_id"`
	JavaPath           string `json:"java_path"`
	MemoryMinMB        int    `json:"memory_min_mb"`
	MemoryMaxMB        int    `json:"memory_max_mb"`
	LaunchSettingsJSON string `json:"launch_settings_json"`
	LinkedPackID       string `json:"linked_pack_id"`
	LinkedPackName     string `json:"linked_pack_name"`
	Status             string `json:"status"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
	LastPlayedAt       int64  `json:"last_played_at"`
}

type InstancePackLinkRecord struct {
	InstanceID string `json:"instance_id"`
	PackID     string `json:"pack_id"`
	PackName   string `json:"pack_name"`
	LinkedAt   int64  `json:"linked_at"`
}

type InstalledVersionRecord struct {
	ID            string `json:"id"`
	InstanceID    string `json:"instance_id"`
	VersionID     string `json:"version_id"`
	MCVersion     string `json:"mc_version"`
	InstallType   string `json:"install_type"`
	Modloader     string `json:"modloader"`
	LoaderVersion string `json:"loader_version"`
	JavaRuntimeID string `json:"java_runtime_id"`
	JavaPath      string `json:"java_path"`
	JavaMajor     int    `json:"java_major"`
	MetadataPath  string `json:"metadata_path"`
	InstalledAt   int64  `json:"installed_at"`
	Verified      bool   `json:"verified"`
	VerifiedAt    int64  `json:"verified_at"`
	VerifyError   string `json:"verify_error"`
	Active        bool   `json:"active"`
}

// AccountRecord stores strictly non-sensitive Microsoft/Minecraft account metadata in SQLite.
// Secret tokens (MS access/refresh, Xbox, XSTS, Minecraft) are NEVER stored in SQLite.
type AccountRecord struct {
	ID                 string `json:"id"`
	AccountType        string `json:"account_type"` // microsoft | offline
	MicrosoftAccountID string `json:"microsoft_account_id"`
	XboxUserID         string `json:"xbox_user_id"`
	XboxGamertag       string `json:"xbox_gamertag"`
	MinecraftUUID      string `json:"minecraft_uuid"`
	MinecraftUsername  string `json:"minecraft_username"`
	ProfileName        string `json:"profile_name"`
	SkinURL            string `json:"skin_url"`
	SkinVariant        string `json:"skin_variant"`
	CapeURL            string `json:"cape_url"`
	CapeAlias          string `json:"cape_alias"`
	AuthStatus         string `json:"auth_status"`    // unauthorized | authenticating | authorized | reauth_required | service_error
	LicenseStatus      string `json:"license_status"` // unknown | checking | owned | not_owned | network_error | service_unavailable | invalid_response
	LastAuthAt         int64  `json:"last_auth_at"`
	LastLicenseCheckAt int64  `json:"last_license_check_at"`
	ProfileUpdatedAt   int64  `json:"profile_updated_at"`
	MSExpiresAt        int64  `json:"ms_expires_at"`
	XBLExpiresAt       int64  `json:"xbl_expires_at"`
	XSTSExpiresAt      int64  `json:"xsts_expires_at"`
	MCExpiresAt        int64  `json:"mc_expires_at"`
	VaultKey           string `json:"vault_key"`
	LastError          string `json:"last_error"`
	Active             bool   `json:"active"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

// GameSessionRecord stores launch and runtime session history in SQLite (strictly zero tokens/secrets).
type GameSessionRecord struct {
	ID            string `json:"id"`
	InstanceID    string `json:"instance_id"`
	InstanceName  string `json:"instance_name"`
	VersionID     string `json:"version_id"`
	MCVersion     string `json:"mc_version"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loader_version"`
	AccountID     string `json:"account_id"`
	Username      string `json:"username"`
	JavaPath      string `json:"java_path"`
	JavaVersion   string `json:"java_version"`
	PID           int    `json:"pid"`
	State         string `json:"state"`
	ExitCode      int    `json:"exit_code"`
	ExitReason    string `json:"exit_reason"`
	ErrorMsg      string `json:"error_msg"`
	LogFilePath   string `json:"log_file_path"`
	StartedAt     int64  `json:"started_at"`
	EndedAt       int64  `json:"ended_at"`
}

type ModpackRecord struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Author          string `json:"author"`
	Description     string `json:"description"`
	Version         string `json:"version"`
	Format          string `json:"format"`
	SourceType      string `json:"source_type"`
	SourceRef       string `json:"source_ref"`
	SourceProjectID string `json:"source_project_id"`
	SourceVersionID string `json:"source_version_id"`
	MCVersion       string `json:"mc_version"`
	Loader          string `json:"loader"`
	LoaderVersion   string `json:"loader_version"`
	InstanceID      string `json:"instance_id"`
	ManifestHash    string `json:"manifest_hash"`
	Status          string `json:"status"`
	LastError       string `json:"last_error"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	InstalledAt     int64  `json:"installed_at"`
}

type ModpackFileRecord struct {
	ModpackID       string `json:"modpack_id"`
	InstanceID      string `json:"instance_id"`
	RelPath         string `json:"rel_path"`
	Category        string `json:"category"`
	SourcePlatform  string `json:"source_platform"`
	SourceProjectID string `json:"source_project_id"`
	SourceFileID    string `json:"source_file_id"`
	SHA1            string `json:"sha1"`
	SHA256          string `json:"sha256"`
	SHA512          string `json:"sha512"`
	Size            int64  `json:"size"`
	Managed         bool   `json:"managed"`
	Optional        bool   `json:"optional"`
	Enabled         bool   `json:"enabled"`
	DownloadURL     string `json:"download_url"`
	UpdatedAt       int64  `json:"updated_at"`
}

type ModpackUpdateRecord struct {
	ID             string `json:"id"`
	ModpackID      string `json:"modpack_id"`
	FromVersion    string `json:"from_version"`
	ToVersion      string `json:"to_version"`
	AddedCount     int    `json:"added_count"`
	UpdatedCount   int    `json:"updated_count"`
	RemovedCount   int    `json:"removed_count"`
	PreservedCount int    `json:"preserved_count"`
	Status         string `json:"status"`
	Summary        string `json:"summary"`
	AppliedAt      int64  `json:"applied_at"`
}

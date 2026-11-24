package runtime

import "modhub/internal/domain"

// JavaRuntime describes a discovered or ModHub-managed Java Runtime Environment.
type JavaRuntime struct {
	ID             string `json:"id"`
	Component      string `json:"component,omitempty"` // e.g. jre-legacy | java-runtime-alpha | java-runtime-gamma | java-runtime-delta
	Name           string `json:"name"`
	ExecutablePath string `json:"executable_path"`
	RootDir        string `json:"root_dir"`
	Version        string `json:"version"`
	MajorVersion   int    `json:"major_version"`
	Arch           string `json:"arch"`    // x64 | arm64 | x86
	Bitness        int    `json:"bitness"` // 64 | 32
	Vendor         string `json:"vendor"`
	Source         string `json:"source"` // managed | path | system | custom
	Managed        bool   `json:"managed"`
	Verified       bool   `json:"verified"`
	Runnable       bool   `json:"runnable"`
	InUse          bool   `json:"in_use"`
	CompatibleMC   string `json:"compatible_mc"`
	Error          string `json:"error,omitempty"`
	InstalledAt    int64  `json:"installed_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

// AvailableRuntime describes an official Mojang Java Runtime available for installation.
type AvailableRuntime struct {
	Component    string `json:"component"`
	DisplayName  string `json:"display_name"`
	MajorVersion int    `json:"major_version"`
	Version      string `json:"version"`
	Released     string `json:"released,omitempty"`
	PlatformKey  string `json:"platform_key"`
	Arch         string `json:"arch"`
	CompatibleMC string `json:"compatible_mc"`
	ManifestURL  string `json:"manifest_url,omitempty"`
	Installed    bool   `json:"installed"`
	Installing   bool   `json:"installing"`
	InstalledID  string `json:"installed_id,omitempty"`
}

// StateSnapshot is returned to the UI via Wails Bindings / HTTP API.
type StateSnapshot struct {
	Runtimes             []JavaRuntime      `json:"runtimes"`
	Available            []AvailableRuntime `json:"available"`
	InstallingComponents []string           `json:"installing_components"`
	RuntimesDir          string             `json:"runtimes_dir"`
	PlatformKey          string             `json:"platform_key"`
	MetadataError        string             `json:"metadata_error,omitempty"`
}

// Store abstracts SQLite persistence for managed & custom Java runtimes.
type Store interface {
	UpsertJavaRuntime(rec domain.JavaRuntimeRecord) error
	ListJavaRuntimes() ([]domain.JavaRuntimeRecord, error)
	DeleteJavaRuntime(id string) error
}

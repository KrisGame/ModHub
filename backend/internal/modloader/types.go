package modloader

import (
	"context"

	"modhub/internal/domain"
	"modhub/internal/mcversion"
)

type LoaderType string

const (
	LoaderVanilla  LoaderType = "vanilla"
	LoaderFabric   LoaderType = "fabric"
	LoaderQuilt    LoaderType = "quilt"
	LoaderForge    LoaderType = "forge"
	LoaderNeoForge LoaderType = "neoforge"
)

type LoaderVersionInfo struct {
	Loader       LoaderType `json:"loader"`
	MCVersion    string     `json:"mc_version"`
	Version      string     `json:"version"`
	FullVersion  string     `json:"full_version,omitempty"`
	Stable       bool       `json:"stable"`
	Recommended  bool       `json:"recommended,omitempty"`
	Latest       bool       `json:"latest,omitempty"`
	InstallerURL string     `json:"installer_url,omitempty"`
	MetadataURL  string     `json:"metadata_url,omitempty"`
}

type LoaderVersionsResult struct {
	Loader     LoaderType          `json:"loader"`
	MCVersion  string              `json:"mc_version"`
	Compatible bool                `json:"compatible"`
	Versions   []LoaderVersionInfo `json:"versions"`
	Message    string              `json:"message,omitempty"`
}

type LoaderInstallContext struct {
	InstanceID    string
	MCVersion     string
	LoaderVersion string
	GroupID       string
	JavaExePath   string
	OnProgress    mcversion.ProgressCallback
}

type LoaderInstallResult struct {
	VersionID     string
	LoaderVersion string
	MetadataPath  string
	JavaMajor     int
	Libraries     int
}

type LoaderVerifyResult struct {
	Valid          bool     `json:"valid"`
	MetadataOK     bool     `json:"metadata_ok"`
	LibrariesTotal int      `json:"libraries_total"`
	LibrariesOK    int      `json:"libraries_ok"`
	OutputsOK      bool     `json:"outputs_ok"`
	MissingFiles   []string `json:"missing_files,omitempty"`
	Errors         []string `json:"errors,omitempty"`
}

type LoaderProvider interface {
	Type() LoaderType
	ListVersions(ctx context.Context, mcVersion string) (LoaderVersionsResult, error)
	Install(ctx context.Context, req LoaderInstallContext) (LoaderInstallResult, error)
	Verify(ctx context.Context, versionID string) LoaderVerifyResult
}

type ProcessRunner interface {
	RunJavaProcessor(ctx context.Context, javaExe string, workDir string, classpath []string, mainClass string, args []string) (string, error)
}

type InstallProfileVersionRequest struct {
	InstanceID    string     `json:"instance_id"`
	MCVersion     string     `json:"mc_version"`
	Loader        LoaderType `json:"loader"`
	LoaderVersion string     `json:"loader_version,omitempty"`
	CreateBackup  bool       `json:"create_backup,omitempty"`
}

type InstalledVersion struct {
	ID            string     `json:"id"`
	InstanceID    string     `json:"instance_id"`
	VersionID     string     `json:"version_id"`
	MCVersion     string     `json:"mc_version"`
	InstallType   LoaderType `json:"install_type"`
	Modloader     LoaderType `json:"modloader,omitempty"`
	LoaderVersion string     `json:"loader_version,omitempty"`
	JavaRuntimeID string     `json:"java_runtime_id,omitempty"`
	JavaPath      string     `json:"java_path,omitempty"`
	JavaMajor     int        `json:"java_major"`
	MetadataPath  string     `json:"metadata_path"`
	InstalledAt   int64      `json:"installed_at"`
	Verified      bool       `json:"verified"`
	VerifiedAt    int64      `json:"verified_at"`
	VerifyError   string     `json:"verify_error,omitempty"`
	Active        bool       `json:"active"`
}

type FullVerificationReport struct {
	InstalledID   string                       `json:"installed_id"`
	VersionID     string                       `json:"version_id"`
	MCVersion     string                       `json:"mc_version"`
	Loader        LoaderType                   `json:"loader"`
	LoaderVersion string                       `json:"loader_version,omitempty"`
	Valid         bool                         `json:"valid"`
	Vanilla       mcversion.VerificationReport `json:"vanilla"`
	LoaderCheck   *LoaderVerifyResult          `json:"loader_check,omitempty"`
	Errors        []string                     `json:"errors,omitempty"`
}

type Store interface {
	UpsertInstalledVersion(rec domain.InstalledVersionRecord) error
	ListInstalledVersions(instanceID string) ([]domain.InstalledVersionRecord, error)
	SetActiveInstalledVersion(instanceID, installedID string) error
	DeleteInstalledVersion(instanceID, installedID string) error
	CountVersionIDReferences(versionID string) int
}

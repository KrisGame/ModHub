package launcher

import "modhub/internal/domain"

type ProcessState string

const (
	StateCheckingProfile ProcessState = "checking_profile"
	StateCheckingAuth    ProcessState = "checking_auth"
	StatePreparingFiles  ProcessState = "preparing_files"
	StateBuildingCommand ProcessState = "building_command"
	StateStarting        ProcessState = "starting"
	StateRunning         ProcessState = "running"
	StateStopping        ProcessState = "stopping"
	StateExited          ProcessState = "exited"
	StateError           ProcessState = "error"
)

func (s ProcessState) IsActive() bool {
	switch s {
	case StateCheckingProfile, StateCheckingAuth, StatePreparingFiles, StateBuildingCommand, StateStarting, StateRunning, StateStopping:
		return true
	default:
		return false
	}
}

type LaunchRequest struct {
	InstanceID         string `json:"instance_id"`
	AccountID          string `json:"account_id,omitempty"`
	InstalledVersionID string `json:"installed_version_id,omitempty"`
}

type CrashReportInfo struct {
	Filename string `json:"filename"`
	FullPath string `json:"full_path"`
	ModTime  int64  `json:"mod_time"`
	Size     int64  `json:"size"`
	Summary  string `json:"summary,omitempty"`
}

type CrashDiagnosis struct {
	Code       string `json:"code"`
	Title      string `json:"title"`
	Cause      string `json:"cause"`
	Suggestion string `json:"suggestion"`
	RelatedMod string `json:"related_mod,omitempty"`
	ActionHint string `json:"action_hint,omitempty"` // settings | mods | java
}

type LogLine struct {
	SessionID  string `json:"session_id"`
	InstanceID string `json:"instance_id"`
	Seq        int64  `json:"seq"`
	Timestamp  string `json:"timestamp"`
	Stream     string `json:"stream"` // stdout | stderr | launcher
	Level      string `json:"level"`  // INFO | WARN | ERROR | FATAL | DEBUG
	Text       string `json:"text"`
}

type GameSession struct {
	ID               string            `json:"id"`
	InstanceID       string            `json:"instance_id"`
	InstanceName     string            `json:"instance_name"`
	VersionID        string            `json:"version_id"`
	MCVersion        string            `json:"mc_version"`
	Loader           string            `json:"loader"`
	LoaderVersion    string            `json:"loader_version,omitempty"`
	AccountID        string            `json:"account_id,omitempty"`
	Username         string            `json:"username,omitempty"`
	JavaPath         string            `json:"java_path,omitempty"`
	JavaVersion      string            `json:"java_version,omitempty"`
	PID              int               `json:"pid"`
	State            ProcessState      `json:"state"`
	StageMessage     string            `json:"stage_message,omitempty"`
	GameLoaded       bool              `json:"game_loaded"`
	ExitCode         int               `json:"exit_code"`
	ExitReason       string            `json:"exit_reason,omitempty"`
	Error            string            `json:"error,omitempty"`
	TechnicalDetails string            `json:"technical_details,omitempty"`
	LogFilePath      string            `json:"log_file_path,omitempty"`
	CrashReports     []CrashReportInfo `json:"crash_reports,omitempty"`
	CrashDiagnosis   *CrashDiagnosis   `json:"crash_diagnosis,omitempty"`
	StartedAt        int64             `json:"started_at"`
	EndedAt          int64             `json:"ended_at"`
}

type PreflightReport struct {
	OK               bool     `json:"ok"`
	InstanceID       string   `json:"instance_id"`
	VersionID        string   `json:"version_id,omitempty"`
	MCVersion        string   `json:"mc_version,omitempty"`
	Loader           string   `json:"loader,omitempty"`
	JavaSummary      string   `json:"java_summary,omitempty"`
	AccountSummary   string   `json:"account_summary,omitempty"`
	SystemRAMMB      int64    `json:"system_ram_mb"`
	RecommendedMaxMB int      `json:"recommended_max_mb"`
	FreeDiskMB       int64    `json:"free_disk_mb"`
	Errors           []string `json:"errors,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
}

type SystemMemoryInfo struct {
	TotalRAMMB       int64 `json:"total_ram_mb"`
	AvailableRAMMB   int64 `json:"available_ram_mb"`
	RecommendedMaxMB int   `json:"recommended_max_mb"`
}

// LaunchSpec holds the resolved process arguments. RawArgs contains the real launch arguments
// (never serialized to JSON or logged), while SanitizedArgs redacts all tokens for safe diagnostics.
type LaunchSpec struct {
	JavaExe        string   `json:"java_exe"`
	WorkDir        string   `json:"work_dir"`
	MainClass      string   `json:"main_class"`
	VersionID      string   `json:"version_id"`
	MCVersion      string   `json:"mc_version"`
	Loader         string   `json:"loader"`
	NativesDir     string   `json:"natives_dir"`
	ClasspathCount int      `json:"classpath_count"`
	MemoryMinMB    int      `json:"memory_min_mb"`
	MemoryMaxMB    int      `json:"memory_max_mb"`
	SanitizedArgs  []string `json:"sanitized_args"`
	RawArgs        []string `json:"-"`
	Warnings       []string `json:"warnings,omitempty"`
}

type LauncherSnapshot struct {
	Sessions []GameSession    `json:"sessions"`
	System   SystemMemoryInfo `json:"system"`
}

type Store interface {
	UpsertGameSession(rec domain.GameSessionRecord) error
	ListGameSessions(instanceID string, limit int) ([]domain.GameSessionRecord, error)
}

package downloads

import "modhub/internal/domain"

type Status string

const (
	StatusPending     Status = "pending"
	StatusQueued      Status = "queued"
	StatusDownloading Status = "downloading"
	StatusPaused      Status = "paused"
	StatusVerifying   Status = "verifying"
	StatusCompleted   Status = "completed"
	StatusCancelled   Status = "cancelled"
	StatusError       Status = "error"
)

type ChecksumAlgo string

const (
	ChecksumNone   ChecksumAlgo = ""
	ChecksumSHA1   ChecksumAlgo = "sha1"
	ChecksumSHA256 ChecksumAlgo = "sha256"
)

// DownloadRequest defines parameters for enqueuing a generic download job.
type DownloadRequest struct {
	ID            string       `json:"id,omitempty"`
	Name          string       `json:"name"`
	Category      string       `json:"category,omitempty"` // java | minecraft | mod | modloader | resourcepack | shader | generic
	URL           string       `json:"url"`
	DestPath      string       `json:"dest_path"`
	ExpectedSize  int64        `json:"expected_size,omitempty"`
	ChecksumAlgo  ChecksumAlgo `json:"checksum_algo,omitempty"`
	ChecksumValue string       `json:"checksum_value,omitempty"`
	MaxBytes      int64        `json:"max_bytes,omitempty"`
	MaxRetries    int          `json:"max_retries,omitempty"`
	GroupID       string       `json:"group_id,omitempty"`
}

// TaskView is an immutable UI/IPC snapshot of a single download task.
type TaskView struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Category         string  `json:"category"`
	URL              string  `json:"url"`
	DestPath         string  `json:"dest_path"`
	TotalBytes       int64   `json:"total_bytes"`
	DownloadedBytes  int64   `json:"downloaded_bytes"`
	SpeedBytesPerSec float64 `json:"speed_bytes_per_sec"`
	ETASeconds       int64   `json:"eta_seconds"` // -1 when unknown
	Progress         float64 `json:"progress"`    // 0..100
	Status           Status  `json:"status"`
	Error            string  `json:"error,omitempty"`
	ChecksumAlgo     string  `json:"checksum_algo,omitempty"`
	ChecksumValue    string  `json:"checksum_value,omitempty"`
	IntegrityNote    string  `json:"integrity_note,omitempty"`
	Verified         bool    `json:"verified"`
	SupportsRange    bool    `json:"supports_range"`
	Attempts         int     `json:"attempts"`
	MaxRetries       int     `json:"max_retries"`
	GroupID          string  `json:"group_id,omitempty"`
	CreatedAt        int64   `json:"created_at"`
	UpdatedAt        int64   `json:"updated_at"`
}

// QueueSummary aggregates metrics across all tasks for top-level indicators & DownloadsPage.
type QueueSummary struct {
	ActiveCount          int     `json:"active_count"`
	QueuedCount          int     `json:"queued_count"`
	PausedCount          int     `json:"paused_count"`
	CompletedCount       int     `json:"completed_count"`
	ErrorCount           int     `json:"error_count"`
	CancelledCount       int     `json:"cancelled_count"`
	TotalCount           int     `json:"total_count"`
	TotalBytes           int64   `json:"total_bytes"`
	DownloadedBytes      int64   `json:"downloaded_bytes"`
	OverallProgress      float64 `json:"overall_progress"`
	TotalSpeedBytesPerSec float64 `json:"total_speed_bytes_per_sec"`
	MaxConcurrent        int     `json:"max_concurrent"`
}

// Snapshot is returned by GetSnapshot() / API / Wails Bindings.
type Snapshot struct {
	Tasks   []TaskView   `json:"tasks"`
	Summary QueueSummary `json:"summary"`
}

// Store abstracts SQLite persistence for download tasks.
type Store interface {
	UpsertDownloadTask(rec domain.DownloadTaskRecord) error
	ListDownloadTasks() ([]domain.DownloadTaskRecord, error)
	DeleteDownloadTask(id string) error
	DeleteDownloadTasksByStatuses(statuses []string) error
}

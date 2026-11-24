package mcversion

import "encoding/json"

const OfficialVersionManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

type VersionManifestV2 struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []VersionSummary `json:"versions"`
}

type VersionSummary struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	URL             string `json:"url"`
	Time            string `json:"time"`
	ReleaseTime     string `json:"releaseTime"`
	SHA1            string `json:"sha1"`
	ComplianceLevel int    `json:"complianceLevel"`
	Installable     bool   `json:"installable"`
	Installed       bool   `json:"installed"`
}

type VersionManifestResult struct {
	LatestRelease  string           `json:"latest_release"`
	LatestSnapshot string           `json:"latest_snapshot"`
	Versions       []VersionSummary `json:"versions"`
	FromCache      bool             `json:"from_cache"`
	Stale          bool             `json:"stale"`
	FetchedAt      int64            `json:"fetched_at"`
	Warning        string           `json:"warning,omitempty"`
}

type DownloadArtifact struct {
	ID   string `json:"id,omitempty"`
	Path string `json:"path,omitempty"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type AssetIndexRef struct {
	ID        string `json:"id"`
	SHA1      string `json:"sha1"`
	Size      int64  `json:"size"`
	TotalSize int64  `json:"totalSize"`
	URL       string `json:"url"`
}

type JavaVersionSpec struct {
	Component    string `json:"component"`
	MajorVersion int    `json:"majorVersion"`
}

type RuleOS struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Arch    string `json:"arch,omitempty"`
}

type Rule struct {
	Action   string          `json:"action"`
	OS       *RuleOS         `json:"os,omitempty"`
	Features map[string]bool `json:"features,omitempty"`
}

type LibraryDownloads struct {
	Artifact    *DownloadArtifact           `json:"artifact,omitempty"`
	Classifiers map[string]DownloadArtifact `json:"classifiers,omitempty"`
}

type ExtractRule struct {
	Exclude []string `json:"exclude,omitempty"`
}

type Library struct {
	Name      string            `json:"name"`
	URL       string            `json:"url,omitempty"`
	Downloads *LibraryDownloads `json:"downloads,omitempty"`
	Natives   map[string]string `json:"natives,omitempty"`
	Extract   *ExtractRule      `json:"extract,omitempty"`
	Rules     []Rule            `json:"rules,omitempty"`
	SHA1      string            `json:"sha1,omitempty"`
	Size      int64             `json:"size,omitempty"`
}

type LoggingConfig struct {
	Client *struct {
		Argument string           `json:"argument"`
		File     DownloadArtifact `json:"file"`
		Type     string           `json:"type"`
	} `json:"client,omitempty"`
}

type ArgumentEntry struct {
	Rules  []Rule   `json:"rules,omitempty"`
	Values []string `json:"values"`
}

func (a *ArgumentEntry) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		a.Values = []string{single}
		return nil
	}
	var obj struct {
		Rules []Rule          `json:"rules"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	a.Rules = obj.Rules
	if len(obj.Value) > 0 {
		var s string
		if err := json.Unmarshal(obj.Value, &s); err == nil {
			a.Values = []string{s}
			return nil
		}
		var arr []string
		if err := json.Unmarshal(obj.Value, &arr); err == nil {
			a.Values = arr
			return nil
		}
	}
	return nil
}

func (a ArgumentEntry) MarshalJSON() ([]byte, error) {
	if len(a.Rules) == 0 && len(a.Values) == 1 {
		return json.Marshal(a.Values[0])
	}
	var val any = a.Values
	if len(a.Values) == 1 {
		val = a.Values[0]
	}
	return json.Marshal(struct {
		Rules []Rule `json:"rules,omitempty"`
		Value any    `json:"value"`
	}{Rules: a.Rules, Value: val})
}

type VersionArguments struct {
	Game []ArgumentEntry `json:"game,omitempty"`
	JVM  []ArgumentEntry `json:"jvm,omitempty"`
}

type VersionMetadata struct {
	ID                     string                      `json:"id"`
	InheritsFrom           string                      `json:"inheritsFrom,omitempty"`
	Type                   string                      `json:"type,omitempty"`
	Time                   string                      `json:"time,omitempty"`
	ReleaseTime            string                      `json:"releaseTime,omitempty"`
	MainClass              string                      `json:"mainClass,omitempty"`
	MinecraftArguments     string                      `json:"minecraftArguments,omitempty"`
	Arguments              *VersionArguments           `json:"arguments,omitempty"`
	Assets                 string                      `json:"assets,omitempty"`
	AssetIndex             *AssetIndexRef              `json:"assetIndex,omitempty"`
	Downloads              map[string]DownloadArtifact `json:"downloads,omitempty"`
	JavaVersion            *JavaVersionSpec            `json:"javaVersion,omitempty"`
	Libraries              []Library                   `json:"libraries,omitempty"`
	Logging                *LoggingConfig              `json:"logging,omitempty"`
	MinimumLauncherVersion int                         `json:"minimumLauncherVersion,omitempty"`
}

type AssetObject struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

type AssetIndex struct {
	Objects map[string]AssetObject `json:"objects"`
}

type ResolvedLibraryFile struct {
	Name     string
	RelPath  string
	URL      string
	SHA1     string
	Size     int64
	IsNative bool
	Extract  *ExtractRule
}

type ProgressCallback func(p InstallProgress)

type InstallProgress struct {
	InstanceID     string  `json:"instance_id,omitempty"`
	MCVersion      string  `json:"mc_version"`
	Loader         string  `json:"loader"`
	LoaderVersion  string  `json:"loader_version,omitempty"`
	Stage          string  `json:"stage"`
	Message        string  `json:"message"`
	Component      string  `json:"component,omitempty"`
	CompletedFiles int     `json:"completed_files"`
	TotalFiles     int     `json:"total_files"`
	Progress       float64 `json:"progress"`
	SpeedBps       float64 `json:"speed_bps"`
	Error          string  `json:"error,omitempty"`
	UpdatedAt      int64   `json:"updated_at"`
}

type VerificationReport struct {
	VersionID         string   `json:"version_id"`
	Valid             bool     `json:"valid"`
	MetadataOK        bool     `json:"metadata_ok"`
	ClientJarOK       bool     `json:"client_jar_ok"`
	LibrariesTotal    int      `json:"libraries_total"`
	LibrariesOK       int      `json:"libraries_ok"`
	AssetIndexOK      bool     `json:"asset_index_ok"`
	AssetsTotal       int      `json:"assets_total"`
	AssetsOK          int      `json:"assets_ok"`
	LoggingConfigOK   bool     `json:"logging_config_ok"`
	JavaRequiredMajor int      `json:"java_required_major"`
	JavaAvailable     bool     `json:"java_available"`
	JavaSummary       string   `json:"java_summary"`
	MissingFiles      []string `json:"missing_files,omitempty"`
	Errors            []string `json:"errors,omitempty"`
}

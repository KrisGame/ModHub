export type ContentType = "mod" | "resourcepack" | "shader" | "datapack";
export type SortMode = "downloads" | "relevance" | "updated" | "newest";
export type ViewMode = "grid" | "list";
export type ThemeMode = "dark" | "light";
export type AccentPreset = "mono" | "graphite" | "emerald" | "amber" | "indigo";
export type BgPreset = "dots" | "grid" | "noise" | "clean";

export interface ProjectLinks {
  source_url: string;
  issues_url: string;
  wiki_url: string;
  discord_url: string;
}

export interface ProjectSource {
  platform: string;
  source_key: string;
  id: string;
  slug: string;
  title: string;
  url: string;
  downloads: number;
  authors: string[];
  loaders: string[];
  project_type: string;
}

export interface DependentInfo {
  relation: string;
  label: string;
  order: number;
}

export interface ProjectCard {
  group_id: string;
  content_type: string;
  title: string;
  slug: string;
  summary: string;
  authors: string[];
  icon_url: string;
  downloads: number;
  date_modified: string;
  date_created?: string;
  license: string;
  status: string;
  client_side: string;
  server_side: string;
  loaders: string[];
  game_versions: string[];
  categories: string[];
  platforms: string[];
  is_merged: boolean;
  sources: ProjectSource[];
  links: ProjectLinks;
  dependent_info?: DependentInfo;
}

export interface DependencyRef {
  platform: string;
  project_id: string;
  version_id: string;
  type: string;
}

export interface VersionFile {
  platform: string;
  source_key: string;
  version_id: string;
  mod_id: string;
  name: string;
  version_number: string;
  release_type: string;
  date_published: string;
  downloads: number;
  loaders: string[];
  game_versions: string[];
  filename: string;
  file_size: number;
  download_url: string;
  fallback_url: string;
  is_available: boolean;
  changelog: string;
  dependencies: DependencyRef[];
}

export interface GalleryItem {
  url: string;
  title: string;
  description: string;
}

export interface SourceDetail {
  platform: string;
  source_key: string;
  id: string;
  slug: string;
  title: string;
  url: string;
  downloads: number;
  links: ProjectLinks;
  allow_dist: boolean;
}

export interface ModDetailsResponse {
  ok: boolean;
  merged_item?: ProjectCard;
  links: ProjectLinks;
  license: string;
  sources_details: SourceDetail[];
  body: string;
  body_format: string;
  gallery: GalleryItem[];
  meta_loaders: string[];
  meta_game_versions: string[];
  platforms: string[];
  versions?: VersionFile[];
  versions_facets?: {
    loaders: string[];
    game_versions: string[];
    channels: string[];
  };
  source_statuses?: Record<string, string>;
  errors: Record<string, string>;
}

export interface ModVersionsResponse {
  ok: boolean;
  items: VersionFile[];
  merged_item?: ProjectCard | null;
  page: number;
  limit: number;
  total: number;
  has_more: boolean;
  facets: {
    loaders: string[];
    game_versions: string[];
    channels: string[];
  };
}

export interface CartItem {
  title: string;
  icon_url: string;
  platform: string;
  source_key: string;
  source_keys: string[];
  group_id: string;
  filename: string;
  file_size: number;
  download_url: string;
  fallback_url: string;
  is_available: boolean;
  folder: string;
  loaders: string[];
  game_versions: string[];
  mc_version: string;
  loader: string;
  auto_dep_for: string[];
}

export interface QuickFileResponse {
  ok: boolean;
  file: CartItem | null;
  resolved_deps: CartItem[];
  unresolved_deps?: UnresolvedDep[];
  needs_confirm_deps?: DepTreeNode[];
  required_deps?: DependencyRef[];
  dep_warnings?: string[];
  dep_errors?: Record<string, string>;
  dep_status?: DepResolutionStatus;
  manifest_status?: string;
  merged_item?: ProjectCard;
  reason?: string;
}

export interface SearchResponse {
  ok: boolean;
  source_mode: string;
  items: ProjectCard[];
  totals: Record<string, number>;
  has_more: boolean;
  next_mr_offset: number;
  next_cf_offset: number;
  curseforge_status: string;
  fuzzy_suggestion: string;
  has_operators: boolean;
  errors: Record<string, string>;
  timing_ms?: { upstream: number; merge: number; total: number };
}

export interface DependentsResponse {
  ok: boolean;
  items: ProjectCard[];
  total_cf: number;
  cf_scanned?: number;
  count: number;
  counts_by_type?: Record<string, number>;
  skipped_modpacks: number;
  next_cf_index: number;
  has_more: boolean;
  cf_dependents_url: string;
  relation: string;
  project_filter: string;
  source_mode: string;
  cf_web_blocked: boolean;
  cf_configured: boolean;
  has_cf_source: boolean;
  curseforge_status: string;
  errors: Record<string, string>;
}

export interface FilterCategory {
  id: string;
  label: string;
  icon: string;
  tags: string[];
  platforms: string[];
}

export interface ProxyInfo {
  mode: "manual" | "env" | "windows_system" | "direct" | string;
  address: string;
  label: string;
}

export interface EndpointProbe {
  ok: boolean;
  status: string;
  latency_ms: number;
  error?: string;
}

export interface NetworkCheckResponse {
  ok: boolean;
  proxy: ProxyInfo;
  modrinth: EndpointProbe;
  curseforge: EndpointProbe;
}

export interface PublicConfig {
  curseforge_configured: boolean;
  curseforge_key_masked: string;
  merge_threshold: number;
  proxy_url?: string;
  proxy_detected?: ProxyInfo;
  auto_install_medium_deps?: boolean;
  max_concurrent_downloads?: number;
  microsoft_configured?: boolean;
  microsoft_client_id?: string;
  port?: number;
}

export interface SavedPack {
  id: string;
  name: string;
  mc_version: string;
  loader: string;
  created_at: string;
  items: CartItem[];
}

export interface InstalledModInfo {
  installed_version_id: string;
  installed_version_number: string;
  has_update: boolean;
  latest_version_number: string;
  filename: string;
}

export interface ToastMessage {
  id: number;
  text: string;
  kind: "info" | "ok" | "err";
  leaving?: boolean;
}

export type DepResolutionStatus = "none" | "complete" | "partial" | "error";

export interface UnresolvedDep {
  mod_id: string;
  platform?: string;
  project_id?: string;
  type: string;
  source?: string;
  reason: string;
}

export interface DepTreeNode {
  platform: string;
  project_id: string;
  source_key: string;
  type: string;
  title: string;
  slug: string;
  icon_url: string;
  url: string;
  filename: string;
  download_url: string;
  is_available: boolean;
  folder: string;
  content_type?: string;
  source?: "api" | "manifest" | "text" | "api+manifest" | "api+text";
  manifest_file?: string;
  confidence?: "high" | "medium" | "low";
  evidence?: string;
  text_origin?: string;
  resolved?: boolean;
  children: DepTreeNode[];
}

export interface DepTreeResponse {
  ok: true;
  tree: DepTreeNode[];
  unresolved?: UnresolvedDep[];
  warnings?: string[];
  errors?: Record<string, string>;
  manifest_status?: string;
  text_status?: string;
  status?: DepResolutionStatus;
}

export interface DepInstallItemStatus {
  project_id: string;
  platform: string;
  title: string;
  type: string;
  source: string;
  stage:
    | "detected"
    | "version_found"
    | "ready"
    | "downloaded"
    | "failed"
    | "unresolved"
    | "already_installed"
    | "skipped"
    | "incompatible";
  filename?: string;
  download_url?: string;
  folder?: string;
  reason?: string;
}

export interface DepResolveResponse {
  ok: true;
  resolved: CartItem[];
  needs_confirm?: DepTreeNode[];
  unresolved?: UnresolvedDep[];
  warnings?: string[];
  errors?: Record<string, string>;
  manifest_status?: string;
  text_status?: string;
  status?: DepResolutionStatus;
  items_status?: DepInstallItemStatus[];
  diagnostics?: string[];
}

export interface PackDownloadReport {
  blob: Blob;
  downloaded: number;
  total: number;
  failed: number;
  status: "complete" | "partial";
  failedReasons: string[];
}

export type DownloadStatus =
  | "pending"
  | "queued"
  | "downloading"
  | "paused"
  | "verifying"
  | "completed"
  | "cancelled"
  | "error";

export interface DownloadRequestInput {
  id?: string;
  name: string;
  category?: string;
  url: string;
  dest_path: string;
  expected_size?: number;
  checksum_algo?: "" | "sha1" | "sha256";
  checksum_value?: string;
  max_bytes?: number;
  max_retries?: number;
  group_id?: string;
}

export interface DownloadTaskView {
  id: string;
  name: string;
  category: string;
  url: string;
  dest_path: string;
  total_bytes: number;
  downloaded_bytes: number;
  speed_bytes_per_sec: number;
  eta_seconds: number;
  progress: number;
  status: DownloadStatus;
  error?: string;
  checksum_algo?: string;
  checksum_value?: string;
  integrity_note?: string;
  verified: boolean;
  supports_range: boolean;
  attempts: number;
  max_retries: number;
  group_id?: string;
  created_at: number;
  updated_at: number;
}

export interface DownloadQueueSummary {
  active_count: number;
  queued_count: number;
  paused_count: number;
  completed_count: number;
  error_count: number;
  cancelled_count: number;
  total_count: number;
  total_bytes: number;
  downloaded_bytes: number;
  overall_progress: number;
  total_speed_bytes_per_sec: number;
  max_concurrent: number;
}

export interface DownloadsSnapshot {
  tasks: DownloadTaskView[];
  summary: DownloadQueueSummary;
}

export interface JavaRuntimeInfo {
  id: string;
  component?: string;
  name: string;
  executable_path: string;
  root_dir: string;
  version: string;
  major_version: number;
  arch: string;
  bitness: number;
  vendor: string;
  source: "managed" | "path" | "system" | "custom" | string;
  managed: boolean;
  verified: boolean;
  runnable: boolean;
  in_use: boolean;
  compatible_mc: string;
  error?: string;
  installed_at: number;
  updated_at: number;
}

export interface AvailableJavaRuntime {
  component: string;
  display_name: string;
  major_version: number;
  version: string;
  released?: string;
  platform_key: string;
  arch: string;
  compatible_mc: string;
  manifest_url?: string;
  installed: boolean;
  installing: boolean;
  installed_id?: string;
}

export interface JavaStateSnapshot {
  runtimes: JavaRuntimeInfo[];
  available: AvailableJavaRuntime[];
  installing_components: string[];
  runtimes_dir: string;
  platform_key: string;
  metadata_error?: string;
}

export interface JavaRequirementResult {
  recommended_major: number;
  min_major: number;
  max_major: number;
  preferred_component: string;
  reason: string;
  source: string;
}

export interface JavaRecommendationResponse {
  requirement: JavaRequirementResult;
  recommended?: JavaRuntimeInfo | null;
}

export type InstanceStatus = "creating" | "ready" | "needs_setup" | "corrupted" | "deleting";
export type InstanceType = "empty" | "vanilla" | "neoforge" | "fabric" | "forge" | "quilt";
export type InstanceJavaMode = "auto" | "managed" | "custom";

export interface InstanceLaunchSettings {
  jvm_args?: string;
  window_width?: number;
  window_height?: number;
  fullscreen?: boolean;
  active_version_id?: string;
  account_id?: string;
}

export interface InstalledModRef {
  filename: string;
  size: number;
  enabled: boolean;
}

export interface ResourcePackRef {
  filename: string;
  name: string;
  description: string;
  pack_format: number;
  size: number;
  enabled: boolean;
  icon_data_url?: string;
}

export interface ShaderPackRef {
  filename: string;
  name: string;
  size: number;
  enabled: boolean;
}

export interface WorldRef {
  folder: string;
  name: string;
  game_type: "survival" | "creative" | "adventure" | "spectator" | "hardcore" | string;
  hardcore: boolean;
  version: string;
  last_played: number; // unix seconds
  size: number;
  icon_data_url?: string;
}

export interface ExportWorldResult {
  archive_path: string;
  filename: string;
  size: number;
}

export interface ModUpdateInfo {
  current_filename: string;
  current_sha1: string;
  current_version?: string;
  project_id?: string;
  project_title?: string;
  new_version_id?: string;
  new_version_number?: string;
  new_filename?: string;
  new_download_url?: string;
  new_size?: number;
  new_sha1?: string;
  release_type?: string;
  has_update: boolean;
}

export interface ApplyModUpdateInput {
  id: string;
  current_filename: string;
  new_filename: string;
  new_url: string;
  new_size?: number;
  new_sha1?: string;
}

export interface BatchModActionInput {
  id: string;
  filenames?: string[];
  enabled?: boolean;
}

export interface InstallInstanceModInput {
  id: string;
  title?: string;
  filename: string;
  url: string;
  fallback_url?: string;
  size?: number;
  sha1?: string;
  sha256?: string;
  category?: string;
  project_id?: string;
  version_id?: string;
  group_id?: string;
  replace?: boolean;
}

export interface InstanceModInstallResult {
  task_id: string;
  filename: string;
  dest_path: string;
}

export interface InstanceIntegrityIssue {
  code: string;
  severity: "missing_component" | "corruption" | "warning" | string;
  message: string;
}

export interface InstanceIntegrityReport {
  instance_id: string;
  status: InstanceStatus;
  root_exists: boolean;
  manifest_valid: boolean;
  game_dir_exists: boolean;
  subdirs_ok: boolean;
  missing_subdirs?: string[];
  shared_resources_ok: boolean;
  java_ok: boolean;
  java_summary: string;
  game_files_installed: boolean;
  mods_count: number;
  issues: InstanceIntegrityIssue[];
  checked_at: number;
}

export interface GameInstance {
  id: string;
  name: string;
  description: string;
  icon: string;
  mc_version: string;
  instance_type: InstanceType;
  loader_version?: string;
  root_dir: string;
  game_dir: string;
  java_mode: InstanceJavaMode;
  java_runtime_id?: string;
  java_path?: string;
  java_requirement?: JavaRequirementResult | null;
  java_resolved?: JavaRuntimeInfo | null;
  memory_min_mb: number;
  memory_max_mb: number;
  launch_settings: InstanceLaunchSettings;
  linked_pack_id?: string;
  linked_pack_name?: string;
  linked_pack_items?: number;
  installed_mods: InstalledModRef[];
  mods_count: number;
  status: InstanceStatus;
  issues: InstanceIntegrityIssue[];
  in_use: boolean;
  created_at: number;
  updated_at: number;
  last_played_at: number;
}

export interface SharedStoragePaths {
  shared_dir: string;
  assets_dir: string;
  libraries_dir: string;
  versions_dir: string;
  runtimes_dir: string;
  instances_dir: string;
}

export interface InstancesSnapshot {
  instances: GameInstance[];
  paths: SharedStoragePaths;
}

export interface CreateInstanceInput {
  name: string;
  description?: string;
  icon?: string;
  mc_version?: string;
  instance_type?: InstanceType;
  loader_version?: string;
  custom_dir?: string;
  java_mode?: InstanceJavaMode;
  java_runtime_id?: string;
  java_path?: string;
  memory_min_mb?: number;
  memory_max_mb?: number;
  launch_settings?: InstanceLaunchSettings;
  linked_pack_id?: string;
}

export interface UpdateInstanceInput {
  id: string;
  name?: string;
  description?: string;
  icon?: string;
  mc_version?: string;
  instance_type?: InstanceType;
  loader_version?: string;
  java_mode?: InstanceJavaMode;
  java_runtime_id?: string;
  java_path?: string;
  memory_min_mb?: number;
  memory_max_mb?: number;
  launch_settings?: InstanceLaunchSettings;
  linked_pack_id?: string;
}

export interface DuplicateInstanceInput {
  source_id: string;
  new_name: string;
  description?: string;
  copy_settings: boolean;
  copy_mods: boolean;
  copy_configs: boolean;
  copy_resourcepacks: boolean;
  copy_shaderpacks: boolean;
  copy_saves: boolean;
}

export interface DeleteInstanceInput {
  id: string;
  delete_files: boolean;
}

export interface InstanceFolderStat {
  name: string;
  rel_path: string;
  full_path: string;
  exists: boolean;
  file_count: number;
  total_bytes: number;
}

export interface InstanceFilesSummary {
  instance_id: string;
  root_dir: string;
  game_dir: string;
  shared_dir: string;
  runtimes_dir: string;
  folders: InstanceFolderStat[];
  total_bytes: number;
}

export type ModloaderKind = "vanilla" | "fabric" | "quilt" | "forge" | "neoforge";

export interface MCVersionSummary {
  id: string;
  type: "release" | "snapshot" | "old_beta" | "old_alpha" | string;
  url: string;
  time: string;
  releaseTime: string;
  sha1: string;
  complianceLevel?: number;
  installable: boolean;
  installed: boolean;
}

export interface MCVersionManifestResult {
  latest_release: string;
  latest_snapshot: string;
  versions: MCVersionSummary[];
  from_cache: boolean;
  stale: boolean;
  fetched_at: number;
  warning?: string;
}

export interface LoaderVersionItem {
  loader: ModloaderKind;
  mc_version: string;
  version: string;
  full_version?: string;
  stable: boolean;
  recommended?: boolean;
  latest?: boolean;
  installer_url?: string;
  metadata_url?: string;
}

export interface LoaderVersionsResult {
  loader: ModloaderKind;
  mc_version: string;
  compatible: boolean;
  versions: LoaderVersionItem[];
  message?: string;
}

export interface InstallProfileVersionInput {
  instance_id: string;
  mc_version: string;
  loader: ModloaderKind;
  loader_version?: string;
  create_backup?: boolean;
}

export interface InstallProgressInfo {
  instance_id?: string;
  mc_version: string;
  loader: string;
  loader_version?: string;
  stage: string;
  message: string;
  component?: string;
  completed_files: number;
  total_files: number;
  progress: number;
  speed_bps: number;
  error?: string;
  updated_at: number;
}

export interface InstalledProfileVersion {
  id: string;
  instance_id: string;
  version_id: string;
  mc_version: string;
  install_type: ModloaderKind;
  modloader?: ModloaderKind | "";
  loader_version?: string;
  java_runtime_id?: string;
  java_path?: string;
  java_major: number;
  metadata_path: string;
  installed_at: number;
  verified: boolean;
  verified_at: number;
  verify_error?: string;
  active: boolean;
}

export interface MCVerificationReport {
  version_id: string;
  valid: boolean;
  metadata_ok: boolean;
  client_jar_ok: boolean;
  libraries_total: number;
  libraries_ok: number;
  asset_index_ok: boolean;
  assets_total: number;
  assets_ok: number;
  logging_config_ok: boolean;
  java_required_major: number;
  java_available: boolean;
  java_summary: string;
  missing_files?: string[];
  errors?: string[];
}

export interface LoaderVerifyReport {
  valid: boolean;
  metadata_ok: boolean;
  libraries_total: number;
  libraries_ok: number;
  outputs_ok: boolean;
  missing_files?: string[];
  errors?: string[];
}

export interface FullVersionVerificationReport {
  installed_id: string;
  version_id: string;
  mc_version: string;
  loader: ModloaderKind;
  loader_version?: string;
  valid: boolean;
  vanilla: MCVerificationReport;
  loader_check?: LoaderVerifyReport | null;
  errors?: string[];
}

export type AccountAuthStatus =
  | "unauthorized"
  | "authenticating"
  | "authorized"
  | "reauth_required"
  | "service_error";

export type AccountLicenseStatus =
  | "unknown"
  | "checking"
  | "owned"
  | "not_owned"
  | "network_error"
  | "service_unavailable"
  | "invalid_response";

export interface AccountView {
  id: string;
  account_type?: "microsoft" | "offline";
  microsoft_account_id?: string;
  xbox_user_id?: string;
  xbox_gamertag?: string;
  minecraft_uuid?: string;
  formatted_uuid?: string;
  minecraft_username?: string;
  profile_name?: string;
  skin_url?: string;
  skin_variant?: string;
  cape_url?: string;
  cape_alias?: string;
  auth_status: AccountAuthStatus;
  license_status: AccountLicenseStatus;
  profile_created: boolean;
  last_auth_at: number;
  last_license_check_at: number;
  profile_updated_at: number;
  ms_expires_at: number;
  xbl_expires_at: number;
  xsts_expires_at: number;
  mc_expires_at: number;
  has_vault_secret: boolean;
  last_error?: string;
  active: boolean;
  created_at: number;
  updated_at: number;
}

export interface AuthFlowProgress {
  active: boolean;
  account_id?: string;
  stage: string;
  message: string;
  error?: string;
  redirect_uri?: string;
  auth_url?: string;
  updated_at: number;
}

export interface AuthConfigStatus {
  configured: boolean;
  source: "env" | "config" | "build" | "none" | string;
  vault_backend: string;
  redirect_note: string;
  diagnostic?: string;
}

export interface AccountsSnapshot {
  accounts: AccountView[];
  active_account_id?: string;
  config: AuthConfigStatus;
  flow: AuthFlowProgress;
}

export type GameProcessState =
  | "checking_profile"
  | "checking_auth"
  | "preparing_files"
  | "building_command"
  | "starting"
  | "running"
  | "stopping"
  | "exited"
  | "error";

export interface LaunchInstanceInput {
  instance_id: string;
  account_id?: string;
  installed_version_id?: string;
}

export interface CrashReportInfo {
  filename: string;
  full_path: string;
  mod_time: number;
  size: number;
  summary?: string;
}

export interface GameLogLine {
  session_id: string;
  instance_id: string;
  seq: number;
  timestamp: string;
  stream: "stdout" | "stderr" | "launcher" | string;
  level: "INFO" | "WARN" | "ERROR" | "FATAL" | "DEBUG" | string;
  text: string;
}

export interface CrashDiagnosis {
  code: string;
  title: string;
  cause: string;
  suggestion: string;
  related_mod?: string;
  action_hint?: string;
}

export interface GameSessionInfo {
  id: string;
  instance_id: string;
  instance_name: string;
  version_id: string;
  mc_version: string;
  loader: string;
  loader_version?: string;
  account_id?: string;
  username?: string;
  java_path?: string;
  java_version?: string;
  pid: number;
  state: GameProcessState;
  stage_message?: string;
  game_loaded: boolean;
  exit_code: number;
  exit_reason?: string;
  error?: string;
  technical_details?: string;
  log_file_path?: string;
  crash_reports?: CrashReportInfo[];
  crash_diagnosis?: CrashDiagnosis;
  started_at: number;
  ended_at: number;
}

export interface PreflightReport {
  ok: boolean;
  instance_id: string;
  version_id?: string;
  mc_version?: string;
  loader?: string;
  java_summary?: string;
  account_summary?: string;
  system_ram_mb: number;
  recommended_max_mb: number;
  free_disk_mb: number;
  errors?: string[];
  warnings?: string[];
}

export interface SystemMemoryInfo {
  total_ram_mb: number;
  available_ram_mb: number;
  recommended_max_mb: number;
}

export interface LauncherSnapshot {
  sessions: GameSessionInfo[];
  system: SystemMemoryInfo;
}

export type ModpackFormat = "mrpack" | "curseforge" | "modhub";
export type ComponentCategory = "mod" | "addon" | "resourcepack" | "shader" | "datapack" | "config" | "userfile";
export type ConflictResolution = "keep_existing" | "overwrite" | "backup_and_overwrite";

export interface ImportCounts {
  mods: number;
  addons: number;
  resourcepacks: number;
  shaders: number;
  datapacks: number;
  configs: number;
  user_files: number;
  total_files: number;
  optional_files: number;
  unavailable: number;
  server_skipped: number;
}

export interface ModpackFilePlan {
  rel_path: string;
  title: string;
  filename: string;
  category: ComponentCategory;
  source_kind: string;
  archive_entry?: string;
  source_platform?: string;
  source_project_id?: string;
  source_file_id?: string;
  download_urls?: string[];
  sha1?: string;
  sha256?: string;
  sha512?: string;
  size: number;
  optional: boolean;
  selected_by_default: boolean;
  available: boolean;
  unavailable_reason?: string;
  suspicious?: boolean;
  suspicious_reason?: string;
}

export interface ImportConflict {
  code: string;
  path?: string;
  message: string;
}

export interface PreviewImportInput {
  source_type: "file" | "url" | "modrinth" | "curseforge" | "pack" | "instance";
  source_ref: string;
  version_id?: string;
}

export interface ModHubPackProfileParams {
  memory_min_mb?: number;
  memory_max_mb?: number;
  jvm_args?: string;
  window_width?: number;
  window_height?: number;
  fullscreen?: boolean;
}

export interface ImportPreview {
  preview_id: string;
  format: ModpackFormat;
  source_type: string;
  source_ref: string;
  source_project_id?: string;
  source_version_id?: string;
  resolved_archive_path?: string;
  name: string;
  author?: string;
  description?: string;
  version: string;
  mc_version: string;
  loader: string;
  loader_version?: string;
  manifest_hash: string;
  estimated_bytes: number;
  counts: ImportCounts;
  files: ModpackFilePlan[];
  unavailable_files?: ModpackFilePlan[];
  optional_files?: ModpackFilePlan[];
  conflicts?: ImportConflict[];
  warnings?: string[];
  profile_settings?: ModHubPackProfileParams | null;
}

export interface ExecuteImportInput {
  preview_id: string;
  profile_name?: string;
  description?: string;
  icon?: string;
  custom_dir?: string;
  memory_max_mb?: number;
  selected_optional?: string[];
  skip_unavailable: boolean;
  conflict_policy?: ConflictResolution;
  install_minecraft: boolean;
  target_instance_id?: string;
}

export interface ModpackImportProgress {
  modpack_id?: string;
  instance_id?: string;
  name: string;
  stage: string;
  message: string;
  completed_files: number;
  total_files: number;
  progress: number;
  error?: string;
  updated_at: number;
}

export interface ExportModpackInput {
  source_type: "instance" | "pack";
  source_id: string;
  format: ModpackFormat;
  output_path?: string;
  name?: string;
  author?: string;
  description?: string;
  version?: string;
  include_mods: boolean;
  include_configs: boolean;
  include_resourcepacks: boolean;
  include_shaders: boolean;
  include_datapacks: boolean;
  include_screenshots: boolean;
  include_saves: boolean;
  include_extra_files: boolean;
}

export interface ExportModpackResult {
  cancelled: boolean;
  format: ModpackFormat;
  output_path?: string;
  total_files: number;
  size_bytes: number;
  warnings?: string[];
}

export interface UpdateDiffEntry {
  rel_path: string;
  title?: string;
  category: ComponentCategory;
  action: "add" | "update" | "remove" | "config_conflict" | "preserve_user" | string;
  old_hash?: string;
  new_hash?: string;
  user_edited?: boolean;
  reason?: string;
}

export interface UpdateDiffPreview {
  modpack_id: string;
  preview_id: string;
  current_version: string;
  new_version: string;
  has_update: boolean;
  source_supported: boolean;
  source_note?: string;
  added: UpdateDiffEntry[];
  updated: UpdateDiffEntry[];
  removed: UpdateDiffEntry[];
  config_conflicts: UpdateDiffEntry[];
  preserved_user: UpdateDiffEntry[];
  unavailable_files?: ModpackFilePlan[];
  warnings?: string[];
}

export interface ApplyUpdateInput {
  modpack_id: string;
  preview_id: string;
  config_conflict_mode?: ConflictResolution;
  skip_unavailable: boolean;
}

export interface ModpackUpdateRecord {
  id: string;
  modpack_id: string;
  from_version: string;
  to_version: string;
  added_count: number;
  updated_count: number;
  removed_count: number;
  preserved_count: number;
  status: string;
  summary: string;
  applied_at: number;
}

export interface ModpackView {
  id: string;
  name: string;
  author?: string;
  description?: string;
  version: string;
  format: ModpackFormat;
  source_type: string;
  source_ref?: string;
  source_project_id?: string;
  source_version_id?: string;
  mc_version: string;
  loader: string;
  loader_version?: string;
  instance_id?: string;
  status: string;
  last_error?: string;
  files_count: number;
  managed_count: number;
  can_check_remote: boolean;
  created_at: number;
  updated_at: number;
  installed_at: number;
  update_history?: ModpackUpdateRecord[];
}

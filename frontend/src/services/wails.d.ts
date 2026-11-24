import type {
  AccountsSnapshot,
  AccountView,
  CartItem,
  CrashReportInfo,
  CrashDiagnosis,
  CreateInstanceInput,
  DeleteInstanceInput,
  DownloadRequestInput,
  DuplicateInstanceInput,
  DownloadsSnapshot,
  DownloadTaskView,
  FilterCategory,
  FullVersionVerificationReport,
  GameInstance,
  GameLogLine,
  GameSessionInfo,
  InstalledModInfo,
  InstalledProfileVersion,
  InstallProfileVersionInput,
  InstallInstanceModInput,
  InstanceModInstallResult,
  InstalledModRef,
  ResourcePackRef,
  ShaderPackRef,
  WorldRef,
  ExportWorldResult,
  ModUpdateInfo,
  ApplyModUpdateInput,
  InstallProgressInfo,
  InstanceFilesSummary,
  InstanceIntegrityReport,
  InstancesSnapshot,
  JavaRecommendationResponse,
  JavaRuntimeInfo,
  JavaStateSnapshot,
  LauncherSnapshot,
  LaunchInstanceInput,
  LoaderVersionsResult,
  MCVersionManifestResult,
  NetworkCheckResponse,
  PreflightReport,
  ProjectCard,
  PublicConfig,
  SavedPack,
  ApplyUpdateInput,
  ExecuteImportInput,
  ExportModpackInput,
  ExportModpackResult,
  ImportPreview,
  ModpackImportProgress,
  ModpackView,
  PreviewImportInput,
  UpdateDiffPreview,
  UpdateInstanceInput,
} from "../types";

export interface DesktopDataDirInfo {
  dir: string;
  mode: "standard" | "portable" | "env" | string;
  migrated_from?: string;
  backup_path?: string;
}

export interface DesktopAppInfo {
  version: string;
  runtime: "wails-desktop" | "web" | string;
  os: string;
  arch: string;
  schema_version: number;
  data_dir: DesktopDataDirInfo;
}

export interface DesktopSavePackZIPResult {
  cancelled: boolean;
  path?: string;
  downloaded: number;
  total: number;
  failed: number;
  status: "complete" | "partial" | "cancelled";
  failed_reasons?: string[];
}

export interface DesktopScanInstalledResult {
  cancelled: boolean;
  folder?: string;
  scanned_files: number;
  installed: Record<string, InstalledModInfo>;
  matched_count: number;
}

export interface DesktopIndexStatus {
  ok: boolean;
  running: boolean;
  phase: string;
  progress: number;
  total_steps: number;
  last_completed: number;
  index_version: number;
  schema_version: number;
  fts5: boolean;
  raw_counts: Record<string, number>;
  merged_counts: Record<string, number>;
  platform_counts: Record<string, Record<string, number>>;
  media_cache: { files: number; size_mb: number };
}

export interface DesktopOverridesData {
  merges: string[][];
  splits: string[][];
}

export interface WailsDesktopBridge {
  GetAppInfo(): Promise<DesktopAppInfo>;
  OpenDataDir(): Promise<void>;
  OpenExternalURL(rawURL: string): Promise<void>;
  ScanInstalledFolder(mcVersion: string, loader: string): Promise<DesktopScanInstalledResult>;
  SavePackZIP(packName: string, items: CartItem[]): Promise<DesktopSavePackZIPResult>;

  GetConfig(): Promise<PublicConfig>;
  UpdateConfig(req: {
    curseforge_api_key?: string;
    merge_threshold?: number;
    proxy_url?: string;
    auto_install_medium_deps?: boolean;
    max_concurrent_downloads?: number;
    microsoft_client_id?: string;
  }): Promise<PublicConfig>;
  TestCurseForgeKey(candidateKey: string): Promise<{ ok: boolean; message?: string; error?: string }>;
  CheckNetwork(): Promise<NetworkCheckResponse>;
  ClearAllCaches(): Promise<void>;
  GetIndexStatus(): Promise<DesktopIndexStatus>;
  TriggerIndexSync(full: boolean): Promise<{ ok: boolean; started: boolean; status: DesktopIndexStatus }>;

  GetMCVersions(): Promise<{ ok: true; versions: string[]; by_loader: Record<string, string[]> }>;
  GetFilters(ct: string): Promise<{ ok: true; type: string; categories: FilterCategory[] }>;
  ResolveURL(rawURL: string): Promise<ProjectCard>;

  GetPacks(): Promise<SavedPack[]>;
  SavePack(pack: SavedPack): Promise<SavedPack[]>;
  DeletePack(id: string): Promise<SavedPack[]>;

  GetOverrides(): Promise<DesktopOverridesData>;
  AddMergeOverride(sourceKeys: string[]): Promise<DesktopOverridesData>;
  AddSplitOverride(sourceKeys: string[]): Promise<DesktopOverridesData>;
  DeleteOverride(kind: string, index: number): Promise<DesktopOverridesData>;
  ResetOverrides(): Promise<DesktopOverridesData>;

  GetDownloads(): Promise<DownloadsSnapshot>;
  AddDownload(req: DownloadRequestInput): Promise<DownloadTaskView>;
  PauseDownload(id: string): Promise<DownloadTaskView>;
  ResumeDownload(id: string): Promise<DownloadTaskView>;
  CancelDownload(id: string): Promise<DownloadTaskView>;
  RetryDownload(id: string): Promise<DownloadTaskView>;
  RemoveDownload(id: string): Promise<void>;
  ClearFinishedDownloads(): Promise<number>;
  SetMaxConcurrentDownloads(n: number): Promise<PublicConfig>;

  GetJavaRuntimes(refreshRemote: boolean): Promise<JavaStateSnapshot>;
  ScanJavaRuntimes(): Promise<JavaStateSnapshot>;
  AddCustomJavaPath(path: string): Promise<JavaRuntimeInfo>;
  BrowseCustomJavaPath(): Promise<JavaRuntimeInfo>;
  VerifyJavaRuntime(id: string): Promise<JavaRuntimeInfo>;
  InstallJavaRuntime(component: string): Promise<void>;
  CancelJavaInstall(component: string): Promise<void>;
  ReinstallJavaRuntime(id: string): Promise<void>;
  RemoveJavaRuntime(id: string): Promise<void>;
  OpenJavaRuntimeDir(id: string): Promise<void>;
  RecommendJavaForMC(mcVersion: string, loader: string): Promise<JavaRecommendationResponse>;

  GetInstances(): Promise<InstancesSnapshot>;
  GetInstance(id: string): Promise<GameInstance>;
  CreateInstance(req: CreateInstanceInput): Promise<GameInstance>;
  UpdateInstance(req: UpdateInstanceInput): Promise<GameInstance>;
  DuplicateInstance(req: DuplicateInstanceInput): Promise<GameInstance>;
  DeleteInstance(req: DeleteInstanceInput): Promise<void>;
  VerifyInstance(id: string): Promise<InstanceIntegrityReport>;
  RepairInstance(id: string): Promise<InstanceIntegrityReport>;
  GetInstanceFiles(id: string): Promise<InstanceFilesSummary>;
  ListInstanceMods(id: string): Promise<InstalledModRef[]>;
  SetInstanceModEnabled(req: { id: string; filename: string; enabled: boolean }): Promise<InstalledModRef[]>;
  DeleteInstanceMod(req: { id: string; filename: string }): Promise<InstalledModRef[]>;
  SetAllInstanceModsEnabled(instanceID: string, enabled: boolean): Promise<InstalledModRef[]>;
  DeleteMultipleInstanceMods(req: { id: string; filenames: string[] }): Promise<InstalledModRef[]>;
  CheckInstanceModUpdates(instanceID: string): Promise<ModUpdateInfo[]>;
  ApplyInstanceModUpdate(req: ApplyModUpdateInput): Promise<InstanceModInstallResult>;
  InstallInstanceMod(req: InstallInstanceModInput): Promise<InstanceModInstallResult>;
  ListInstanceResourcePacks(id: string): Promise<ResourcePackRef[]>;
  SetInstanceResourcePackEnabled(req: { id: string; filename: string; enabled: boolean }): Promise<ResourcePackRef[]>;
  DeleteInstanceResourcePack(req: { id: string; filename: string }): Promise<ResourcePackRef[]>;
  ListInstanceShaderPacks(id: string): Promise<ShaderPackRef[]>;
  SetInstanceShaderPackEnabled(req: { id: string; filename: string; enabled: boolean }): Promise<ShaderPackRef[]>;
  DeleteInstanceShaderPack(req: { id: string; filename: string }): Promise<ShaderPackRef[]>;
  ListInstanceWorlds(id: string): Promise<WorldRef[]>;
  ExportInstanceWorld(id: string, folder: string): Promise<ExportWorldResult>;
  OpenInstanceDir(id: string, subdir: string): Promise<void>;
  BrowseInstanceFolder(): Promise<string>;

  GetMinecraftManifest(forceRefresh: boolean): Promise<MCVersionManifestResult>;
  GetModloaderVersions(loader: string, mcVersion: string): Promise<LoaderVersionsResult>;
  InstallProfileVersion(req: InstallProfileVersionInput): Promise<void>;
  CancelProfileInstall(instanceID: string): Promise<boolean>;
  GetActiveProfileInstall(instanceID: string): Promise<InstallProgressInfo | null>;
  ListProfileInstalledVersions(instanceID: string): Promise<InstalledProfileVersion[]>;
  ActivateProfileVersion(instanceID: string, installedID: string, createBackup: boolean): Promise<InstalledProfileVersion>;
  VerifyProfileVersion(instanceID: string, installedID: string): Promise<FullVersionVerificationReport>;
  DeleteProfileInstalledVersion(instanceID: string, installedID: string): Promise<void>;

  GetAccounts(): Promise<AccountsSnapshot>;
  StartMicrosoftLogin(reauthAccountID: string): Promise<void>;
  AddOfflineAccount(username: string): Promise<AccountView>;
  CancelMicrosoftLogin(): Promise<boolean>;
  SetActiveAccount(id: string): Promise<AccountView>;
  RefreshAccountAuth(id: string, force: boolean): Promise<AccountView>;
  CheckAccountLicense(id: string): Promise<AccountView>;
  SignOutAccount(id: string): Promise<AccountView>;
  DeleteAccount(id: string): Promise<void>;
  UpdateAccountSkin(id: string, skinURL: string, skinVariant: string, capeURL: string, capeAlias: string): Promise<AccountView>;

  GetLauncherSnapshot(): Promise<LauncherSnapshot>;
  PreflightLaunch(req: LaunchInstanceInput): Promise<PreflightReport>;
  LaunchInstance(req: LaunchInstanceInput): Promise<GameSessionInfo>;
  StopInstance(instanceID: string, force: boolean): Promise<void>;
  GetInstanceSession(instanceID: string): Promise<GameSessionInfo | null>;
  GetInstanceSessionHistory(instanceID: string, limit: number): Promise<GameSessionInfo[]>;
  GetInstanceLogs(instanceID: string, limit: number): Promise<GameLogLine[]>;
  ListInstanceCrashReports(instanceID: string): Promise<CrashReportInfo[]>;
  AnalyzeInstanceCrash(instanceID: string): Promise<CrashDiagnosis | null>;
  OpenInstanceLogFile(instanceID: string): Promise<void>;
  OpenCrashReport(instanceID: string, filename: string): Promise<void>;

  ListModpacks(): Promise<ModpackView[]>;
  DeleteModpackRecord(id: string): Promise<void>;
  BrowseModpackArchive(): Promise<string>;
  PreviewModpackImport(req: PreviewImportInput): Promise<ImportPreview>;
  ExecuteModpackImport(req: ExecuteImportInput): Promise<void>;
  GetActiveModpackProgress(): Promise<ModpackImportProgress | null>;
  ExportModpack(req: ExportModpackInput): Promise<ExportModpackResult>;
  PreviewModpackUpdate(modpackID: string, newArchivePath: string): Promise<UpdateDiffPreview>;
  ApplyModpackUpdate(req: ApplyUpdateInput): Promise<ModpackView>;

  MinimizeWindow(): Promise<void>;
  HideWindow(): Promise<void>;
  ShowWindow(): Promise<void>;
  ToggleMaximizeWindow(): Promise<void>;
  CloseWindow(): Promise<void>;
  TrimMemory(): Promise<void>;
  EnterPhantomMode(): Promise<void>;
}

export interface WailsRuntime {
  EventsOn(eventName: string, callback: (...data: unknown[]) => void): () => void;
  EventsOff(eventName: string, ...additionalEventNames: string[]): void;
  OnFileDrop?: (callback: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean) => void;
  OnFileDropOff?: () => void;
  BrowserOpenURL(url: string): void;
}

declare global {
  interface Window {
    go?: {
      desktop?: {
        Bridge?: WailsDesktopBridge;
      };
    };
    runtime?: WailsRuntime;
  }
}

export {};

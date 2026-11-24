import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "../api";
import type { DependencyRef, DepResolutionStatus, DepTreeNode, UnresolvedDep } from "../types";

export interface UseDependencyTreeParams {
  enabled: boolean;
  ownerKey: string;
  dependencies: DependencyRef[];
  mcVersion?: string;
  loader?: string;
  fileURL?: string;
  platform?: string;
  projectID?: string;
  title?: string;
  summary?: string;
  body?: string;
  changelog?: string;
  maxDepth?: number;
}

export interface DependencyTreeState {
  tree: DepTreeNode[];
  unresolved: UnresolvedDep[];
  warnings: string[];
  errors: Record<string, string>;
  status: DepResolutionStatus;
  manifestStatus: string;
  loading: boolean;
  fatalError: string;
  retry: () => void;
}

export function useDependencyTree({
  enabled,
  ownerKey,
  dependencies,
  mcVersion = "",
  loader = "",
  fileURL = "",
  platform = "",
  projectID = "",
  title = "",
  summary = "",
  body = "",
  changelog = "",
  maxDepth = 3,
}: UseDependencyTreeParams): DependencyTreeState {
  const [state, setState] = useState<{
    owner: string;
    tree: DepTreeNode[];
    unresolved: UnresolvedDep[];
    warnings: string[];
    errors: Record<string, string>;
    status: DepResolutionStatus;
    manifestStatus: string;
    loading: boolean;
    fatalError: string;
  }>({
    owner: "",
    tree: [],
    unresolved: [],
    warnings: [],
    errors: {},
    status: "none",
    manifestStatus: "skipped",
    loading: false,
    fatalError: "",
  });
  const [retryTick, setRetryTick] = useState(0);

  const cleanDeps = useMemo(
    () => (dependencies || []).filter((d) => d.project_id || d.version_id),
    [dependencies]
  );

  const depSig = useMemo(
    () => cleanDeps.map((d) => `${d.platform}:${d.project_id}:${d.version_id}:${d.type}`).join(";"),
    [cleanDeps]
  );

  const retry = useCallback(() => {
    setRetryTick((t) => t + 1);
  }, []);

  useEffect(() => {
    if (!enabled || !ownerKey) return;
    if (cleanDeps.length === 0 && !fileURL && !title && !summary && !body && !projectID) {
      setState({
        owner: ownerKey,
        tree: [],
        unresolved: [],
        warnings: [],
        errors: {},
        status: "none",
        manifestStatus: "unavailable",
        loading: false,
        fatalError: "",
      });
      return;
    }

    const ctrl = new AbortController();
    setState({
      owner: ownerKey,
      tree: [],
      unresolved: [],
      warnings: [],
      errors: {},
      status: "none",
      manifestStatus: "skipped",
      loading: true,
      fatalError: "",
    });

    api
      .getDependencyTree(cleanDeps, {
        mc_version: mcVersion,
        loader,
        file_url: fileURL,
        platform,
        project_id: projectID,
        title,
        summary,
        body,
        changelog,
        max_depth: maxDepth,
        signal: ctrl.signal,
      })
      .then((res) => {
        if (ctrl.signal.aborted) return;
        setState({
          owner: ownerKey,
          tree: res.tree || [],
          unresolved: res.unresolved || [],
          warnings: res.warnings || [],
          errors: res.errors || {},
          status: res.status || ((res.tree || []).length > 0 ? "complete" : "none"),
          manifestStatus: res.manifest_status || "skipped",
          loading: false,
          fatalError: "",
        });
      })
      .catch((err) => {
        if ((err as Error).name === "AbortError" || ctrl.signal.aborted) return;
        setState({
          owner: ownerKey,
          tree: [],
          unresolved: [],
          warnings: [],
          errors: {},
          status: "error",
          manifestStatus: "failed",
          loading: false,
          fatalError: `Не удалось проверить зависимости: ${(err as Error).message}`,
        });
      });

    return () => ctrl.abort();
  }, [enabled, ownerKey, depSig, mcVersion, loader, fileURL, platform, projectID, title, summary, body, changelog, maxDepth, retryTick]);

  // Strict ownership guard: never return stale data from a previous project/version on frame 0
  const owned = state.owner === ownerKey;
  return {
    tree: owned ? state.tree : [],
    unresolved: owned ? state.unresolved : [],
    warnings: owned ? state.warnings : [],
    errors: owned ? state.errors : {},
    status: owned ? state.status : "none",
    manifestStatus: owned ? state.manifestStatus : "skipped",
    loading: owned ? state.loading : enabled && Boolean(ownerKey && (cleanDeps.length > 0 || fileURL || title || summary || body || projectID)),
    fatalError: owned ? state.fatalError : "",
    retry,
  };
}

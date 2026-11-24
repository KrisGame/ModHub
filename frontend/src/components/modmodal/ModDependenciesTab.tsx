import React from "react";
import { CustomSelect, type CustomSelectOption } from "../CustomSelect";
import { DependencyTreeList } from "../DependencyTreeList";
import { useDependencyTree } from "../../hooks/useDependencyTree";
import { LoaderIcon } from "../../icons";
import type { ModVersionsResponse, VersionFile } from "../../types";
import { formatLoaderName } from "../../utils";

interface ModDependenciesTabProps {
  projectGroupId: string;
  projectTitle?: string;
  projectSummary?: string;
  projectBody?: string;
  itemSupportsLoaders: boolean;
  versData: ModVersionsResponse | null;
  versLoading: boolean;
  ownDepMc: string;
  ownDepLoader: string;
  onChangeMc: (v: string) => void;
  onChangeLoader: (l: string) => void;
  ownDepFile: VersionFile | null;
  ownDepMismatch: boolean;
}

export const ModDependenciesTab: React.FC<ModDependenciesTabProps> = ({
  projectGroupId,
  projectTitle = "",
  projectSummary = "",
  projectBody = "",
  itemSupportsLoaders,
  versData,
  versLoading,
  ownDepMc,
  ownDepLoader,
  onChangeMc,
  onChangeLoader,
  ownDepFile,
  ownDepMismatch,
}) => {
  const effMc =
    ownDepFile
      ? (ownDepMc && ownDepFile.game_versions.includes(ownDepMc) ? ownDepMc : ownDepFile.game_versions[0]) || ""
      : "";
  const effLd =
    ownDepFile
      ? (ownDepLoader !== "all" && ownDepFile.loaders.includes(ownDepLoader) ? ownDepLoader : ownDepFile.loaders[0]) || ""
      : "";

  const ownerKey = ownDepFile
    ? `${projectGroupId}|${ownDepFile.platform}|${ownDepFile.version_id}|${effMc}|${effLd}`
    : `${projectGroupId}|none`;

  const depState = useDependencyTree({
    enabled: Boolean(ownDepFile && !versLoading),
    ownerKey,
    dependencies: ownDepFile?.dependencies || [],
    mcVersion: effMc,
    loader: effLd,
    fileURL: ownDepFile?.download_url || "",
    platform: ownDepFile?.platform || "",
    projectID: ownDepFile?.mod_id || "",
    title: projectTitle,
    summary: projectSummary,
    body: projectBody,
    changelog: ownDepFile?.changelog || "",
    maxDepth: 3,
  });

  return (
    <div className="own-deps-pane">
      <div className="deps-toolbar">
        <div className="deps-toolbar-row">
          {itemSupportsLoaders && (versData?.facets?.loaders?.length || 0) > 0 && (
            <CustomSelect
              value={ownDepLoader}
              options={[
                { value: "all", label: "Любой загрузчик" },
                ...versData!.facets.loaders.map((l): CustomSelectOption => ({
                  value: l,
                  label: formatLoaderName(l),
                  icon: <LoaderIcon loader={l} size={12.5} />,
                })),
              ]}
              onChange={onChangeLoader}
              className="cs-auto"
              ariaLabel="Загрузчик для проверки зависимостей"
            />
          )}
          {(versData?.facets?.game_versions?.length || 0) > 0 && (
            <CustomSelect
              value={ownDepMc}
              options={[
                { value: "", label: "Последняя версия MC" },
                ...versData!.facets.game_versions.map((gv): CustomSelectOption => ({
                  value: gv,
                  label: gv,
                })),
              ]}
              onChange={onChangeMc}
              searchable
              searchPlaceholder="Версия MC..."
              mono
              className="cs-auto"
              ariaLabel="Версия MC для проверки зависимостей"
            />
          )}
        </div>
        {ownDepFile && (
          <div className="deps-toolbar-sub">
            <span className="mono-num">
              Файл: {ownDepFile.filename} ({ownDepFile.platform === "modrinth" ? "Modrinth" : "CurseForge"})
            </span>
          </div>
        )}
      </div>

      {versLoading ? (
        <div className="empty-state">
          <span className="spinner" style={{ marginRight: 8, verticalAlign: "middle" }} />
          Загрузка версий проекта…
        </div>
      ) : ownDepMismatch ? (
        <div className="empty-state">
          <h3>Нет файлов для выбранного фильтра</h3>
          <p>У этого проекта нет версии под выбранную комбинацию Minecraft и загрузчика.</p>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            style={{ marginTop: 10 }}
            onClick={() => {
              onChangeMc("");
              onChangeLoader("all");
            }}
          >
            Сбросить фильтр версий
          </button>
        </div>
      ) : (
        <DependencyTreeList
          tree={depState.tree}
          unresolved={depState.unresolved}
          warnings={depState.warnings}
          errors={depState.errors}
          status={depState.status}
          loading={depState.loading}
          fatalError={depState.fatalError}
          grouped
          onRetry={depState.retry}
        />
      )}
    </div>
  );
};

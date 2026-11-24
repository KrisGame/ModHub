import React, { useEffect } from "react";
import { useOverlayTransition } from "../hooks";
import { CloseIcon } from "../icons";

interface AboutModalProps {
  open: boolean;
  onClose: () => void;
  onOpenSettings: () => void;
}

export const AboutModal: React.FC<AboutModalProps> = ({ open, onClose, onOpenSettings }) => {
  const { mounted, shown } = useOverlayTransition(open, 230);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!mounted) return null;

  return (
    <div
      className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`}
      role="dialog"
      aria-modal="true"
      aria-labelledby="aboutModalTitle"
      aria-hidden={!shown}
      onClick={onClose}
    >
      <div className="modal-window modal-about" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h2 className="modal-title" id="aboutModalTitle">О проекте ModHub</h2>
            <p className="modal-subtitle">Весь Minecraft-моддинг. В одном каталоге.</p>
          </div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon size={15} />
          </button>
        </div>

        <div className="modal-body about-body">
          <section className="about-section">
            <h3>Что делает ModHub</h3>
            <p>
              ModHub — локальное десктоп- и веб-приложение, объединяющее каталоги{" "}
              <strong>Modrinth</strong> и <strong>CurseForge</strong> в одном рабочем пространстве.
              Оно позволяет искать моды, ресурспаки, шейдеры и датапаки сразу по двум платформам,
              сравнивать версии и загрузчики, проверять зависимости и собирать готовые <code>.ZIP</code>-сборки.
            </p>
          </section>

          <section className="about-section">
            <h3>Как работают поиск и объединение</h3>
            <ul>
              <li>
                <strong>Два официальных источника:</strong> запросы отправляются с локального Go-бэкенда
                в Modrinth и CurseForge. Ключ CurseForge хранится локально в{" "}
                <code>data/config.json</code>, не передаётся в браузер и в Windows дополнительно защищается средствами DPAPI.
              </li>
              <li>
                <strong>Безопасная склейка дублей:</strong> одинаковые проекты с двух платформ объединяются
                по совпадению официального репозитория или названия с проверкой авторов/slug. Версии разных
                источников никогда не смешиваются — у каждого файла всегда указана исходная платформа и ссылка.
              </li>
              <li>
                <strong>Локальный индекс SQLite FTS5:</strong> кэширует выдачу, метаданные и обратные
                зависимости для быстрого отклика и снижения нагрузки на внешние API.
              </li>
            </ul>
          </section>

          <section className="about-section">
            <h3>Откуда поступают файлы и как работает скачивание</h3>
            <p>
              ModHub не перепаковывает и не модифицирует отдельные <code>.jar</code>/<code>.zip</code>-файлы:
              каждая загрузка идёт напрямую с официальных CDN Modrinth или CurseForge (либо открывает
              оригинальную страницу файла, если автор ограничил стороннюю загрузку на CurseForge).
              При экспорте сборки <code>.ZIP</code> локальный сервер скачивает выбранные официальные файлы,
              раскладывает их по папкам (<code>mods/</code>, <code>resourcepacks/</code>,{" "}
              <code>shaderpacks/</code>, <code>datapacks/</code>) и добавляет <code>manifest.txt</code>.
            </p>
          </section>

          <section className="about-section about-security">
            <h3>Безопасность и честные границы проверок</h3>
            <ul>
              <li>
                <strong>Что проверяется:</strong> все запросы к API и CDN идут по HTTPS; при сканировании
                локальной папки <code>mods</code> файлы читаются только для чтения и сопоставляются по хэшу{" "}
                <strong>SHA-1</strong> через Modrinth API; HTML-описания проектов проходят санитизацию перед показом.
              </li>
              <li>
                <strong>Что НЕ выполняется:</strong> ModHub <strong>не проводит собственное антивирусное
                сканирование</strong> содержимого <code>.jar</code>-архивов. Доверие к бинарным файлам
                основано на модерации оригинальных платформ (Modrinth / CurseForge) и выборе проверенных авторов.
              </li>
            </ul>
          </section>
        </div>

        <div className="modal-footer about-footer">
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            onClick={() => {
              onClose();
              onOpenSettings();
            }}
          >
            Настройки API и прокси
          </button>
          <button type="button" className="btn btn-primary btn-sm" onClick={onClose}>
            Понятно
          </button>
        </div>
      </div>
    </div>
  );
};

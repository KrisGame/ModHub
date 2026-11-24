import DOMPurify from "dompurify";
import type { CartItem, FilterCategory, ProjectCard } from "./types";

export const REAL_LOADERS = ["neoforge", "fabric", "forge", "quilt"] as const;

export function escapeHtml(str: string | null | undefined): string {
  return String(str ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

export function stripHtmlTags(str: string | null | undefined): string {
  const s = String(str ?? "");
  if (!s) return "";
  if (!s.includes("<")) return s.replace(/\s+/g, " ").trim();
  return s.replace(/<[^>]*>/g, "").replace(/\s+/g, " ").trim();
}

export function formatNumber(n: number | null | undefined): string {
  const num = Number(n || 0);
  if (num >= 1_000_000_000) return `${(num / 1_000_000_000).toFixed(1)}B`;
  if (num >= 1_000_000) return `${(num / 1_000_000).toFixed(1)}M`;
  if (num >= 1_000) return `${(num / 1_000).toFixed(1)}K`;
  return String(num);
}

export function formatBytes(bytes: number | null | undefined): string {
  const b = Number(bytes || 0);
  if (b <= 0) return "—";
  if (b >= 1_048_576) return `${(b / 1_048_576).toFixed(1)} МБ`;
  if (b >= 1_024) return `${(b / 1_024).toFixed(0)} КБ`;
  return `${b} Б`;
}

const ruDateFormatter = new Intl.DateTimeFormat("ru-RU", {
  day: "2-digit",
  month: "short",
  year: "numeric",
});
const formattedDateCache = new Map<string, string>();

export function formatDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  const cached = formattedDateCache.get(iso);
  if (cached !== undefined) return cached;
  const ms = Date.parse(iso);
  let res: string;
  if (Number.isNaN(ms)) {
    res = String(iso).slice(0, 10);
  } else {
    const diffDays = Math.floor((Date.now() - ms) / 86_400_000);
    if (diffDays <= 0) res = "сегодня";
    else if (diffDays === 1) res = "вчера";
    else if (diffDays < 30) res = `${diffDays} дн. назад`;
    else res = ruDateFormatter.format(ms);
  }
  if (formattedDateCache.size > 600) formattedDateCache.clear();
  formattedDateCache.set(iso, res);
  return res;
}

export function formatLoaderName(l: string): string {
  switch (String(l || "").toLowerCase()) {
    case "neoforge": return "NeoForge";
    case "fabric": return "Fabric";
    case "forge": return "Forge";
    case "quilt": return "Quilt";
    case "datapack": return "Datapack";
    default: return l;
  }
}

export function filterRealLoaders(loaders: string[], ct: string): string[] {
  if (ct === "shader" || ct === "resourcepack" || ct === "modpack") return [];
  const out: string[] = [];
  for (const l of loaders || []) {
    const low = String(l).toLowerCase();
    if ((REAL_LOADERS as readonly string[]).includes(low) && !out.includes(low)) {
      out.push(low);
    }
  }
  return out;
}

export function isProjectUrlString(str: string): boolean {
  return /^https?:\/\/(?:www\.)?(?:modrinth\.com\/(?:mod|resourcepack|shader|datapack|plugin)\/|curseforge\.com\/minecraft\/(?:mc-mods|texture-packs|shaders|data-packs|customization)\/)[A-Za-z0-9._-]+/i.test(
    String(str || "").trim()
  );
}

export function cachedIconUrl(rawUrl: string, size = 144): string {
  if (!rawUrl || !rawUrl.startsWith("https://")) return rawUrl || "";
  return `/api/media/icon?url=${encodeURIComponent(rawUrl)}&size=${size}`;
}

const fallbackIconCache = new Map<string, string>();

export function fallbackIconDataUri(title: string): string {
  const raw = String(title || "M").replace(/[\uD800-\uDFFF]/g, "").trim();
  const alnum = raw.match(/[A-Za-zА-Яа-яЁё0-9]/);
  const ch = (alnum ? alnum[0] : "M").toUpperCase();
  const cached = fallbackIconCache.get(ch);
  if (cached) return cached;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="96" height="96" viewBox="0 0 96 96"><rect width="96" height="96" rx="18" fill="#23262c"/><text x="48" y="61" text-anchor="middle" font-family="system-ui,sans-serif" font-size="42" font-weight="700" fill="#cfd5de">${escapeHtml(ch)}</text></svg>`;
  const uri = `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(svg)}`;
  fallbackIconCache.set(ch, uri);
  return uri;
}

export function highlightSearchTermsHtml(text: string, rawQuery: string): string {
  const safe = escapeHtml(text);
  const cleaned = String(rawQuery || "")
    .replace(/\b(?:author|dep|depends|mc|v|version|dl|downloads):(?:"[^"]*"|\S+)/gi, " ")
    .replace(/[-"|*]/g, " ")
    .trim();
  if (!cleaned) return safe;
  const terms = cleaned.split(/\s+/).filter((w) => w.length >= 2).slice(0, 5);
  if (!terms.length) return safe;
  const pattern = terms.map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|");
  return safe.replace(new RegExp(`(${pattern})`, "gi"), '<mark class="hl-match">$1</mark>');
}

export function mapCategoryTags(
  rawCats: string[],
  availableCats: FilterCategory[],
  maxTags = 0
): { id: string; label: string }[] {
  const out: { id: string; label: string }[] = [];
  const seen = new Set<string>();
  const rawSet = new Set(
    (rawCats || [])
      .map((c) => String(c || "").toLowerCase().trim())
      .flatMap((c) => [c, c.replace(/-/g, " "), c.replace(/\s+/g, "-")])
  );
  for (const def of availableCats || []) {
    const needles = [def.id.toLowerCase(), ...(def.tags || []).map((t) => t.toLowerCase())];
    if (needles.some((n) => rawSet.has(n)) && !seen.has(def.id)) {
      seen.add(def.id);
      out.push({ id: def.id, label: def.label });
      if (maxTags > 0 && out.length >= maxTags) break;
    }
  }
  return out;
}

export function findCartIndex(cart: CartItem[], item: ProjectCard, activeMcVersion: string): number {
  const skeys = new Set((item.sources || []).map((s) => s.source_key).filter(Boolean));
  const normTitle = item.title.trim().toLowerCase();
  return cart.findIndex((c) => {
    const match =
      (c.group_id && c.group_id === item.group_id) ||
      (c.source_key && skeys.has(c.source_key)) ||
      (c.source_keys && c.source_keys.some((k) => skeys.has(k))) ||
      (normTitle && c.title.trim().toLowerCase() === normTitle);
    if (!match) return false;
    if (activeMcVersion && c.game_versions?.length && !c.game_versions.includes(activeMcVersion)) {
      return false;
    }
    return true;
  });
}

export function detectCartConflicts(items: CartItem[]): string[] {
  const warnings: string[] = [];
  if (!items || items.length < 2) return warnings;

  const explicitMc = new Set<string>();
  const gvSets: Set<string>[] = [];
  for (const it of items) {
    if (it.mc_version) explicitMc.add(it.mc_version);
    const gvs = (it.game_versions || []).filter(Boolean);
    if (gvs.length) gvSets.push(new Set(gvs));
    else if (it.mc_version) gvSets.push(new Set([it.mc_version]));
  }
  if (gvSets.length >= 2) {
    let common = new Set(gvSets[0]);
    for (let i = 1; i < gvSets.length; i++) {
      common = new Set([...common].filter((v) => gvSets[i].has(v)));
    }
    if (common.size === 0) {
      const shown = (explicitMc.size >= 2
        ? Array.from(explicitMc)
        : Array.from(new Set(gvSets.map((s) => Array.from(s)[0]))))
        .slice(0, 4)
        .join(", ");
      warnings.push(`Смешаны разные версии MC: ${shown}`);
    }
  }

  const modLoaderSets: Set<string>[] = [];
  const explicitLoaders = new Set<string>();
  for (const it of items) {
    if ((it.folder || "mods") !== "mods") continue;
    const ls = (it.loaders || [])
      .map((l) => l.toLowerCase())
      .filter((l) => (REAL_LOADERS as readonly string[]).includes(l));
    const single = it.loader && (REAL_LOADERS as readonly string[]).includes(it.loader.toLowerCase())
      ? it.loader.toLowerCase()
      : "";
    if (single) explicitLoaders.add(single);
    if (ls.length) modLoaderSets.push(new Set(ls));
    else if (single) modLoaderSets.push(new Set([single]));
  }
  if (modLoaderSets.length >= 2) {
    let commonL = new Set(modLoaderSets[0]);
    for (let i = 1; i < modLoaderSets.length; i++) {
      commonL = new Set([...commonL].filter((l) => modLoaderSets[i].has(l)));
    }
    if (commonL.size === 0) {
      const shownL = (explicitLoaders.size >= 2
        ? Array.from(explicitLoaders)
        : Array.from(new Set(modLoaderSets.map((s) => Array.from(s)[0]))))
        .slice(0, 4)
        .map(formatLoaderName)
        .join(", ");
      warnings.push(`Смешаны разные загрузчики: ${shownL}`);
    }
  }

  return warnings;
}

let domPurifyHooked = false;

function ensureDomPurifyHook(): void {
  if (domPurifyHooked) return;
  domPurifyHooked = true;
  DOMPurify.addHook("afterSanitizeAttributes", (node) => {
    if (!(node instanceof Element)) return;
    const tag = node.tagName.toLowerCase();

    if (tag === "a") {
      const href = (node.getAttribute("href") || "").trim();
      if (href && !/^(?:https?:|mailto:|#)/i.test(href)) {
        node.removeAttribute("href");
      } else if (href) {
        node.setAttribute("target", "_blank");
        node.setAttribute("rel", "noopener noreferrer");
      }
    } else if (tag === "img") {
      const src = (node.getAttribute("src") || "").trim();
      if (src && !/^https?:\/\//i.test(src)) {
        node.removeAttribute("src");
      } else {
        node.setAttribute("loading", "lazy");
        node.setAttribute("decoding", "async");
      }
    }

    if (node.hasAttribute("style")) {
      const safeStyle = (node.getAttribute("style") || "")
        .replace(/position\s*:[^;]+;?/gi, "")
        .replace(/z-index\s*:[^;]+;?/gi, "")
        .replace(/expression\s*\([^)]*\)/gi, "")
        .replace(/url\s*\([^)]*\)/gi, "")
        .trim();
      if (safeStyle) node.setAttribute("style", safeStyle);
      else node.removeAttribute("style");
    }
  });
}

export function sanitizeHtml(dirtyHtml: string): string {
  ensureDomPurifyHook();
  return DOMPurify.sanitize(String(dirtyHtml || ""), {
    USE_PROFILES: { html: true },
    FORBID_TAGS: [
      "script", "style", "iframe", "object", "embed", "form",
      "input", "button", "textarea", "select", "link", "meta",
      "base", "noscript", "svg", "math",
    ],
    FORBID_ATTR: ["srcdoc", "formaction", "xlink:href", "action", "ping"],
    ALLOW_DATA_ATTR: false,
    ALLOWED_URI_REGEXP: /^(?:(?:https?|mailto):|#)/i,
  });
}

export function renderMarkdown(md: string): string {
  if (!md) return "<p>Описание отсутствует.</p>";
  let text = String(md).replace(/\r\n?/g, "\n");

  const placeholders: string[] = [];
  const stash = (html: string) => {
    const idx = placeholders.length;
    placeholders.push(html);
    return `\u0000MHPH${idx}\u0000`;
  };

  // 1. Protect code blocks & inline code
  text = text.replace(/```[^\n]*\n([\s\S]*?)```/g, (_, code) =>
    stash(`<pre><code>${escapeHtml(code.replace(/\n$/, ""))}</code></pre>`)
  );
  text = text.replace(/`([^`\n]+)`/g, (_, code) => stash(`<code>${escapeHtml(code)}</code>`));

  // 2. Protect raw HTML tags so Markdown emphasis inside URLs/attrs isn't mangled
  const htmlTagPlaceholders: string[] = [];
  text = text.replace(/<\/?[a-zA-Z][a-zA-Z0-9-]*(?:\s+[^<>]*?)?\s*\/?>/g, (tagStr) => {
    const idx = htmlTagPlaceholders.length;
    htmlTagPlaceholders.push(tagStr);
    return `\u0000MHTG${idx}\u0000`;
  });

  // 3. Linked images: [![alt](imgUrl)](linkUrl)
  text = text.replace(
    /\[!\[([^\]]*)\]\((https?:\/\/[^\s)]+)(?:\s+"[^"]*")?\)\]\((https?:\/\/[^\s)]+)(?:\s+"[^"]*")?\)/g,
    (_, alt, imgUrl, href) =>
      stash(
        `<a href="${escapeHtml(href)}" target="_blank" rel="noopener noreferrer"><img src="${escapeHtml(imgUrl)}" alt="${escapeHtml(alt)}" loading="lazy"></a>`
      )
  );

  // 4. Standalone images: ![alt](url)
  text = text.replace(
    /!\[([^\]]*)\]\((https?:\/\/[^\s)]+)(?:\s+"[^"]*")?\)/g,
    (_, alt, imgUrl) => stash(`<img src="${escapeHtml(imgUrl)}" alt="${escapeHtml(alt)}" loading="lazy">`)
  );

  // 5. Links: [label](url)
  text = text.replace(
    /\[([^\]]+)\]\((https?:\/\/[^\s)]+)(?:\s+"[^"]*")?\)/g,
    (_, label, href) => `<a href="${escapeHtml(href)}" target="_blank" rel="noopener noreferrer">${label}</a>`
  );

  // 6. Headings (# to ######)
  text = text.replace(/^######\s+(.+)$/gm, "<h6>$1</h6>");
  text = text.replace(/^#####\s+(.+)$/gm, "<h5>$1</h5>");
  text = text.replace(/^####\s+(.+)$/gm, "<h4>$1</h4>");
  text = text.replace(/^###\s+(.+)$/gm, "<h3>$1</h3>");
  text = text.replace(/^##\s+(.+)$/gm, "<h2>$1</h2>");
  text = text.replace(/^#\s+(.+)$/gm, "<h1>$1</h1>");

  // 7. Horizontal rules
  text = text.replace(/^[ \t]*(?:[-*_][ \t]*){3,}$/gm, "<hr>");

  // 8. Blockquotes
  text = text.replace(/^>\s?(.*)$/gm, "<blockquote>$1</blockquote>");
  text = text.replace(/<\/blockquote>\n<blockquote>/g, "<br>");

  // 9. Lists
  text = text.replace(/^[ \t]*[-*+]\s+(.+)$/gm, "<li>$1</li>");
  text = text.replace(/^[ \t]*\d+\.\s+(.+)$/gm, "<li>$1</li>");
  text = text.replace(/((?:<li>.*<\/li>\n?)+)/g, "<ul>$1</ul>");

  // 10. Bold, italic, strikethrough
  text = text.replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>");
  text = text.replace(/__([^_\n]+)__/g, "<strong>$1</strong>");
  text = text.replace(/~~([^~\n]+)~~/g, "<del>$1</del>");
  text = text.replace(/(^|[\s(])\*([^*\n]+)\*(?=[\s).,!?:;]|$)/g, "$1<em>$2</em>");

  // 11. Restore raw HTML tags before paragraph splitting
  text = text.replace(/\u0000MHTG(\d+)\u0000/g, (_, idx) => htmlTagPlaceholders[Number(idx)] || "");

  // 12. Paragraph wrapping
  const blockStartRe =
    /^<(?:h[1-6]|p|div|center|details|summary|table|thead|tbody|tr|td|th|ul|ol|li|pre|blockquote|hr|figure|section|article|header|footer|img|a\b[^>]*>\s*<img)/i;
  text = text
    .split(/\n{2,}/)
    .map((block) => {
      const trimmed = block.trim();
      if (!trimmed) return "";
      if (blockStartRe.test(trimmed) || /^\u0000MHPH\d+\u0000$/.test(trimmed)) {
        return trimmed;
      }
      return `<p>${trimmed.replace(/\n/g, "<br>")}</p>`;
    })
    .join("\n");

  // 13. Restore stashed code/image placeholders
  text = text.replace(/\u0000MHPH(\d+)\u0000/g, (_, idx) => placeholders[Number(idx)] || "");

  return sanitizeHtml(text);
}

const safeBodyCache = new Map<string, string>();

export function renderSafeBodyHtml(raw: string, format: string): string {
  if (!raw) return "<p>Описание отсутствует.</p>";
  const key = `${format}:${raw.length}:${raw.slice(0, 48)}:${raw.slice(-32)}`;
  const cached = safeBodyCache.get(key);
  if (cached !== undefined) return cached;
  const html = format === "html" ? sanitizeHtml(raw) : renderMarkdown(raw);
  if (safeBodyCache.size >= 48) {
    const oldest = safeBodyCache.keys().next().value;
    if (oldest !== undefined) safeBodyCache.delete(oldest);
  }
  safeBodyCache.set(key, html);
  return html;
}

export async function sha1FileHex(file: File): Promise<string> {
  const buf = await file.arrayBuffer();
  const digest = await crypto.subtle.digest("SHA-1", buf);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

export function animateFlyToCart(originEl: HTMLElement | null | undefined, iconUrl: string, title: string): void {
  if (!originEl) return;
  if (typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {
    return;
  }
  const cartBtn = document.getElementById("cartBtn");
  if (!cartBtn) return;
  const from = originEl.getBoundingClientRect();
  const to = cartBtn.getBoundingClientRect();
  const img = document.createElement("img");
  img.src = iconUrl || fallbackIconDataUri(title);
  img.className = "fly-to-cart-ghost";
  img.style.left = `${from.left + from.width / 2 - 14}px`;
  img.style.top = `${from.top + from.height / 2 - 14}px`;
  document.body.appendChild(img);
  requestAnimationFrame(() => {
    const dx = to.left + to.width / 2 - (from.left + from.width / 2);
    const dy = to.top + to.height / 2 - (from.top + from.height / 2);
    img.style.transform = `translate(${dx}px, ${dy}px) scale(0.35)`;
    img.style.opacity = "0";
  });
  window.setTimeout(() => {
    img.remove();
    triggerCartSpringBounce();
  }, 420);
}


const ambientRgbCache = new Map<string, string>();
let sharedColorCtx: CanvasRenderingContext2D | null = null;

export function fallbackRgbFromTitle(title: string): string {
  let h = 2166136261;
  const s = String(title || "mod");
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  const hue = Math.abs(h) % 360;
  // Convert muted HSL (s=42%, l=56%) to RGB
  const c = 0.37;
  const x = c * (1 - Math.abs(((hue / 60) % 2) - 1));
  const m = 0.375;
  let r = 0, g = 0, b = 0;
  if (hue < 60) [r, g, b] = [c, x, 0];
  else if (hue < 120) [r, g, b] = [x, c, 0];
  else if (hue < 180) [r, g, b] = [0, c, x];
  else if (hue < 240) [r, g, b] = [0, x, c];
  else if (hue < 300) [r, g, b] = [x, 0, c];
  else [r, g, b] = [c, 0, x];
  return `${Math.round((r + m) * 255)}, ${Math.round((g + m) * 255)}, ${Math.round((b + m) * 255)}`;
}

/** Extract dominant vibrant RGB string ("r, g, b") from a loaded same-origin <img> with zero extra requests */
export function extractDominantRgbFromImg(img: HTMLImageElement, cacheKey: string, title = ""): string {
  const cached = ambientRgbCache.get(cacheKey);
  if (cached) return cached;
  try {
    const size = 20;
    if (!sharedColorCtx) {
      const c = document.createElement("canvas");
      c.width = size;
      c.height = size;
      sharedColorCtx = c.getContext("2d", { willReadFrequently: true });
    }
    const ctx = sharedColorCtx;
    if (!ctx) return fallbackRgbFromTitle(title);
    ctx.clearRect(0, 0, size, size);
    ctx.drawImage(img, 0, 0, size, size);
    const data = ctx.getImageData(0, 0, size, size).data;

    let rSum = 0, gSum = 0, bSum = 0, wSum = 0;
    for (let i = 0; i < data.length; i += 4) {
      const a = data[i + 3];
      if (a < 120) continue;
      const r = data[i], g = data[i + 1], b = data[i + 2];
      const max = Math.max(r, g, b);
      const min = Math.min(r, g, b);
      const sat = max === 0 ? 0 : (max - min) / max;
      const lum = (r * 0.299 + g * 0.587 + b * 0.114) / 255;
      if (lum < 0.08 || lum > 0.94) continue;
      const weight = 0.25 + sat * 2.2 * (1 - Math.abs(lum - 0.52));
      rSum += r * weight;
      gSum += g * weight;
      bSum += b * weight;
      wSum += weight;
    }
    if (wSum > 0.5) {
      const r = Math.min(235, Math.max(48, Math.round(rSum / wSum)));
      const g = Math.min(235, Math.max(48, Math.round(gSum / wSum)));
      const b = Math.min(235, Math.max(48, Math.round(bSum / wSum)));
      const rgb = `${r}, ${g}, ${b}`;
      ambientRgbCache.set(cacheKey, rgb);
      return rgb;
    }
  } catch {
    // Cross-origin fallback
  }
  const fb = fallbackRgbFromTitle(title);
  ambientRgbCache.set(cacheKey, fb);
  return fb;
}

export function getCachedAmbientRgb(cacheKey: string): string | undefined {
  return ambientRgbCache.get(cacheKey);
}

export type EnvKind = "client" | "server" | "both" | "";

export function getEnvKind(item: ProjectCard): EnvKind {
  if ((item.content_type || "mod") !== "mod") return "";
  const cs = String(item.client_side || "").toLowerCase();
  const ss = String(item.server_side || "").toLowerCase();
  if (!cs && !ss) return "";
  const clientOk = cs === "required" || cs === "optional";
  const serverOk = ss === "required" || ss === "optional";
  if (clientOk && ss === "unsupported") return "client";
  if (serverOk && cs === "unsupported") return "server";
  if (clientOk && serverOk) return "both";
  return "";
}

export interface CompatStatus {
  state: "ok" | "warn" | "none";
  label: string;
  title: string;
}

/** Determine whether a card matches the target MC version & loader of the current pack or active filters */
export function evaluatePackCompatibility(
  item: ProjectCard,
  targetMc: string,
  targetLoader: string
): CompatStatus {
  if (!targetMc && !targetLoader) {
    return { state: "none", label: "", title: "" };
  }
  const ct = item.content_type || "mod";
  const gvs = item.game_versions || [];
  const loaders = filterRealLoaders(item.loaders || [], ct);

  const mcChecked = Boolean(targetMc && gvs.length > 0);
  const mcMatch = !mcChecked || gvs.includes(targetMc);

  const loaderChecked = Boolean(targetLoader && ct === "mod" && loaders.length > 0);
  const loaderMatch = !loaderChecked || loaders.includes(targetLoader.toLowerCase());

  if (!mcChecked && !loaderChecked) {
    return { state: "none", label: "", title: "" };
  }

  const targetDesc = [targetMc ? `MC ${targetMc}` : "", targetLoader ? formatLoaderName(targetLoader) : ""]
    .filter(Boolean)
    .join(" / ");

  if (mcMatch && loaderMatch) {
    return {
      state: "ok",
      label: targetMc || formatLoaderName(targetLoader),
      title: `Совместим с текущей сборкой (${targetDesc})`,
    };
  }
  const missing: string[] = [];
  if (!mcMatch) missing.push(`нет MC ${targetMc}`);
  if (!loaderMatch) missing.push(`нет ${formatLoaderName(targetLoader)}`);
  return {
    state: "warn",
    label: "Несовм.",
    title: `Не совпадает со сборкой (${targetDesc}): ${missing.join(", ")}`,
  };
}




export function triggerCartSpringBounce(): void {
  const btn = document.getElementById("cartBtn");
  if (!btn) return;
  btn.classList.remove("is-spring-bouncing");
  void btn.getBoundingClientRect();
  btn.classList.add("is-spring-bouncing");
  window.setTimeout(() => {
    btn.classList.remove("is-spring-bouncing");
  }, 520);
}



function parseMcVerTuple(v: string): [number, number, number] | null {
  const m = String(v || "").trim().match(/^(\d+)\.(\d+)(?:\.(\d+))?$/);
  if (!m) return null;
  return [Number(m[1]), Number(m[2]), Number(m[3] || 0)];
}

export function formatMcVersionRange(versions?: string[]): { label: string; full: string } | null {
  if (!versions || versions.length === 0) return null;
  const parsed: Array<{ raw: string; t: [number, number, number] }> = [];
  const seen = new Set<string>();
  for (const raw of versions) {
    const clean = String(raw || "").trim();
    if (!clean || seen.has(clean)) continue;
    const t = parseMcVerTuple(clean);
    if (t) {
      seen.add(clean);
      parsed.push({ raw: clean, t });
    }
  }
  if (parsed.length === 0) return null;
  parsed.sort((a, b) => {
    if (a.t[0] !== b.t[0]) return a.t[0] - b.t[0];
    if (a.t[1] !== b.t[1]) return a.t[1] - b.t[1];
    return a.t[2] - b.t[2];
  });
  const oldest = parsed[0].raw;
  const newest = parsed[parsed.length - 1].raw;
  const full = parsed.map((p) => p.raw).reverse().join(", ");
  if (oldest === newest) {
    return { label: newest, full };
  }
  return { label: `${oldest}–${newest}`, full };
}


const loadedImageUrls = new Set<string>();

export function isImageUrlLoaded(url: string | null | undefined): boolean {
  if (!url) return true;
  if (url.startsWith("data:")) return true;
  return loadedImageUrls.has(url);
}

export function markImageUrlLoaded(url: string | null | undefined): void {
  if (!url || url.startsWith("data:")) return;
  if (loadedImageUrls.size > 1500) {
    loadedImageUrls.clear();
  }
  loadedImageUrls.add(url);
}

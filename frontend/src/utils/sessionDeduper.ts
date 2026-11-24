import type { ProjectCard, ProjectSource, SortMode } from "../types";

const RE_BRACKET_SUFFIX =
  /[\[\(\{]\s*(?:neoforge|fabric|forge|quilt|liteloader|reforged|neoforged|rift|legacy\s*fabric)(?:\s*[/,&+|•\-]\s*(?:neoforge|fabric|forge|quilt|liteloader|reforged|neoforged|rift|legacy\s*fabric))*\s*[\]\)\}]/gi;
const RE_TRAILING_LOADER =
  /(?:\s*[-–—|:]\s*|\s+for\s+)(?:neoforge|fabric|forge|quilt)(?:\s*[/,&+]\s*(?:neoforge|fabric|forge|quilt))*\s*$/i;
const RE_GIT_REPO = /https?:\/\/(?:www\.)?(?:github\.com|gitlab\.com|codeberg\.org)\/([a-z0-9_.-]+\/[a-z0-9_.-]+)/i;

function compactNorm(raw: string): string {
  const cleaned = String(raw || "")
    .replace(RE_BRACKET_SUFFIX, " ")
    .replace(RE_TRAILING_LOADER, " ")
    .toLowerCase();
  return cleaned.replace(/[^a-z0-9а-яё]+/gi, "");
}

function extractRepo(card: ProjectCard): string {
  const l = card.links;
  if (!l) return "";
  for (const u of [l.source_url, l.issues_url, l.wiki_url]) {
    if (!u) continue;
    const m = RE_GIT_REPO.exec(u);
    if (m && m[1]) {
      return m[1].toLowerCase().replace(/\.git$/, "").replace(/\/+$/, "");
    }
  }
  return "";
}

function uniqueCI(a: string[], b: string[]): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const list of [a, b]) {
    for (const v of list || []) {
      const t = String(v || "").trim();
      if (!t) continue;
      const low = t.toLowerCase();
      if (!seen.has(low)) {
        seen.add(low);
        out.push(t);
      }
    }
  }
  return out;
}

function mergeTwoCards(existing: ProjectCard, incoming: ProjectCard): { card: ProjectCard; changed: boolean } {
  const existingSKeys = new Set((existing.sources || []).map((s) => s.source_key));
  const newSources: ProjectSource[] = [];
  for (const s of incoming.sources || []) {
    if (s.source_key && !existingSKeys.has(s.source_key)) {
      existingSKeys.add(s.source_key);
      newSources.push(s);
    }
  }
  if (newSources.length === 0) {
    return { card: existing, changed: false };
  }

  const allSources = [...(existing.sources || []), ...newSources];
  const platforms = uniqueCI(existing.platforms || [], incoming.platforms || []);
  const addedDl = newSources.reduce((acc, s) => acc + Number(s.downloads || 0), 0);

  const merged: ProjectCard = {
    ...existing,
    is_merged: allSources.length > 1 || platforms.length > 1,
    platforms,
    sources: allSources,
    downloads: Number(existing.downloads || 0) + (addedDl > 0 ? addedDl : Number(incoming.downloads || 0)),
    authors: uniqueCI(existing.authors || [], incoming.authors || []),
    loaders: uniqueCI(existing.loaders || [], incoming.loaders || []),
    game_versions: uniqueCI(existing.game_versions || [], incoming.game_versions || []),
    categories: uniqueCI(existing.categories || [], incoming.categories || []),
    icon_url: existing.icon_url || incoming.icon_url,
    summary: (incoming.summary || "").length > (existing.summary || "").length ? incoming.summary : existing.summary,
    date_modified:
      incoming.date_modified && incoming.date_modified > (existing.date_modified || "")
        ? incoming.date_modified
        : existing.date_modified,
    date_created:
      incoming.date_created && (!existing.date_created || incoming.date_created < existing.date_created)
        ? incoming.date_created
        : existing.date_created,
    client_side: existing.client_side || incoming.client_side,
    server_side: existing.server_side || incoming.server_side,
    license: existing.license || incoming.license,
    links: {
      source_url: existing.links?.source_url || incoming.links?.source_url || "",
      issues_url: existing.links?.issues_url || incoming.links?.issues_url || "",
      wiki_url: existing.links?.wiki_url || incoming.links?.wiki_url || "",
      discord_url: existing.links?.discord_url || incoming.links?.discord_url || "",
    },
  };
  return { card: merged, changed: true };
}

export interface SessionIngestResult {
  items: ProjectCard[];
  added: number;
  updated: number;
  skipped: number;
  dedupMs: number;
}

/**
 * O(1) Set/Map-based Search Session Deduplicator.
 * Guarantees that within a single search session:
 * - No project is ever displayed twice (across parallel sources, pagination, or refreshes).
 * - When the second platform arrives for an already-displayed card, that card is enriched in-place.
 * - Doubtful title-only similarities without author/slug/repo corroboration remain separate cards.
 */
export class SearchSessionDeduper {
  private items: ProjectCard[] = [];
  private bySourceKey = new Map<string, number>();
  private byGroupId = new Map<string, number>();
  private byRepo = new Map<string, number>();
  private byTypeSlugAuthor = new Map<string, number>();
  private byTypeTitleAuthor = new Map<string, number>();
  private byTypeExactSlug = new Map<string, number>();

  reset(): void {
    this.items = [];
    this.bySourceKey.clear();
    this.byGroupId.clear();
    this.byRepo.clear();
    this.byTypeSlugAuthor.clear();
    this.byTypeTitleAuthor.clear();
    this.byTypeExactSlug.clear();
  }

  getItems(): ProjectCard[] {
    return this.items;
  }

  private indexCard(card: ProjectCard, idx: number): void {
    if (card.group_id) this.byGroupId.set(card.group_id, idx);
    for (const s of card.sources || []) {
      if (s.source_key) this.bySourceKey.set(s.source_key.toLowerCase(), idx);
    }
    const ct = (card.content_type || "mod").toLowerCase();
    const repo = extractRepo(card);
    if (repo) this.byRepo.set(`${ct}::${repo}`, idx);

    const normTitle = compactNorm(card.title);
    const normSlug = compactNorm(card.slug || card.sources?.[0]?.slug || "");
    if (normSlug.length >= 5) {
      const sk = `${ct}::${normSlug}`;
      if (!this.byTypeExactSlug.has(sk)) {
        this.byTypeExactSlug.set(sk, idx);
      }
    }
    for (const a of card.authors || []) {
      const na = compactNorm(a);
      if (na.length < 2) continue;
      if (normSlug.length >= 3) {
        this.byTypeSlugAuthor.set(`${ct}::${normSlug}::${na}`, idx);
      }
      if (normTitle.length >= 3) {
        this.byTypeTitleAuthor.set(`${ct}::${normTitle}::${na}`, idx);
      }
    }
  }

  private findExistingIndex(card: ProjectCard): number {
    // 1. Exact platform source_key match (strongest, O(1))
    for (const s of card.sources || []) {
      if (!s.source_key) continue;
      const idx = this.bySourceKey.get(s.source_key.toLowerCase());
      if (idx !== undefined) return idx;
    }
    // 2. Exact group_id match (O(1))
    if (card.group_id) {
      const idx = this.byGroupId.get(card.group_id);
      if (idx !== undefined) return idx;
    }
    const ct = (card.content_type || "mod").toLowerCase();
    // 3. Exact normalized Git repository match (O(1))
    const repo = extractRepo(card);
    if (repo) {
      const idx = this.byRepo.get(`${ct}::${repo}`);
      if (idx !== undefined) return idx;
    }
    const normTitle = compactNorm(card.title);
    const normSlug = compactNorm(card.slug || card.sources?.[0]?.slug || "");
    // 4. Author + slug or author + normalized title match (O(1) — never merges different mods by different authors!)
    for (const a of card.authors || []) {
      const na = compactNorm(a);
      if (na.length < 2) continue;
      if (normSlug.length >= 3) {
        const idx = this.byTypeSlugAuthor.get(`${ct}::${normSlug}::${na}`);
        if (idx !== undefined) return idx;
      }
      if (normTitle.length >= 3) {
        const idx = this.byTypeTitleAuthor.get(`${ct}::${normTitle}::${na}`);
        if (idx !== undefined) return idx;
      }
    }
    // 5. Cross-platform exact slug + exact normalized title match (only across DIFFERENT platforms, conservative!)
    if (normSlug.length >= 5) {
      const idx = this.byTypeExactSlug.get(`${ct}::${normSlug}`);
      if (idx !== undefined) {
        const existing = this.items[idx];
        const incPlats = new Set(card.platforms || []);
        const sharesPlatform = (existing.platforms || []).some((p) => incPlats.has(p));
        if (!sharesPlatform && compactNorm(existing.title) === normTitle) {
          return idx;
        }
      }
    }
    return -1;
  }

  ingest(incoming: ProjectCard[], resortMode?: SortMode): SessionIngestResult {
    const t0 = typeof performance !== "undefined" ? performance.now() : Date.now();
    let added = 0;
    let updated = 0;
    let skipped = 0;
    let mutated = false;
    const next = this.items.slice();

    for (const card of incoming || []) {
      if (!card || (!card.group_id && !(card.sources || []).length)) continue;
      const existingIdx = this.findExistingIndex(card);
      if (existingIdx >= 0) {
        const res = mergeTwoCards(next[existingIdx], card);
        if (res.changed) {
          next[existingIdx] = res.card;
          this.indexCard(res.card, existingIdx);
          updated++;
          mutated = true;
        } else {
          skipped++;
        }
        continue;
      }
      const newIdx = next.length;
      next.push(card);
      this.indexCard(card, newIdx);
      added++;
      mutated = true;
    }

    if (mutated && resortMode && next.length > 1) {
      if (resortMode === "downloads") {
        next.sort((a, b) => Number(b.downloads || 0) - Number(a.downloads || 0));
      } else if (resortMode === "updated") {
        next.sort((a, b) => String(b.date_modified || "").localeCompare(String(a.date_modified || "")));
      } else if (resortMode === "newest") {
        next.sort((a, b) =>
          String(b.date_created || b.date_modified || "").localeCompare(String(a.date_created || a.date_modified || ""))
        );
      }
      // Rebuild index positions in O(N) after re-sort
      this.items = next;
      this.rebuildIndex();
    } else if (mutated) {
      this.items = next;
    }

    const t1 = typeof performance !== "undefined" ? performance.now() : Date.now();
    return {
      items: this.items,
      added,
      updated,
      skipped,
      dedupMs: Math.max(0, t1 - t0),
    };
  }

  updateMerged(origGroupId: string, merged: ProjectCard): ProjectCard[] {
    if (!merged) return this.items;
    const foundIdx = this.findExistingIndex(merged);
    const origIdx = this.byGroupId.get(origGroupId) ?? -1;
    const idx = foundIdx >= 0 ? foundIdx : origIdx;
    if (idx < 0 || idx >= this.items.length) return this.items;

    const mergedSKeys = new Set((merged.sources || []).map((s) => s.source_key.toLowerCase()).filter(Boolean));
    const updatedCard: ProjectCard = {
      ...merged,
      dependent_info: this.items[idx].dependent_info || merged.dependent_info,
    };

    const next: ProjectCard[] = [];
    for (let i = 0; i < this.items.length; i++) {
      if (i === idx) {
        next.push(updatedCard);
        continue;
      }
      const cur = this.items[i];
      const overlapsMerged =
        cur.group_id === origGroupId ||
        cur.group_id === merged.group_id ||
        (cur.sources || []).some((s) => s.source_key && mergedSKeys.has(s.source_key.toLowerCase()));
      if (!overlapsMerged) {
        next.push(cur);
      }
    }
    this.items = next;
    this.rebuildIndex();
    return this.items;
  }

  private rebuildIndex(): void {
    this.bySourceKey.clear();
    this.byGroupId.clear();
    this.byRepo.clear();
    this.byTypeSlugAuthor.clear();
    this.byTypeTitleAuthor.clear();
    this.byTypeExactSlug.clear();
    for (let i = 0; i < this.items.length; i++) {
      this.indexCard(this.items[i], i);
    }
  }
}

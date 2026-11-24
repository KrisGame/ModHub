export interface PersistedCacheEntry {
  key: string;
  data: unknown;
  freshUntil: number;
  staleUntil: number;
  savedAt: number;
}

const DB_NAME = "modhub_swr_cache_v1";
const STORE_NAME = "entries";
const SS_PREFIX = "mh_swr_";
const MAX_IDB_ENTRIES = 140;

let dbPromise: Promise<IDBDatabase | null> | null = null;
let writeCount = 0;

function openDB(): Promise<IDBDatabase | null> {
  if (typeof indexedDB === "undefined") return Promise.resolve(null);
  if (dbPromise) return dbPromise;
  dbPromise = new Promise<IDBDatabase | null>((resolve) => {
    try {
      const req = indexedDB.open(DB_NAME, 1);
      req.onupgradeneeded = () => {
        const db = req.result;
        if (!db.objectStoreNames.contains(STORE_NAME)) {
          const store = db.createObjectStore(STORE_NAME, { keyPath: "key" });
          store.createIndex("savedAt", "savedAt", { unique: false });
        }
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => resolve(null);
    } catch {
      resolve(null);
    }
  });
  return dbPromise;
}

function isHotSessionKey(key: string): boolean {
  return key === "mc_versions" || key.startsWith("filters::");
}

/** Synchronously restores tiny hot metadata (MC versions & filter categories) from sessionStorage on boot. */
export function loadSyncSessionEntries(): PersistedCacheEntry[] {
  const out: PersistedCacheEntry[] = [];
  if (typeof sessionStorage === "undefined") return out;
  const now = Date.now();
  try {
    for (let i = 0; i < sessionStorage.length; i++) {
      const k = sessionStorage.key(i);
      if (!k || !k.startsWith(SS_PREFIX)) continue;
      const raw = sessionStorage.getItem(k);
      if (!raw) continue;
      const parsed = JSON.parse(raw) as PersistedCacheEntry;
      if (parsed && parsed.key && parsed.staleUntil > now) {
        out.push(parsed);
      }
    }
  } catch {
    // Ignore storage quota or parse errors
  }
  return out;
}

/** Asynchronously hydrates search/details/versions cache from IndexedDB on boot. */
export async function loadPersistentEntries(): Promise<PersistedCacheEntry[]> {
  const db = await openDB();
  if (!db) return [];
  const now = Date.now();
  return new Promise<PersistedCacheEntry[]>((resolve) => {
    try {
      const tx = db.transaction(STORE_NAME, "readonly");
      const store = tx.objectStore(STORE_NAME);
      const req = store.getAll();
      req.onsuccess = () => {
        const all = (req.result || []) as PersistedCacheEntry[];
        resolve(all.filter((e) => e && e.key && e.staleUntil > now));
      };
      req.onerror = () => resolve([]);
    } catch {
      resolve([]);
    }
  });
}

async function pruneOldEntries(db: IDBDatabase): Promise<void> {
  try {
    const tx = db.transaction(STORE_NAME, "readwrite");
    const store = tx.objectStore(STORE_NAME);
    const req = store.getAll();
    req.onsuccess = () => {
      const now = Date.now();
      const all = (req.result || []) as PersistedCacheEntry[];
      const valid: PersistedCacheEntry[] = [];
      for (const item of all) {
        if (!item || item.staleUntil <= now) {
          if (item?.key) store.delete(item.key);
        } else {
          valid.push(item);
        }
      }
      if (valid.length > MAX_IDB_ENTRIES) {
        valid.sort((a, b) => (a.savedAt || 0) - (b.savedAt || 0));
        const excess = valid.length - MAX_IDB_ENTRIES;
        for (let i = 0; i < excess; i++) {
          store.delete(valid[i].key);
        }
      }
    };
  } catch {
    // Best-effort pruning
  }
}

export function persistEntry(entry: PersistedCacheEntry): void {
  if (isHotSessionKey(entry.key) && typeof sessionStorage !== "undefined") {
    try {
      sessionStorage.setItem(SS_PREFIX + entry.key, JSON.stringify(entry));
    } catch {
      // Ignore quota errors
    }
  }
  void openDB().then((db) => {
    if (!db) return;
    try {
      const tx = db.transaction(STORE_NAME, "readwrite");
      tx.objectStore(STORE_NAME).put(entry);
      writeCount++;
      if (writeCount % 25 === 0) {
        void pruneOldEntries(db);
      }
    } catch {
      // Ignore transient IDB write errors
    }
  });
}

export function clearPersistentCache(): void {
  if (typeof sessionStorage !== "undefined") {
    try {
      const keysToDel: string[] = [];
      for (let i = 0; i < sessionStorage.length; i++) {
        const k = sessionStorage.key(i);
        if (k && k.startsWith(SS_PREFIX)) keysToDel.push(k);
      }
      for (const k of keysToDel) sessionStorage.removeItem(k);
    } catch {}
  }
  void openDB().then((db) => {
    if (!db) return;
    try {
      const tx = db.transaction(STORE_NAME, "readwrite");
      tx.objectStore(STORE_NAME).clear();
    } catch {}
  });
}

package storage

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"modhub/internal/domain"

	_ "modernc.org/sqlite"
)

type cacheWriteReq struct {
	key     string
	exp     int64
	payload string
	epoch   uint64
}

type lookupWriteReq struct {
	key   string
	epoch uint64
}

type Database struct {
	writeDB     *sql.DB
	readDB      *sql.DB
	writeMu     sync.Mutex
	memCache    *LRUCache[string]
	negCache    *LRUCache[bool]
	hasFTS5       bool
	schemaVersion int
	closed      atomic.Bool
	cacheEpoch  atomic.Uint64
	stopCh      chan struct{}
	clearCh     chan chan struct{}
	cacheCh     chan cacheWriteReq
	lookupCh    chan lookupWriteReq
	catalogCh   chan []domain.RawProjectCard
	rdepsCh     chan []domain.RDepEdge
	writerDone  chan struct{}
	janitorDone chan struct{}
}

func (d *Database) HasFTS5() bool {
	return d != nil && d.hasFTS5
}

// SchemaVersion returns the current applied SQLite schema migration version.
func (d *Database) SchemaVersion() int {
	if d == nil {
		return 0
	}
	if v, err := GetSchemaVersion(d.readDB); err == nil {
		return v
	}
	return d.schemaVersion
}

// AppliedMigrations returns all applied schema migrations in ascending order.
func (d *Database) AppliedMigrations() ([]AppliedMigration, error) {
	if d == nil {
		return nil, nil
	}
	return ListAppliedMigrations(d.readDB)
}

func OpenDatabase(dbPath string) (*Database, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	baseDSN := dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-16000)&_pragma=mmap_size(67108864)"

	writeDB, err := sql.Open("sqlite", baseDSN)
	if err != nil {
		return nil, err
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)

	schemaVer, err := RunMigrations(writeDB)
	if err != nil {
		_ = writeDB.Close()
		return nil, err
	}

	hasFTS := false
	var ftsName string
	if err := writeDB.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='catalog_fts'`).Scan(&ftsName); err == nil && ftsName == "catalog_fts" {
		hasFTS = true
	}

	readDB, err := sql.Open("sqlite", baseDSN+"&_pragma=query_only(ON)")
	if err != nil {
		_ = writeDB.Close()
		return nil, err
	}
	readDB.SetMaxOpenConns(6)
	readDB.SetMaxIdleConns(4)

	d := &Database{
		writeDB:       writeDB,
		readDB:        readDB,
		memCache:      NewLRUCache[string](512),
		negCache:      NewLRUCache[bool](2048),
		hasFTS5:       hasFTS,
		schemaVersion: schemaVer,
		stopCh:        make(chan struct{}),
		clearCh:       make(chan chan struct{}, 8),
		cacheCh:       make(chan cacheWriteReq, 256),
		lookupCh:      make(chan lookupWriteReq, 256),
		catalogCh:     make(chan []domain.RawProjectCard, 32),
		rdepsCh:       make(chan []domain.RDepEdge, 32),
		writerDone:    make(chan struct{}),
		janitorDone:   make(chan struct{}),
	}
	go d.runBatchWriter()
	go d.runCacheJanitor()
	return d, nil
}

func (d *Database) runBatchWriter() {
	defer close(d.writerDone)
	ticker := time.NewTicker(140 * time.Millisecond)
	defer ticker.Stop()

	cacheBuf := make([]cacheWriteReq, 0, 48)
	lookupBuf := make([]lookupWriteReq, 0, 48)

	dropStale := func(curEpoch uint64) {
		keptCache := cacheBuf[:0]
		for _, r := range cacheBuf {
			if r.epoch == curEpoch {
				keptCache = append(keptCache, r)
			}
		}
		cacheBuf = keptCache

		keptLookup := lookupBuf[:0]
		for _, l := range lookupBuf {
			if l.epoch == curEpoch {
				keptLookup = append(keptLookup, l)
			}
		}
		lookupBuf = keptLookup
	}

	flush := func() {
		curEpoch := d.cacheEpoch.Load()
		dropStale(curEpoch)
		if len(cacheBuf) > 0 {
			d.flushCacheBatch(cacheBuf)
			cacheBuf = cacheBuf[:0]
		}
		if len(lookupBuf) > 0 {
			keys := make([]string, 0, len(lookupBuf))
			for _, l := range lookupBuf {
				if l.epoch == d.cacheEpoch.Load() {
					keys = append(keys, l.key)
				}
			}
			if len(keys) > 0 {
				d.MarkLookups(keys)
			}
			lookupBuf = lookupBuf[:0]
		}
	}

	drainQueuedBeforeClear := func(curEpoch uint64) {
		for {
			select {
			case req := <-d.cacheCh:
				if req.epoch == curEpoch {
					cacheBuf = append(cacheBuf, req)
				}
			case lk := <-d.lookupCh:
				if lk.epoch == curEpoch {
					lookupBuf = append(lookupBuf, lk)
				}
			default:
				dropStale(curEpoch)
				return
			}
		}
	}

	for {
		select {
		case done := <-d.clearCh:
			drainQueuedBeforeClear(d.cacheEpoch.Load())
			close(done)
		case req := <-d.cacheCh:
			if req.epoch == d.cacheEpoch.Load() {
				cacheBuf = append(cacheBuf, req)
				if len(cacheBuf) >= 48 {
					flush()
				}
			}
		case lk := <-d.lookupCh:
			if lk.epoch == d.cacheEpoch.Load() {
				lookupBuf = append(lookupBuf, lk)
				if len(lookupBuf) >= 48 {
					flush()
				}
			}
		case items := <-d.catalogCh:
			d.UpsertCatalogItems(items)
		case edges := <-d.rdepsCh:
			d.UpsertReverseDeps(edges)
		case <-ticker.C:
			flush()
		case <-d.stopCh:
		drain:
			for {
				select {
				case done := <-d.clearCh:
					drainQueuedBeforeClear(d.cacheEpoch.Load())
					close(done)
				case req := <-d.cacheCh:
					if req.epoch == d.cacheEpoch.Load() {
						cacheBuf = append(cacheBuf, req)
					}
				case lk := <-d.lookupCh:
					if lk.epoch == d.cacheEpoch.Load() {
						lookupBuf = append(lookupBuf, lk)
					}
				case items := <-d.catalogCh:
					d.UpsertCatalogItems(items)
				case edges := <-d.rdepsCh:
					d.UpsertReverseDeps(edges)
				default:
					break drain
				}
			}
			flush()
			return
		}
	}
}

func (d *Database) flushCacheBatch(batch []cacheWriteReq) {
	if len(batch) == 0 {
		return
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return
	}
	curEpoch := d.cacheEpoch.Load()
	tx, err := d.writeDB.Begin()
	if err != nil {
		return
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO http_cache (cache_key, expires_at, payload) VALUES (?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return
	}
	defer stmt.Close()
	for _, r := range batch {
		if r.epoch != curEpoch {
			continue
		}
		_, _ = stmt.Exec(r.key, r.exp, r.payload)
	}
	_ = tx.Commit()
}

func (d *Database) runCacheJanitor() {
	defer close(d.janitorDone)
	d.CleanupExpiredCache()
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.CleanupExpiredCache()
		case <-d.stopCh:
			return
		}
	}
}

func (d *Database) CleanupExpiredCache() {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return
	}
	now := time.Now().Unix()
	_, _ = d.writeDB.Exec(`DELETE FROM http_cache WHERE expires_at <= ?`, now)
	_, _ = d.writeDB.Exec(`DELETE FROM lookup_log WHERE updated_at <= ? AND lookup_key NOT LIKE 'schema_mig::%'`, now-86400)
}

func (d *Database) Close() error {
	select {
	case <-d.stopCh:
	default:
		close(d.stopCh)
	}
	<-d.writerDone
	<-d.janitorDone
	d.writeMu.Lock()
	d.closed.Store(true)
	d.writeMu.Unlock()
	if d.readDB != nil {
		_ = d.readDB.Close()
	}
	if d.writeDB != nil {
		_, _ = d.writeDB.Exec(`PRAGMA optimize`)
		return d.writeDB.Close()
	}
	return nil
}

func (d *Database) CacheGet(key string) (string, bool) {
	if val, ok := d.memCache.Get(key); ok {
		return val, true
	}
	var exp int64
	var payload string
	err := d.readDB.QueryRow(`SELECT expires_at, payload FROM http_cache WHERE cache_key = ?`, key).Scan(&exp, &payload)
	if err != nil {
		return "", false
	}
	now := time.Now().Unix()
	if exp <= now {
		return "", false
	}
	remTTL := exp - now
	if remTTL > 300 {
		remTTL = 300
	}
	d.memCache.Set(key, payload, remTTL)
	return payload, true
}

func (d *Database) CacheSet(key, payload string, ttlSecs int64) {
	if ttlSecs <= 0 {
		return
	}
	memTTL := ttlSecs
	if memTTL > 300 {
		memTTL = 300
	}
	d.memCache.Set(key, payload, memTTL)

	req := cacheWriteReq{
		key:     key,
		exp:     time.Now().Unix() + ttlSecs,
		payload: payload,
		epoch:   d.cacheEpoch.Load(),
	}
	select {
	case d.cacheCh <- req:
	default:
	}
}

func (d *Database) CacheClear() {
	d.cacheEpoch.Add(1)
	d.memCache.Clear()
	d.negCache.Clear()

	done := make(chan struct{})
	select {
	case d.clearCh <- done:
		select {
		case <-done:
		case <-d.stopCh:
		}
	case <-d.stopCh:
	}

	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, _ = d.writeDB.Exec(`DELETE FROM http_cache`)
	_, _ = d.writeDB.Exec(`DELETE FROM lookup_log WHERE lookup_key LIKE 'neg_sib::%'`)
}

func (d *Database) UpsertCatalogItemsAsync(items []domain.RawProjectCard) {
	if len(items) == 0 || d.closed.Load() {
		return
	}
	cp := append([]domain.RawProjectCard(nil), items...)
	select {
	case d.catalogCh <- cp:
	case <-d.stopCh:
	default:
		d.UpsertCatalogItems(cp)
	}
}

func (d *Database) UpsertCatalogItems(items []domain.RawProjectCard) {
	if len(items) == 0 {
		return
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return
	}
	tx, err := d.writeDB.Begin()
	if err != nil {
		return
	}
	stmt, err := tx.Prepare(`
		INSERT INTO catalog_items
			(source_key, platform, content_type, title, norm_title, downloads, data, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_key) DO UPDATE SET
			platform=excluded.platform,
			content_type=excluded.content_type,
			title=excluded.title,
			norm_title=excluded.norm_title,
			downloads=excluded.downloads,
			data=excluded.data,
			updated_at=excluded.updated_at
	`)
	if err != nil {
		_ = tx.Rollback()
		return
	}
	defer stmt.Close()

	var rowidStmt, ftsDel, ftsIns *sql.Stmt
	if d.hasFTS5 {
		rowidStmt, _ = tx.Prepare(`SELECT rowid FROM catalog_items WHERE source_key = ?`)
		ftsDel, _ = tx.Prepare(`DELETE FROM catalog_fts WHERE rowid = ?`)
		ftsIns, _ = tx.Prepare(`INSERT INTO catalog_fts (rowid, source_key, title, summary, authors) VALUES (?, ?, ?, ?, ?)`)
		if rowidStmt != nil {
			defer rowidStmt.Close()
		}
		if ftsDel != nil {
			defer ftsDel.Close()
		}
		if ftsIns != nil {
			defer ftsIns.Close()
		}
	}

	now := time.Now().Unix()
	for _, it := range items {
		if it.SourceKey == "" {
			continue
		}
		raw, err := json.Marshal(it)
		if err != nil {
			continue
		}
		norm := domain.NormalizeCompactTitle(it.Title)
		_, _ = stmt.Exec(it.SourceKey, it.Platform, it.ContentType, it.Title, norm, it.Downloads, string(raw), now)
		if rowidStmt != nil && ftsDel != nil && ftsIns != nil {
			var rid int64
			if rowidStmt.QueryRow(it.SourceKey).Scan(&rid) == nil && rid > 0 {
				_, _ = ftsDel.Exec(rid)
				_, _ = ftsIns.Exec(rid, it.SourceKey, it.Title, it.Summary, strings.Join(it.Authors, " "))
			}
		}
	}
	_ = tx.Commit()
}

func (d *Database) GetCatalogItem(sourceKey string) (*domain.RawProjectCard, bool) {
	var data string
	err := d.readDB.QueryRow(`SELECT data FROM catalog_items WHERE source_key = ?`, sourceKey).Scan(&data)
	if err != nil {
		return nil, false
	}
	var card domain.RawProjectCard
	if err := json.Unmarshal([]byte(data), &card); err != nil {
		return nil, false
	}
	return &card, true
}

func (d *Database) GetCatalogTitles(contentType string, limit int) []string {
	rows, err := d.readDB.Query(`SELECT title FROM catalog_items WHERE content_type = ? ORDER BY downloads DESC LIMIT ?`, contentType, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]string, 0, min(limit, 64))
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			out = append(out, t)
		}
	}
	return out
}

func sanitizeFTSQuery(raw string) string {
	words := strings.Fields(domain.NormalizeHaystack(raw))
	if len(words) == 0 {
		return ""
	}
	parts := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) >= 2 {
			parts = append(parts, `"`+strings.ReplaceAll(w, `"`, "")+`"*`)
		}
	}
	return strings.Join(parts, " ")
}

func (d *Database) FindSourceKeysByQuery(query string, limit int) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var out []string
	seen := make(map[string]struct{}, limit)

	if d.hasFTS5 {
		if ftsQ := sanitizeFTSQuery(q); ftsQ != "" {
			rows, err := d.readDB.Query(`
				SELECT c.source_key
				FROM catalog_fts f
				JOIN catalog_items c ON c.source_key = f.source_key
				WHERE catalog_fts MATCH ?
				ORDER BY c.downloads DESC
				LIMIT ?
			`, ftsQ, limit)
			if err == nil {
				for rows.Next() {
					var sk string
					if rows.Scan(&sk) == nil {
						if _, dup := seen[sk]; !dup {
							seen[sk] = struct{}{}
							out = append(out, sk)
						}
					}
				}
				_ = rows.Close()
			}
		}
	}

	if len(out) >= limit {
		return out
	}

	pat := "%" + strings.ReplaceAll(q, "%", "") + "%"
	compPat := "%" + domain.NormalizeCompactTitle(q) + "%"
	rows, err := d.readDB.Query(
		`SELECT source_key FROM catalog_items WHERE lower(title) LIKE ? OR norm_title LIKE ? ORDER BY downloads DESC LIMIT ?`,
		pat, compPat, limit,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var sk string
		if rows.Scan(&sk) == nil {
			if _, dup := seen[sk]; !dup {
				seen[sk] = struct{}{}
				out = append(out, sk)
				if len(out) >= limit {
					break
				}
			}
		}
	}
	return out
}


func (d *Database) UpsertReverseDepsAsync(edges []domain.RDepEdge) {
	if len(edges) == 0 || d.closed.Load() {
		return
	}
	cp := append([]domain.RDepEdge(nil), edges...)
	select {
	case d.rdepsCh <- cp:
	case <-d.stopCh:
	default:
		d.UpsertReverseDeps(cp)
	}
}

func (d *Database) UpsertReverseDeps(edges []domain.RDepEdge) {
	if len(edges) == 0 {
		return
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return
	}
	tx, err := d.writeDB.Begin()
	if err != nil {
		return
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO reverse_deps (dep_key, dependent_key, dep_type, updated_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, e := range edges {
		if e.DepKey == "" || e.DependentKey == "" {
			continue
		}
		_, _ = stmt.Exec(e.DepKey, e.DependentKey, e.DepType, now)
	}
	_ = tx.Commit()
}

func buildInPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	if n == 1 {
		return "?"
	}
	return strings.Repeat("?,", n-1) + "?"
}

func (d *Database) GetReverseDependents(depKeys []string, depTypes []string, limit int) []domain.RDepEdge {
	if len(depKeys) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 500
	}
	args := make([]any, 0, len(depKeys)+len(depTypes)+1)
	for _, dk := range depKeys {
		args = append(args, dk)
	}
	q := `SELECT dep_key, dependent_key, dep_type FROM reverse_deps WHERE dep_key IN (` + buildInPlaceholders(len(depKeys)) + `)`
	if len(depTypes) > 0 {
		for _, dt := range depTypes {
			args = append(args, dt)
		}
		q += ` AND dep_type IN (` + buildInPlaceholders(len(depTypes)) + `)`
	}
	q += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := d.readDB.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := make([]domain.RDepEdge, 0, min(limit, 64))
	seen := make(map[string]struct{}, min(limit, 64))
	for rows.Next() {
		var dk, depKey, depType string
		if rows.Scan(&dk, &depKey, &depType) == nil {
			edgeKey := depKey + "|" + depType
			if _, dup := seen[edgeKey]; dup {
				continue
			}
			seen[edgeKey] = struct{}{}
			out = append(out, domain.RDepEdge{DepKey: dk, DependentKey: depKey, DepType: depType})
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func (d *Database) FilterUnindexedLookups(keys []string, ttlSecs int64) []string {
	if len(keys) == 0 {
		return nil
	}
	cutoff := time.Now().Unix() - ttlSecs
	fresh := make(map[string]struct{}, len(keys))
	const chunkSize = 200
	for start := 0; start < len(keys); start += chunkSize {
		end := start + chunkSize
		if end > len(keys) {
			end = len(keys)
		}
		chunk := keys[start:end]
		args := make([]any, 0, len(chunk)+1)
		for _, k := range chunk {
			args = append(args, k)
		}
		args = append(args, cutoff)
		q := `SELECT lookup_key FROM lookup_log WHERE lookup_key IN (` + buildInPlaceholders(len(chunk)) + `) AND updated_at > ?`
		rows, err := d.readDB.Query(q, args...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var k string
			if rows.Scan(&k) == nil {
				fresh[k] = struct{}{}
			}
		}
		_ = rows.Close()
	}

	needed := make([]string, 0, len(keys))
	for _, k := range keys {
		if _, ok := fresh[k]; !ok {
			needed = append(needed, k)
		}
	}
	return needed
}

func (d *Database) MarkLookups(keys []string) {
	if len(keys) == 0 {
		return
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return
	}
	tx, err := d.writeDB.Begin()
	if err != nil {
		return
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO lookup_log (lookup_key, updated_at) VALUES (?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, k := range keys {
		_, _ = stmt.Exec(k, now)
	}
	_ = tx.Commit()
}

func (d *Database) Stats() (int, int) {
	var cat, rdeps int
	_ = d.readDB.QueryRow(`SELECT COUNT(*) FROM catalog_items`).Scan(&cat)
	_ = d.readDB.QueryRow(`SELECT COUNT(*) FROM reverse_deps`).Scan(&rdeps)
	return cat, rdeps
}

func (d *Database) GetCatalogItemsByType(contentType string, limit int) []domain.RawProjectCard {
	rows, err := d.readDB.Query(
		`SELECT data FROM catalog_items WHERE content_type = ? ORDER BY downloads DESC LIMIT ?`,
		contentType, limit,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]domain.RawProjectCard, 0, min(limit, 128))
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) == nil {
			var card domain.RawProjectCard
			if json.Unmarshal([]byte(raw), &card) == nil {
				out = append(out, card)
			}
		}
	}
	return out
}

func (d *Database) CountsByType() map[string]int {
	out := map[string]int{"mod": 0, "resourcepack": 0, "shader": 0, "datapack": 0}
	rows, err := d.readDB.Query(`SELECT content_type, COUNT(*) FROM catalog_items GROUP BY content_type`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var ct string
		var cnt int
		if rows.Scan(&ct, &cnt) == nil {
			out[ct] = cnt
		}
	}
	return out
}

func (d *Database) PlatformCountsByType() map[string]map[string]int {
	out := map[string]map[string]int{
		"mod":          {"modrinth": 0, "curseforge": 0},
		"resourcepack": {"modrinth": 0, "curseforge": 0},
		"shader":       {"modrinth": 0, "curseforge": 0},
		"datapack":     {"modrinth": 0, "curseforge": 0},
	}
	rows, err := d.readDB.Query(`SELECT content_type, platform, COUNT(*) FROM catalog_items GROUP BY content_type, platform`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var ct, plat string
		var cnt int
		if rows.Scan(&ct, &plat, &cnt) == nil {
			if _, ok := out[ct]; !ok {
				out[ct] = map[string]int{"modrinth": 0, "curseforge": 0}
			}
			out[ct][plat] = cnt
		}
	}
	return out
}

func (d *Database) FindCatalogSiblings(contentType, normTitle string) []domain.RawProjectCard {
	if normTitle == "" {
		return nil
	}
	rows, err := d.readDB.Query(
		`SELECT data FROM catalog_items WHERE content_type = ? AND norm_title = ? ORDER BY downloads DESC LIMIT 8`,
		contentType, normTitle,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []domain.RawProjectCard
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) == nil {
			var card domain.RawProjectCard
			if json.Unmarshal([]byte(raw), &card) == nil {
				out = append(out, card)
			}
		}
	}
	return out
}

func (d *Database) IsNegLookupCached(key string, ttlSecs int64) bool {
	if key == "" || ttlSecs <= 0 {
		return false
	}
	if hit, ok := d.negCache.Get(key); ok {
		return hit
	}
	var ts int64
	err := d.readDB.QueryRow(`SELECT updated_at FROM lookup_log WHERE lookup_key = ?`, key).Scan(&ts)
	if err != nil {
		return false
	}
	rem := (ts + ttlSecs) - time.Now().Unix()
	if rem <= 0 {
		return false
	}
	d.negCache.Set(key, true, rem)
	return true
}

func (d *Database) MarkNegLookupAsync(key string, ttlSecs int64) {
	if key == "" || ttlSecs <= 0 {
		return
	}
	d.negCache.Set(key, true, ttlSecs)
	select {
	case d.lookupCh <- lookupWriteReq{key: key, epoch: d.cacheEpoch.Load()}:
	default:
	}
}

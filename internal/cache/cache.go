// Package cache is a two-layer, pure-Go SQLite store that makes repeated
// extraction cheap and keeps sec-cli a good EDGAR citizen by not re-fetching
// bytes it already has.
//
// It wraps the fetch seam rather than living inside internal/edgar, so both
// packages stay single-purpose: edgar is a pure HTTP client, cache is a pure
// key/value store. The two layers key on two different things:
//
//   - The raw layer caches the bytes of every EDGAR fetch by URL. EDGAR
//     documents for a filed accession are immutable, so a URL hit is
//     permanently valid — there is no TTL.
//   - The parsed layer caches the assembled model.Document (as canonical JSON)
//     by accession + model.ParserVersion. A parser fix bumps ParserVersion and
//     transparently invalidates parsed output while the raw bytes stay warm, so
//     re-parsing costs no network.
//
// The driver is modernc.org/sqlite (pure Go) so CGO_ENABLED=0 builds keep
// working on every platform.
package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" database/sql driver

	"github.com/kritidutta01/sec-cli/internal/model"
)

// schemaVersion is the cache's table-layout version, tracked in PRAGMA
// user_version. Bumping it adds a migration step in migrate; it is independent
// of model.SchemaVersion (the output contract) and model.ParserVersion (the
// parsed-layer key).
const schemaVersion = 2

// FetchFunc is the fetch seam the cache wraps: the shape of edgar.Client.Get.
type FetchFunc func(ctx context.Context, url string) ([]byte, error)

// Cache is the two-layer store. The zero value (and any nil *Cache) is a valid
// no-op cache: every Get misses and every Put is silently dropped, which is how
// --no-cache and hermetic tests bypass persistence. A Cache opened with Open is
// backed by a SQLite file; close it with Close.
type Cache struct {
	db   *sql.DB
	path string
}

// NoOp returns a cache that stores nothing: every lookup misses and every write
// is dropped. Used for --no-cache and tests that want the pipeline shape
// without touching disk.
func NoOp() *Cache { return &Cache{} }

// DefaultPath is the cache file location under the OS cache directory:
// <os.UserCacheDir>/sec-cli/cache.db.
func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache: locate user cache dir: %w", err)
	}
	return filepath.Join(dir, "sec-cli", "cache.db"), nil
}

// Open opens (creating if absent) the SQLite cache at path, creating the parent
// directory as needed, and runs migrations gated on PRAGMA user_version. Pass
// DefaultPath() for the standard location.
func Open(path string) (*Cache, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("cache: create cache dir: %w", err)
	}

	// busy_timeout lets concurrent invocations wait briefly rather than fail on
	// a locked database.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("cache: open %s: %w", path, err)
	}
	// The pure-Go driver serializes writers most reliably with a single
	// connection; a CLI cache sees no contention worth more.
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Cache{db: db, path: path}, nil
}

// Path returns the cache file location, or "" for a no-op cache.
func (c *Cache) Path() string {
	if c == nil {
		return ""
	}
	return c.path
}

// Close releases the underlying database. It is safe to call on a no-op cache.
func (c *Cache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	if err := c.db.Close(); err != nil {
		return fmt.Errorf("cache: close: %w", err)
	}
	return nil
}

// GetRaw returns the cached bytes for url and whether they were present. A
// no-op cache always misses.
func (c *Cache) GetRaw(url string) ([]byte, bool) {
	if c == nil || c.db == nil {
		return nil, false
	}
	var body []byte
	err := c.db.QueryRow("SELECT body FROM raw WHERE url = ?", url).Scan(&body)
	if err != nil {
		return nil, false
	}
	return body, true
}

// PutRaw stores body under url. EDGAR bytes for a filed accession are immutable,
// so an existing entry is overwritten harmlessly. A no-op cache drops the write.
func (c *Cache) PutRaw(url string, body []byte) error {
	if c == nil || c.db == nil {
		return nil
	}
	_, err := c.db.Exec(
		"INSERT INTO raw (url, body, fetched_at) VALUES (?, ?, ?) "+
			"ON CONFLICT(url) DO UPDATE SET body = excluded.body, fetched_at = excluded.fetched_at",
		url, body, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("cache: put raw %s: %w", url, err)
	}
	return nil
}

// GetDocument returns the cached Document for accession under the current
// model.ParserVersion and model.SchemaVersion. A document cached under a
// different parser or schema version misses, so either a parser fix or a schema
// bump invalidates parsed output without touching the raw layer.
func (c *Cache) GetDocument(accession string) (*model.Document, bool) {
	if c == nil || c.db == nil {
		return nil, false
	}
	var raw []byte
	err := c.db.QueryRow(
		"SELECT doc FROM parsed WHERE accession = ? AND parser_version = ? AND schema_version = ?",
		accession, model.ParserVersion, model.SchemaVersion,
	).Scan(&raw)
	if err != nil {
		return nil, false
	}
	var doc model.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		// A corrupt or schema-incompatible row is treated as a miss rather than
		// an error: re-parsing overwrites it.
		return nil, false
	}
	return &doc, true
}

// PutDocument stores doc as canonical JSON keyed on accession, model.ParserVersion,
// and model.SchemaVersion. A no-op cache drops the write.
func (c *Cache) PutDocument(accession string, doc *model.Document) error {
	if c == nil || c.db == nil {
		return nil
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("cache: marshal document %s: %w", accession, err)
	}
	_, err = c.db.Exec(
		"INSERT INTO parsed (accession, parser_version, schema_version, doc, created_at) VALUES (?, ?, ?, ?, ?) "+
			"ON CONFLICT(accession, parser_version, schema_version) DO UPDATE SET doc = excluded.doc, created_at = excluded.created_at",
		accession, model.ParserVersion, model.SchemaVersion, raw, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("cache: put document %s: %w", accession, err)
	}
	return nil
}

// Fetching wraps fetch so a URL is fetched at most once: it checks the raw layer
// first and populates it on a miss. This is what the pipeline composes around
// edgar.Client.Get — edgar itself is never modified. A no-op cache returns fetch
// unchanged in effect (every call misses and nothing is stored).
func (c *Cache) Fetching(fetch FetchFunc) FetchFunc {
	return func(ctx context.Context, url string) ([]byte, error) {
		if body, ok := c.GetRaw(url); ok {
			return body, nil
		}
		body, err := fetch(ctx, url)
		if err != nil {
			return nil, err
		}
		_ = c.PutRaw(url, body)
		return body, nil
	}
}

// Clear truncates both layers, leaving an empty but valid cache. It is safe to
// call on a no-op cache.
func (c *Cache) Clear() error {
	if c == nil || c.db == nil {
		return nil
	}
	if _, err := c.db.Exec("DELETE FROM raw"); err != nil {
		return fmt.Errorf("cache: clear raw: %w", err)
	}
	if _, err := c.db.Exec("DELETE FROM parsed"); err != nil {
		return fmt.Errorf("cache: clear parsed: %w", err)
	}
	return nil
}

// migrate brings the database up to schemaVersion, gated on PRAGMA user_version
// so an already-current file is a no-op and a reopened file does not re-run DDL.
func migrate(db *sql.DB) error {
	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("cache: read user_version: %w", err)
	}
	if current >= schemaVersion {
		return nil
	}

	if current < 1 {
		const ddl = `
CREATE TABLE IF NOT EXISTS raw (
	url        TEXT PRIMARY KEY,
	body       BLOB NOT NULL,
	fetched_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS parsed (
	accession      TEXT NOT NULL,
	parser_version TEXT NOT NULL,
	doc            BLOB NOT NULL,
	created_at     INTEGER NOT NULL,
	PRIMARY KEY (accession, parser_version)
);`
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("cache: create schema: %w", err)
		}
	}
	if current < 2 {
		// Add schema_version to the parsed key so a JSON schema bump also
		// invalidates cached output without touching the raw layer.
		const ddl2 = `
ALTER TABLE parsed ADD COLUMN schema_version TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS parsed_full_key
	ON parsed (accession, parser_version, schema_version);`
		if _, err := db.Exec(ddl2); err != nil {
			return fmt.Errorf("cache: migrate to schema v2: %w", err)
		}
	}

	// PRAGMA does not accept bound parameters; the value is a trusted constant.
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("cache: set user_version: %w", err)
	}
	return nil
}
